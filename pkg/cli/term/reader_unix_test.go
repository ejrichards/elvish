//go:build unix

package term

import (
	"os"
	"strings"
	"testing"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/ui"
)

var readEventTests = []struct {
	input string
	want  Event
}{
	// Simple graphical key.
	{"x", K('x')},
	{"X", K('X')},
	{" ", K(' ')},

	// Ctrl key.
	{"\001", K('A', ui.Ctrl)},
	{"\033", K('[', ui.Ctrl)},

	// Special Ctrl keys that do not obey the usual 0x40 rule.
	{"\000", K(' ', ui.Ctrl)},
	{"\x1e", K('6', ui.Ctrl)},
	{"\x1f", K('/', ui.Ctrl)},

	// Ambiguous Ctrl keys; the reader uses the non-Ctrl form as canonical.
	{"\n", K('\n')},
	{"\t", K('\t')},
	{"\x7f", K('\x7f')}, // backspace

	// Alt plus simple graphical key.
	{"\033a", K('a', ui.Alt)},
	{"\033[", K('[', ui.Alt)},

	// G3-style key.
	{"\033OA", K(ui.Up)},
	{"\033OH", K(ui.Home)},

	// G3-style key with leading Escape.
	{"\033\033OA", K(ui.Up, ui.Alt)},
	{"\033\033OH", K(ui.Home, ui.Alt)},

	// Alt-O. This is handled as a special case because it looks like a G3-style
	// key.
	{"\033O", K('O', ui.Alt)},

	// CSI-sequence key identified by the ending rune.
	{"\033[A", K(ui.Up)},
	{"\033[H", K(ui.Home)},
	// Modifiers.
	{"\033[1;0A", K(ui.Up)},
	{"\033[1;1A", K(ui.Up)},
	{"\033[1;2A", K(ui.Up, ui.Shift)},
	{"\033[1;3A", K(ui.Up, ui.Alt)},
	{"\033[1;4A", K(ui.Up, ui.Shift, ui.Alt)},
	{"\033[1;5A", K(ui.Up, ui.Ctrl)},
	{"\033[1;6A", K(ui.Up, ui.Shift, ui.Ctrl)},
	{"\033[1;7A", K(ui.Up, ui.Alt, ui.Ctrl)},
	{"\033[1;8A", K(ui.Up, ui.Shift, ui.Alt, ui.Ctrl)},
	// The modifiers below should be for Meta, but we conflate Alt and Meta.
	{"\033[1;9A", K(ui.Up, ui.Alt)},
	{"\033[1;10A", K(ui.Up, ui.Shift, ui.Alt)},
	{"\033[1;11A", K(ui.Up, ui.Alt)},
	{"\033[1;12A", K(ui.Up, ui.Shift, ui.Alt)},
	{"\033[1;13A", K(ui.Up, ui.Alt, ui.Ctrl)},
	{"\033[1;14A", K(ui.Up, ui.Shift, ui.Alt, ui.Ctrl)},
	{"\033[1;15A", K(ui.Up, ui.Alt, ui.Ctrl)},
	{"\033[1;16A", K(ui.Up, ui.Shift, ui.Alt, ui.Ctrl)},

	// CSI-sequence key with one argument, ending in '~'.
	{"\033[1~", K(ui.Home)},
	{"\033[11~", K(ui.F1)},
	// Modified.
	{"\033[1;2~", K(ui.Home, ui.Shift)},
	// Urxvt-flavor modifier, shifting the '~' to reflect the modifier
	{"\033[1$", K(ui.Home, ui.Shift)},
	{"\033[1^", K(ui.Home, ui.Ctrl)},
	{"\033[1@", K(ui.Home, ui.Shift, ui.Ctrl)},
	// With a leading Escape.
	{"\033\033[1~", K(ui.Home, ui.Alt)},

	// CSI-sequence key with three arguments and ending in '~'. The first
	// argument is always 27, the second identifies the modifier and the last
	// identifies the key.
	{"\033[27;4;63~", K(';', ui.Shift, ui.Alt)},

	// F1, F2 and F4 encoded with the final rune, used by xterm when modified
	// and the kitty keyboard protocol.
	{"\033[P", K(ui.F1)},
	{"\033[1;5Q", K(ui.F2, ui.Ctrl)},
	{"\033[1;2S", K(ui.F4, ui.Shift)},

	// Kitty keyboard protocol.
	{"\033[?5u", KittyKeyboardFlags(5)},
	{"\033[?u", KittyKeyboardFlags(0)},
	// Escape is unambiguous.
	{"\033[27u", K('[', ui.Ctrl)},
	{"\033[27;3u", K('[', ui.Ctrl, ui.Alt)},
	// Ctrl and Alt with letters, normalized like the legacy encoding.
	{"\033[97;5u", K('A', ui.Ctrl)},
	{"\033[97;3u", K('a', ui.Alt)},
	{"\033[97;7u", K('A', ui.Ctrl, ui.Alt)},
	// Shift is applied to the key, using the shifted key if reported.
	{"\033[97:65;4u", KeyEventWithAlternates{K('A', ui.Alt), [4]ui.Key{ui.K('a', ui.Alt, ui.Shift), ui.K('A', ui.Alt)}}},
	{"\033[97;4u", KeyEventWithAlternates{K('A', ui.Alt), [4]ui.Key{ui.K('a', ui.Alt, ui.Shift), ui.K('A', ui.Alt)}}},
	{"\033[50:64;6u", KeyEventWithAlternates{K('@', ui.Ctrl), [4]ui.Key{ui.K('2', ui.Ctrl, ui.Shift), ui.K('@', ui.Ctrl)}}},
	// Ctrl-Shift-letter is distinct from Ctrl-letter.
	{"\033[97:65;6u", K('A', ui.Ctrl, ui.Shift)},
	// Preserve Shift when no shifted representation is available.
	{"\033[32;6u", K(' ', ui.Ctrl, ui.Shift)},
	{"\033[49;6u", K('1', ui.Ctrl, ui.Shift)},
	{"\033[228:196;6u", KeyEventWithAlternates{K('ä', ui.Ctrl, ui.Shift), [4]ui.Key{ui.K('ä', ui.Ctrl, ui.Shift), ui.K('Ä', ui.Ctrl)}}},
	// Keys that can't be distinguished in the legacy encoding.
	{"\033[13;2u", K(ui.Enter, ui.Shift)},
	{"\033[13;5u", K(ui.Enter, ui.Ctrl)},
	{"\033[13;3u", K(ui.Enter, ui.Alt)},
	{"\033[127;5u", K(ui.Backspace, ui.Ctrl)},
	{"\033[9;5u", K(ui.Tab, ui.Ctrl)},
	{"\033[49;5u", K('1', ui.Ctrl)},
	// Keys conflated in the legacy encoding are distinct.
	{"\033[105;5u", K('I', ui.Ctrl)},
	{"\033[106;5u", K('J', ui.Ctrl)},
	{"\033[109;5u", K('M', ui.Ctrl)},
	{"\033[32;5u", K(' ', ui.Ctrl)},
	{"\033[96;5u", K('`', ui.Ctrl)},
	// Preserve both layout identities, including for ASCII layouts.
	{"\033[1094::119;5u", KeyEventWithAlternates{K('ц', ui.Ctrl), [4]ui.Key{ui.K('ц', ui.Ctrl), {}, ui.K('W', ui.Ctrl)}}},
	{"\033[1094:1062:119;4u", KeyEventWithAlternates{K('Ц', ui.Alt), [4]ui.Key{ui.K('ц', ui.Alt, ui.Shift), ui.K('Ц', ui.Alt), ui.K('w', ui.Alt, ui.Shift), ui.K('W', ui.Alt)}}},
	{"\033[1094:1062:119;6u", KeyEventWithAlternates{K('ц', ui.Ctrl, ui.Shift), [4]ui.Key{ui.K('ц', ui.Ctrl, ui.Shift), ui.K('Ц', ui.Ctrl), ui.K('W', ui.Ctrl, ui.Shift)}}},
	{"\033[122::121;5u", KeyEventWithAlternates{K('Z', ui.Ctrl), [4]ui.Key{ui.K('Z', ui.Ctrl), {}, ui.K('Y', ui.Ctrl)}}},
	// Do not attach redundant or unmodified base-layout shortcuts.
	{"\033[97::97;5u", K('A', ui.Ctrl)},
	{"\033[1094::119u", K('ц')},
	// Caps Lock and Num Lock are ignored.
	{"\033[97;69u", K('A', ui.Ctrl)},
	{"\033[97;133u", K('A', ui.Ctrl)},
	{"\033[97;66u", K('a')}, // Caps Lock cancels Shift for text letters.
	{"\033[228;66u", K('ä')},
	// Meta is conflated with Alt.
	{"\033[97;33u", K('a', ui.Alt)},
	// Keypad keys.
	{"\033[57399u", K('0')},
	{"\033[57414u", K(ui.Enter)},
	{"\033[57419;5u", K(ui.Up, ui.Ctrl)},
	{"\033[E", K('5')},
	{"\033[1;5E", K('5', ui.Ctrl)},
	{"\033[57427~", K('5')},
	// Lock, media and modifier keys are skipped.
	{"\033[57441ux", K('x')},
	{"\033[57358;65u\033[57416ux", K('x')},
	{"\033[97;9ux", K('x')},  // Super
	{"\033[97;17ux", K('x')}, // Hyper
	{"\033[8;9ux", K('x')},   // Super-Backspace
	{"\033[127;9ux", K('x')}, // Super-Backspace (DEL)
	{"\033[9;9ux", K('x')},   // Super-Tab
	{"\033[13;9ux", K('x')},  // Super-Enter
	{"\033[27;9ux", K('x')},  // Super-Escape
	{"\033[57376ux", K('x')}, // F13
	{"\033[57361ux", K('x')}, // Print Screen
	{"\033[57363ux", K('x')}, // Menu
	{"\033[57427ux", K('x')}, // Unrepresentable functional key
	// Event type and text fields are ignored.
	{"\033[97;5:1u", K('A', ui.Ctrl)},
	{"\033[97;3;97u", K('a', ui.Alt)},
	// Sub-parameters are ignored for legacy sequences.
	{"\033[1;5:1A", K(ui.Up, ui.Ctrl)},

	// Cursor Position Report.
	{"\033[3;4R", CursorPosition{3, 4}},

	// Paste setting.
	{"\033[200~", PasteSetting(true)},
	{"\033[201~", PasteSetting(false)},

	// Mouse event.
	{"\033[M\x00\x23\x24", MouseEvent{Pos{4, 3}, true, 0, 0}},
	// Other buttons.
	{"\033[M\x01\x23\x24", MouseEvent{Pos{4, 3}, true, 1, 0}},
	// Button up.
	{"\033[M\x03\x23\x24", MouseEvent{Pos{4, 3}, false, -1, 0}},
	// Modified.
	{"\033[M\x04\x23\x24", MouseEvent{Pos{4, 3}, true, 0, ui.Shift}},
	{"\033[M\x08\x23\x24", MouseEvent{Pos{4, 3}, true, 0, ui.Alt}},
	{"\033[M\x10\x23\x24", MouseEvent{Pos{4, 3}, true, 0, ui.Ctrl}},
	{"\033[M\x14\x23\x24", MouseEvent{Pos{4, 3}, true, 0, ui.Shift | ui.Ctrl}},

	// SGR-style mouse event.
	{"\033[<0;3;4M", MouseEvent{Pos{4, 3}, true, 0, 0}},
	// Other buttons.
	{"\033[<1;3;4M", MouseEvent{Pos{4, 3}, true, 1, 0}},
	// Button up.
	{"\033[<0;3;4m", MouseEvent{Pos{4, 3}, false, 0, 0}},
	// Modified.
	{"\033[<4;3;4M", MouseEvent{Pos{4, 3}, true, 0, ui.Shift}},
	{"\033[<16;3;4M", MouseEvent{Pos{4, 3}, true, 0, ui.Ctrl}},
}

