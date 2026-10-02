package term

import (
	"src.elv.sh/pkg/ui"
)

// Event represents an event that can be read from the terminal.
type Event interface {
	isEvent()
}

// KeyEvent represents a key press.
type KeyEvent ui.Key

// KeyEventWithAlternates retains shortcut candidates in match-quality order:
// exact, shifted, base layout, and shifted base layout. KeyEvent remains the
// normalized key used for text insertion. Unused candidates are zero keys.
type KeyEventWithAlternates struct {
	KeyEvent
	Keys [4]ui.Key
}

// BindingKeyEvent asks handlers to try only an explicit binding for Key. It
// must not trigger text insertion, default bindings, or mode transitions.
type BindingKeyEvent struct {
	Key ui.Key
}

// KeyOf returns the primary key of a keyboard event.
func KeyOf(event Event) (ui.Key, bool) {
	switch event := event.(type) {
	case KeyEvent:
		return ui.Key(event), true
	case KeyEventWithAlternates:
		return ui.Key(event.KeyEvent), true
	default:
		return ui.Key{}, false
	}
}

// BindingKeys returns shortcut candidates in descending order of preference.
func BindingKeys(event Event) []ui.Key {
	if event, ok := event.(BindingKeyEvent); ok {
		return []ui.Key{event.Key}
	}
	if event, ok := event.(KeyEventWithAlternates); ok {
		var keys []ui.Key
		for _, key := range event.Keys {
			if key != (ui.Key{}) {
				keys = append(keys, key)
			}
		}
		return keys
	}
	if key, ok := KeyOf(event); ok {
		return []ui.Key{key}
	}
	return nil
}

// K constructs a new KeyEvent.
func K(r rune, mods ...ui.Mod) KeyEvent {
	return KeyEvent(ui.K(r, mods...))
}

// MouseEvent represents a mouse event (either pressing or releasing).
type MouseEvent struct {
	Pos
	Down bool
	// Number of the Button, 0-based. -1 for unknown.
	Button int
	Mod    ui.Mod
}

// CursorPosition represents a report of the current cursor position from the
// terminal driver, usually as a response from a cursor position request.
type CursorPosition Pos

// PasteSetting indicates the start or finish of pasted text.
type PasteSetting bool

// KittyKeyboardFlags is the terminal's response to a query for the kitty
// keyboard protocol's progressive enhancement flags. Receiving it means that
// the terminal supports the protocol; see [HandleKittyKeyboardFlags].
type KittyKeyboardFlags int

// FatalErrorEvent represents an error that affects the Reader's ability to
// continue reading events. After sending a FatalError, the Reader makes no more
// attempts at continuing to read events and wait for Stop to be called.
type FatalErrorEvent struct{ Err error }

// NonfatalErrorEvent represents an error that can be gradually recovered. After
// sending a NonfatalError, the Reader will continue to read events. Note that
// one anamoly in the terminal might cause multiple NonfatalError events to be
// sent.
type NonfatalErrorEvent struct{ Err error }

func (KeyEvent) isEvent()               {}
func (KeyEventWithAlternates) isEvent() {}
func (BindingKeyEvent) isEvent()        {}
func (MouseEvent) isEvent()             {}

func (CursorPosition) isEvent() {}
func (PasteSetting) isEvent()   {}

func (KittyKeyboardFlags) isEvent() {}

func (FatalErrorEvent) isEvent()    {}
func (NonfatalErrorEvent) isEvent() {}
