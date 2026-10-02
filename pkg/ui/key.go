package ui

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/persistent/hash"
)

// Key represents a single keyboard input, typically assembled from a escape
// sequence.
type Key struct {
	Rune rune
	Mod  Mod
}

// K constructs a new Key.
func K(r rune, mods ...Mod) Key {
	var mod Mod
	for _, m := range mods {
		mod |= m
	}
	return Key{r, mod}
}

// Default is used in the key binding table to indicate a default binding.
var DefaultKey = Key{DefaultBindingRune, 0}

// Mod represents a modifier key.
type Mod byte

// Values for Mod.
const (
	// Shift is the shift modifier. It is consumed when applied to a text key,
	// except for Ctrl-Shift-letter, which is distinct from Ctrl-letter.
	Shift Mod = 1 << iota
	// Alt is the alt modifier, traditionally known as the meta modifier.
	Alt
	Ctrl
)

const functionKeyOffset = 1000

// Special negative runes to represent function keys, used in the Rune field
// of the Key struct. This also has a few function names that are aliases for
// simple runes. See keyNames below for mapping these values to strings.
const (
	// DefaultBindingRune is a special value to represent a default binding.
	DefaultBindingRune rune = iota - functionKeyOffset

	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12

	Up
	Down
	Right
	Left

	Home
	Insert
	Delete
	End
	PageUp
	PageDown

	// Function key names that are aliases for their ASCII representation.
	Tab       = '\t'
	Enter     = '\n'
	Backspace = 0x7f
)

// keyNames maps runes, whether simple or function, to symbolic key names.
var keyNames = map[rune]string{
	DefaultBindingRune: "Default",
	F1:                 "F1",
	F2:                 "F2",
	F3:                 "F3",
	F4:                 "F4",
	F5:                 "F5",
	F6:                 "F6",
	F7:                 "F7",
	F8:                 "F8",
	F9:                 "F9",
	F10:                "F10",
	F11:                "F11",
	F12:                "F12",
	Up:                 "Up",
	Down:               "Down",
	Right:              "Right",
	Left:               "Left",
	Home:               "Home",
	Insert:             "Insert",
	Delete:             "Delete",
	End:                "End",
	PageUp:             "PageUp",
	PageDown:           "PageDown",
	Tab:                "Tab",
	Enter:              "Enter",
	Backspace:          "Backspace",
	' ':                "Space",
}

func (k Key) Kind() string {
	return "edit:key"
}

func (k Key) Equal(other any) bool {
	return k == other
}

func (k Key) Hash() uint32 {
	return hash.DJB(uint32(k.Rune), uint32(k.Mod))
}

func (k Key) Repr(int) string {
	return "(edit:key " + parse.Quote(k.String()) + ")"
}

func (k Key) String() string {
	var b bytes.Buffer

	if k.Mod&Ctrl != 0 {
		b.WriteString("Ctrl-")
	}
	if k.Mod&Alt != 0 {
		b.WriteString("Alt-")
	}
	if k.Mod&Shift != 0 {
		b.WriteString("Shift-")
	}

	if name, ok := keyNames[k.Rune]; ok {
		b.WriteString(name)
	} else {
		if k.Rune >= 0 {
			b.WriteRune(k.Rune)
		} else {
			fmt.Fprintf(&b, "(bad function key %d)", k.Rune)
		}
	}

	return b.String()
}

// modifierByName maps a name to an modifier. It is used for parsing keys where
// the modifier string is first turned to lower case, so that all of C, c,
// CTRL, Ctrl and ctrl can represent the Ctrl modifier.
var modifierByName = map[string]Mod{
	"S": Shift, "Shift": Shift,
	"A": Alt, "Alt": Alt,
	"M": Alt, "Meta": Alt,
	"C": Ctrl, "Ctrl": Ctrl,
}

// ParseKey parses a symbolic key. The syntax is:
//
//	Key = { Mod ('+' | '-') } BareKey
//
//	BareKey = FunctionKeyName | SingleRune
func ParseKey(s string) (Key, error) {
	var k Key

	// Parse modifiers.
	for utf8.RuneCountInString(s) > 1 {
		i := strings.IndexAny(s, "+-")
		if i == -1 {
			break
		}
		modname := s[:i]
		if mod, ok := modifierByName[modname]; ok {
			k.Mod |= mod
			s = s[i+1:]
		} else {
			return Key{}, fmt.Errorf("bad modifier: %s", parse.Quote(modname))
		}
	}

	if utf8.RuneCountInString(s) == 1 && utf8.ValidString(s) {
		k.Rune, _ = utf8.DecodeRuneInString(s)
		// Tab and Enter are their own keys rather than Ctrl-I and Ctrl-J,
		// which can be distinguished with the kitty keyboard protocol.
		if k.Rune < 0x20 && k.Rune != Tab && k.Rune != Enter {
			if k.Mod&Ctrl != 0 {
				//lint:ignore ST1005 We want this error to begin with "Ctrl" rather than "ctrl"
				// since the user has to use the capitalized form when creating a key binding.
				return Key{}, fmt.Errorf("Ctrl modifier with literal control char: %q", k.Rune)
			}
			// Convert literal control char to the equivalent canonical form,
			// e.g. "\e" to Ctrl-'['.
			k.Mod |= Ctrl
			k.Rune += 0x40
		}
		// Keys with Ctrl as one of the modifiers and a single ASCII letter as
		// the base rune do not distinguish between cases. So we normalize the
		// base rune to upper case.
		if k.Mod&Ctrl != 0 && 'a' <= k.Rune && k.Rune <= 'z' {
			k.Rune += 'A' - 'a'
		}
		return k, nil
	}

	// Is this is a symbolic key name, such as `Enter`, we recognize?
	for r, name := range keyNames {
		if s == name {
			k.Rune = r
			return k, nil
		}
	}

	return Key{}, fmt.Errorf("bad key: %s", parse.Quote(s))
}

// Keys implements sort.Interface.
type Keys []Key

func (ks Keys) Len() int      { return len(ks) }
func (ks Keys) Swap(i, j int) { ks[i], ks[j] = ks[j], ks[i] }
func (ks Keys) Less(i, j int) bool {
	return ks[i].Mod < ks[j].Mod ||
		(ks[i].Mod == ks[j].Mod && ks[i].Rune < ks[j].Rune)
}