func TestReader_ReadEvent(t *testing.T) {
	r, w := setupReader(t)

	for _, test := range readEventTests {
		t.Run(test.input, func(t *testing.T) {
			w.WriteString(test.input)
			ev, err := r.ReadEvent()
			if ev != test.want {
				t.Errorf("got event %v, want %v", ev, test.want)
			}
			if err != nil {
				t.Errorf("got err %v, want %v", err, nil)
			}
		})
	}
}

var readEventBadSeqTests = []struct {
	input      string
	wantErrMsg string
}{
	// mouse event should have exactly 3 bytes after \033[M
	{"\033[M", "incomplete mouse event"},
	{"\033[M1", "incomplete mouse event"},
	{"\033[M12", "incomplete mouse event"},

	// CSI needs to be terminated by something that is not a parameter
	{"\033[1", "incomplete CSI"},
	{"\033[;", "incomplete CSI"},
	{"\033[1;", "incomplete CSI"},

	// CPR should have exactly 2 parameters
	{"\033[1R", "bad CPR"},
	{"\033[1;2;3R", "bad CPR"},

	// SGR mouse event should have exactly 3 parameters
	{"\033[<1;2m", "bad SGR mouse event"},

	// csiSeqByLast should have 0 or 2 parameters
	{"\033[1;2;3A", "bad CSI"},
	// csiSeqByLast with 2 parameters should have first parameter = 1
	{"\033[2;1A", "bad CSI"},
	// xterm-style modifier should be 0 to 16
	{"\033[1;17A", "bad CSI"},
	// unknown CSI terminator
	{"\033[x", "bad CSI"},

	// Private CSI only supports the kitty keyboard protocol response
	{"\033[?62;22c", "bad private CSI"},
	{"\033[?1;2u", "bad private CSI"},

	// Kitty keyboard protocol keys that can't be represented
	{"\033[u", "bad CSI"},
	{"\033[97;1;2;3u", "bad CSI"}, // too many parameters
	{"\033[1u", "bad CSI"},        // control character
	{"\033[1114112u", "bad CSI"},  // outside Unicode
	{"\033[55296u", "bad CSI"},    // surrogate
	{"\033[55296;9u", "bad CSI"},  // Unsupported modifiers must not hide malformed Unicode.
	{"\033[97:55296;4u", "bad CSI"},
	{"\033[97::1114112;5u", "bad CSI"},
	{"\033[97;1;55296u", "bad CSI"},
	{"\033[97;257u", "bad CSI"},        // unknown modifier bit
	{"\033[57358;257u", "bad CSI"},     // Invalid modifiers on an ignored key.
	{"\033[57358;1;55296u", "bad CSI"}, // Invalid text on an ignored key.
	{"\033[57427;257u", "bad CSI"},     // Invalid modifiers on an unsupported key.
	{"\033[1;9u", "bad CSI"},           // Unsupported modifiers must not hide invalid control characters.
	{"\033[97:65:97:98u", "bad CSI"},
	{"\033[4294967393u", "CSI parameter overflow"},
	{"\033[18446744073709551616u", "CSI parameter overflow"},

	// G3 allows a small list of allowed bytes after \033O
	{"\033Ox", "bad G3"},
}

