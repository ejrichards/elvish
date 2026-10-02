package term

import (
	"testing"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/testutil"
)

func TestKittyKeyboard(t *testing.T) {
	setupKittyKeyboard(t, false)

	// The first setup queries support.
	if s := kittyKeyboardSetupSeq(); s != kittyKeyboardQuery {
		t.Errorf("got setup seq %q, want query", s)
	}
	// The response enables the protocol while in the TUI.
	if s := handleKittyKeyboardFlags(t); s != kittyKeyboardEnable {
		t.Errorf("got %q written after response, want enable", s)
	}
	if s := kittyKeyboardRestoreSeq(); s != kittyKeyboardDisable {
		t.Errorf("got restore seq %q, want disable", s)
	}
	// Subsequent setups enable the protocol directly.
	if s := kittyKeyboardSetupSeq(); s != kittyKeyboardEnable {
		t.Errorf("got setup seq %q, want enable", s)
	}
	if s := kittyKeyboardRestoreSeq(); s != kittyKeyboardDisable {
		t.Errorf("got restore seq %q, want disable", s)
	}
}

func TestKittyKeyboard_NoResponse(t *testing.T) {
	setupKittyKeyboard(t, false)

	if s := kittyKeyboardSetupSeq(); s != kittyKeyboardQuery {
		t.Errorf("got setup seq %q, want query", s)
	}
	if s := kittyKeyboardRestoreSeq(); s != "" {
		t.Errorf("got restore seq %q, want empty", s)
	}
	// The query is only sent once.
	if s := kittyKeyboardSetupSeq(); s != "" {
		t.Errorf("got setup seq %q, want empty", s)
	}
	kittyKeyboardRestoreSeq()
	// A late response outside the TUI doesn't enable the protocol right away.
	if s := handleKittyKeyboardFlags(t); s != "" {
		t.Errorf("got %q written after response outside TUI, want empty", s)
	}
	if s := kittyKeyboardSetupSeq(); s != kittyKeyboardEnable {
		t.Errorf("got setup seq %q, want enable", s)
	}
}

func TestKittyKeyboard_Quirk(t *testing.T) {
	setupKittyKeyboard(t, true)

	if s := kittyKeyboardSetupSeq(); s != "" {
		t.Errorf("got setup seq %q with quirk, want empty", s)
	}
}

func TestSuspendKittyKeyboard(t *testing.T) {
	setupKittyKeyboard(t, false)
	kittyKeyboardSetupSeq()
	r, w := must.Pipe()
	defer r.Close()
	defer w.Close()
	if err := HandleKittyKeyboardFlags(w); err != nil {
		t.Fatal(err)
	}
	outer := SuspendKittyKeyboard()
	inner := SuspendKittyKeyboard()
	inner()
	if kittyKeyboardActive() {
		t.Fatal("nested suspension enabled the protocol too early")
	}
	outer()
	w.Close()
	if got := string(must.ReadAllAndClose(r)); got != kittyKeyboardEnable+kittyKeyboardDisable+kittyKeyboardEnable {
		t.Errorf("protocol writes = %q", got)
	}
}

var quirkTests = []struct {
	env  map[string]string
	want bool
}{
	{map[string]string{}, false},
	{map[string]string{"MC_SID": "123"}, true},
	{map[string]string{"MC_SID": "123", "__mc_kitty_keyboard": "1"}, false},
	{map[string]string{"TERM": "st-256color"}, true},
	{map[string]string{"KONSOLE_VERSION": "260780"}, true},
	{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.11"}, true},
	{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.4.0beta2"}, true},
	{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.12"}, false},
	{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6"}, false},
	{map[string]string{"TERM_PROGRAM": "WezTerm", "TERM_PROGRAM_VERSION": "1.0"}, false},
}

func TestHasKittyKeyboardQuirk(t *testing.T) {
	for _, test := range quirkTests {
		got := hasKittyKeyboardQuirk(func(s string) string { return test.env[s] })
		if got != test.want {
			t.Errorf("hasKittyKeyboardQuirk with %v -> %v, want %v", test.env, got, test.want)
		}
	}
}

func setupKittyKeyboard(t *testing.T, quirk bool) {
	testutil.Set(t, &kittyKeyboard, kittyKeyboardState{})
	testutil.Set(t, &kittyKeyboardQuirk, func() bool { return quirk })
}

func TestKittyKeyboard_EnableWriteFailure(t *testing.T) {
	setupKittyKeyboard(t, false)
	kittyKeyboardSetupSeq()
	r, w := must.Pipe()
	r.Close()
	w.Close()
	if err := HandleKittyKeyboardFlags(w); err == nil {
		t.Fatal("expected a write error")
	}
	if kittyKeyboardActive() {
		t.Error("failed write marked the protocol active")
	}
}

// Calls HandleKittyKeyboardFlags and returns what it wrote.
func handleKittyKeyboardFlags(t *testing.T) string {
	r, w := must.Pipe()
	err := HandleKittyKeyboardFlags(w)
	if err != nil {
		t.Errorf("HandleKittyKeyboardFlags -> %v", err)
	}
	w.Close()
	return string(must.ReadAllAndClose(r))
}
