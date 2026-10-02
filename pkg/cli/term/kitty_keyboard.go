package term

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Support for the kitty keyboard protocol's progressive enhancements
// (https://sw.kovidgoyal.net/kitty/keyboard-protocol/).
//
// The approach follows fish:
//
//   - The protocol is never enabled blindly. The first time the terminal is set
//     up for the TUI, the terminal is queried for its current flags with
//     CSI ? u. Terminals that don't support the protocol ignore the query.
//
//   - Once a response arrives, the protocol is enabled with CSI = 5 u, and
//     re-enabled each time the terminal is set up for the TUI. It is disabled
//     with CSI = 0 u whenever the terminal is restored, so that external
//     commands always see the legacy encoding. The "set" form is used instead
//     of the push/pop form so that a child that messes up the stack can't
//     leave the terminal in a bad state.
//
//   - The protocol is not used in some environments known to be problematic.
//
// Unlike fish, the query is not blocking: keys read before the response
// arrives are simply in the legacy encoding, which the reader also handles.

const (
	// Disambiguate escape codes (1) | report alternate keys (4).
	kittyKeyboardEnable  = "\033[=5u"
	kittyKeyboardDisable = "\033[=0u"
	kittyKeyboardQuery   = "\033[?u"
)

type kittyKeyboardState struct {
	// Whether the query has been sent.
	queried bool
	// Whether the terminal has responded to the query.
	supported bool
	// Whether the terminal is currently set up for the TUI.
	inTUI bool
	// Whether the protocol has been enabled and not yet disabled.
	active    bool
	out       *os.File
	suspended int
}

var (
	kittyKeyboardMutex sync.Mutex
	kittyKeyboard      kittyKeyboardState
)

var kittyKeyboardQuirk = sync.OnceValue(func() bool {
	return runtime.GOOS == "windows" || hasKittyKeyboardQuirk(os.Getenv)
})

// HandleKittyKeyboardFlags should be called when a [KittyKeyboardFlags] event
// is read. It records that the terminal supports the kitty keyboard protocol,
// and enables it if the terminal is currently set up for the TUI.
func HandleKittyKeyboardFlags(out *os.File) error {
	kittyKeyboardMutex.Lock()
	defer kittyKeyboardMutex.Unlock()
	kittyKeyboard.supported = true
	kittyKeyboard.out = out
	if kittyKeyboard.inTUI && !kittyKeyboard.active && kittyKeyboard.suspended == 0 && !kittyKeyboardQuirk() {
		_, err := out.WriteString(kittyKeyboardEnable)
		kittyKeyboard.active = err == nil
		return err
	}
	return nil
}

// Returns the sequence to write when setting up the terminal for the TUI.
func kittyKeyboardSetupSeq() string {
	kittyKeyboardMutex.Lock()
	defer kittyKeyboardMutex.Unlock()
	kittyKeyboard.inTUI = true
	if kittyKeyboardQuirk() {
		return ""
	}
	if kittyKeyboard.supported {
		if kittyKeyboard.suspended > 0 {
			return ""
		}
		kittyKeyboard.active = true
		return kittyKeyboardEnable
	}
	if !kittyKeyboard.queried {
		kittyKeyboard.queried = true
		return kittyKeyboardQuery
	}
	return ""
}

// Returns the sequence to write when restoring the terminal.
func kittyKeyboardRestoreSeq() string {
	kittyKeyboardMutex.Lock()
	defer kittyKeyboardMutex.Unlock()
	kittyKeyboard.inTUI = false
	if kittyKeyboard.active {
		kittyKeyboard.active = false
		return kittyKeyboardDisable
	}
	return ""
}

func kittyKeyboardActive() bool {
	kittyKeyboardMutex.Lock()
	defer kittyKeyboardMutex.Unlock()
	return kittyKeyboard.active
}

// SuspendKittyKeyboard switches to legacy input, for raw reads and while
// synchronous editor evaluation occupies the event loop, so that the terminal
// driver can still raise SIGINT and SIGQUIT. The returned function restores the
// protocol. Calls may be nested.
func SuspendKittyKeyboard() func() {
	kittyKeyboardMutex.Lock()
	kittyKeyboard.suspended++
	if kittyKeyboard.active && kittyKeyboard.out != nil {
		kittyKeyboard.out.WriteString(kittyKeyboardDisable)
		kittyKeyboard.active = false
	}
	kittyKeyboardMutex.Unlock()
	return func() {
		kittyKeyboardMutex.Lock()
		defer kittyKeyboardMutex.Unlock()
		kittyKeyboard.suspended--
		if kittyKeyboard.suspended == 0 && kittyKeyboard.inTUI && kittyKeyboard.supported &&
			!kittyKeyboard.active && kittyKeyboard.out != nil && !kittyKeyboardQuirk() {
			_, err := kittyKeyboard.out.WriteString(kittyKeyboardEnable)
			kittyKeyboard.active = err == nil
		}
	}
}

// Reports whether the environment is one where the kitty keyboard protocol is
// known to cause problems. Fish detects most of these with XTVERSION; Elvish
// doesn't query that, so environment variables are used instead.
func hasKittyKeyboardQuirk(getenv func(string) string) bool {
	// Midnight Commander can't parse CSI u.
	if getenv("MC_SID") != "" && getenv("__mc_kitty_keyboard") == "" {
		return true
	}
	// st can misinterpret the protocol query.
	if getenv("TERM") == "st-256color" {
		return true
	}
	// Konsole's implementation is buggy (fish-shell/fish-shell#12948).
	if getenv("KONSOLE_VERSION") != "" {
		return true
	}
	// iTerm2 before 3.5.12 has a buggy implementation
	// (fish-shell/fish-shell#11192).
	if getenv("TERM_PROGRAM") == "iTerm.app" &&
		versionLess(getenv("TERM_PROGRAM_VERSION"), []int{3, 5, 12}) {
		return true
	}
	return false
}

// Reports whether the dot-separated version string v is less than want.
// Unparsable components are treated as 0.
func versionLess(v string, want []int) bool {
	parts := strings.Split(v, ".")
	for i, w := range want {
		n := 0
		if i < len(parts) {
			// Ignore suffixes like "beta1" in "3.5.0beta1".
			s := parts[i]
			end := 0
			for end < len(s) && '0' <= s[end] && s[end] <= '9' {
				end++
			}
			n, _ = strconv.Atoi(s[:end])
		}
		if n != w {
			return n < w
		}
	}
	return false
}