func TestReader_ReadEvent_BadSeq(t *testing.T) {
	r, w := setupReader(t)

	for _, test := range readEventBadSeqTests {
		t.Run(test.input, func(t *testing.T) {
			w.WriteString(test.input)
			ev, err := r.ReadEvent()
			if err == nil {
				t.Fatalf("got nil err with event %v, want non-nil error", ev)
			}
			errMsg := err.Error()
			if !strings.HasPrefix(errMsg, test.wantErrMsg) {
				t.Errorf("got err with message %v, want message starting with %v",
					errMsg, test.wantErrMsg)
			}
		})
	}
}

func TestReader_ReadRawEvent(t *testing.T) {
	rd, w := setupReader(t)

	for _, test := range readEventTests {
		input := test.input
		t.Run(input, func(t *testing.T) {
			w.WriteString(input)
			for _, r := range input {
				ev, err := rd.ReadRawEvent()
				if err != nil {
					t.Errorf("got error %v, want nil", err)
				}
				if ev != K(r) {
					t.Errorf("got event %v, want %v", ev, K(r))
				}
			}
		})
	}
}

func TestReader_KittyFunctionalModifiers(t *testing.T) {
	setupKittyKeyboard(t, false)
	kittyKeyboard.active = true
	r, w := setupReader(t)
	for _, test := range []struct {
		input string
		want  Event
	}{
		{"\033[1;65A", K(ui.Up)},
		{"\033[1;69A", K(ui.Up, ui.Ctrl)},
		{"\033[3;133~", K(ui.Delete, ui.Ctrl)},
		{"\033[1;33Q", K(ui.F2, ui.Alt)},
	} {
		w.WriteString(test.input)
		if got, err := r.ReadEvent(); got != test.want || err != nil {
			t.Errorf("ReadEvent(%q) = %v, %v; want %v, nil", test.input, got, err, test.want)
		}
	}
	for _, input := range []string{"\033[1;9A", "\033[3;17~", "\033[1;9P"} {
		w.WriteString(input + "x")
		if got, err := r.ReadEvent(); got != K('x') || err != nil {
			t.Errorf("ReadEvent(%q) = %v, %v; want x, nil", input, got, err)
		}
	}
	for _, input := range []string{"\033[1;257P", "\033[2;9A", "\033[999;9~"} {
		w.WriteString(input)
		if got, err := r.ReadEvent(); got != nil || err == nil {
			t.Errorf("ReadEvent(%q) = %v, %v; want rejection", input, got, err)
		}
	}
}

func TestReader_OverflowConsumesSequence(t *testing.T) {
	r, w := setupReader(t)
	w.WriteString("\033[4294967393ux")
	if _, err := r.ReadEvent(); err == nil {
		t.Fatal("expected parameter overflow")
	}
	if got, err := r.ReadEvent(); got != K('x') || err != nil {
		t.Errorf("next event = %v, %v; want x, nil", got, err)
	}
}

func setupReader(t *testing.T) (Reader, *os.File) {
	pr, pw := must.Pipe()
	r := NewReader(pr)
	t.Cleanup(func() {
		r.Close()
		pr.Close()
		pw.Close()
	})
	return r, pw
}
