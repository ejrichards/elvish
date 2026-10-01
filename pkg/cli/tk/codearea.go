package tk

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/ui"
)

// CodeArea is a Widget for displaying and editing code.
type CodeArea interface {
	Widget
	// CopyState returns a copy of the state.
	CopyState() CodeAreaState
	// MutateState calls the given the function while locking StateMutex.
	MutateState(f func(*CodeAreaState))
	// Submit triggers the OnSubmit callback.
	Submit()
	// AcceptSuggestion accepts the suggestion shown in the last render, up to
	// the position returned by upTo, which is called with the suggested full
	// code and the length of the buffer content. If the whole suggestion is
	// accepted, the buffer is replaced by the suggestion, which may correct
	// the case of the typed code; otherwise the accepted part is appended
	// to the buffer. The dot is moved to the end. It returns whether anything
	// was accepted. Nothing is accepted if no suggestion was shown, if the
	// buffer has changed since the last render, or if upTo does not move
	// forward.
	AcceptSuggestion(upTo func(full string, from int) int) bool
}

// CodeAreaSpec specifies the configuration and initial state for CodeArea.
type CodeAreaSpec struct {
	// Key bindings.
	Bindings Bindings
	// A function that highlights the given code and returns any tips it has
	// found, such as errors and autofixes. If this function is not given, the
	// Widget does not highlight the code nor show any tips.
	Highlighter func(code string) (ui.Text, []ui.Text)
	// A function that returns a suggestion for the given code, like the
	// autosuggestion feature of the Fish shell. The return value is the
	// suggested full code, which must be longer than the given code and have
	// it as a prefix (compared case-insensitively); the part after the prefix
	// is shown after the code in a different style. Accepting the whole
	// suggestion replaces the code with the suggestion, which may correct the
	// case of the typed code. It is only called when there is no pending
	// code. If this function is not given, the Widget does not show
	// suggestions.
	Suggester func(code string) string
	// Prompt callback.
	Prompt func() ui.Text
	// Right-prompt callback.
	RPrompt func() ui.Text
	// A function that calls the callback with string pairs for abbreviations
	// and their expansions. If no function is provided the Widget does not
	// expand any abbreviations of the specified type.
	SimpleAbbreviations    func(f func(abbr, full string))
	CommandAbbreviations   func(f func(abbr, full string))
	SmallWordAbbreviations func(f func(abbr, full string))
	// A function that returns whether pasted texts (from bracketed pastes)
	// should be quoted. If this function is not given, the Widget defaults to
	// not quoting pasted texts.
	QuotePaste func() bool
	// A function that is called on the submit event.
	OnSubmit func()

	// State. When used in New, this field specifies the initial state.
	State CodeAreaState
}

// CodeAreaState keeps the mutable state of the CodeArea widget.
type CodeAreaState struct {
	Buffer         CodeBuffer
	Pending        PendingCode
	HideRPrompt    bool
	HideTips       bool
	HideSuggestion bool
}

// CodeBuffer represents the buffer of the CodeArea widget.
type CodeBuffer struct {
	// Content of the buffer.
	Content string
	// Position of the dot (more commonly known as the cursor), as a byte index
	// into Content.
	Dot int
}

// PendingCode represents pending code, such as during completion.
type PendingCode struct {
	// Beginning index of the text area that the pending code replaces, as a
	// byte index into RawState.Code.
	From int
	// End index of the text area that the pending code replaces, as a byte
	// index into RawState.Code.
	To int
	// The content of the pending code.
	Content string
}

// ApplyPending applies pending code to the code buffer, and resets pending code.
func (s *CodeAreaState) ApplyPending() {
	s.Buffer, _, _ = patchPending(s.Buffer, s.Pending)
	s.Pending = PendingCode{}
}

func (c *CodeBuffer) InsertAtDot(text string) {
	*c = CodeBuffer{
		Content: c.Content[:c.Dot] + text + c.Content[c.Dot:],
		Dot:     c.Dot + len(text),
	}
}

type codeArea struct {
	// Mutex for synchronizing access to State.
	StateMutex sync.RWMutex
	// Configuration and state.
	CodeAreaSpec

	// Consecutively inserted text. Used for expanding abbreviations.
	inserts string
	// Value of State.CodeBuffer when handleKeyEvent was last called. Used for
	// detecting whether insertion has been interrupted.
	lastCodeBuffer CodeBuffer
	// Whether the widget is in the middle of bracketed pasting.
	pasting bool
	// Buffer for keeping Pasted text during bracketed pasting.
	pasteBuffer bytes.Buffer

	// Mutex for synchronizing access to shownFor and shown.
	shownMutex sync.Mutex
	// Value of State.Buffer when the suggestion was last rendered.
	shownFor CodeBuffer
	// The suggested full code that was last rendered; empty if none was
	// shown.
	shown string
}

// NewCodeArea creates a new CodeArea from the given spec.
func NewCodeArea(spec CodeAreaSpec) CodeArea {
	if spec.Bindings == nil {
		spec.Bindings = DummyBindings{}
	}
	if spec.Highlighter == nil {
		spec.Highlighter = func(s string) (ui.Text, []ui.Text) { return ui.T(s), nil }
	}
	if spec.Suggester == nil {
		spec.Suggester = func(string) string { return "" }
	}
	if spec.Prompt == nil {
		spec.Prompt = func() ui.Text { return nil }
	}
	if spec.RPrompt == nil {
		spec.RPrompt = func() ui.Text { return nil }
	}
	if spec.SimpleAbbreviations == nil {
		spec.SimpleAbbreviations = func(func(a, f string)) {}
	}
	if spec.CommandAbbreviations == nil {
		spec.CommandAbbreviations = func(func(a, f string)) {}
	}
	if spec.SmallWordAbbreviations == nil {
		spec.SmallWordAbbreviations = func(func(a, f string)) {}
	}
	if spec.QuotePaste == nil {
		spec.QuotePaste = func() bool { return false }
	}
	if spec.OnSubmit == nil {
		spec.OnSubmit = func() {}
	}
	return &codeArea{CodeAreaSpec: spec}
}

// Submit emits a submit event with the current code content.
func (w *codeArea) Submit() {
	w.OnSubmit()
}

// Render renders the code area, including the prompt and rprompt, highlighted
// code, the cursor, and compilation errors in the code content.
func (w *codeArea) Render(width, height int) *term.Buffer {
	b := w.render(width)
	truncateToHeight(b, height)
	return b
}

func (w *codeArea) MaxHeight(width, height int) int {
	return len(w.render(width).Lines)
}

func (w *codeArea) render(width int) *term.Buffer {
	view := getView(w)
	bb := term.NewBufferBuilder(width)
	renderView(view, bb)
	return bb.Buffer()
}

// Handle handles KeyEvent's of non-function keys, as well as PasteSetting
// events.
func (w *codeArea) Handle(event term.Event) bool {
	switch event := event.(type) {
	case term.PasteSetting:
		return w.handlePasteSetting(bool(event))
	case term.KeyEvent:
		return w.handleKeyEvent(ui.Key(event))
	}
	return false
}

func (w *codeArea) MutateState(f func(*CodeAreaState)) {
	w.StateMutex.Lock()
	defer w.StateMutex.Unlock()
	f(&w.State)
}

func (w *codeArea) CopyState() CodeAreaState {
	w.StateMutex.RLock()
	defer w.StateMutex.RUnlock()
	return w.State
}

func (w *codeArea) AcceptSuggestion(upTo func(full string, dot int) int) bool {
	w.StateMutex.Lock()
	defer w.StateMutex.Unlock()
	w.shownMutex.Lock()
	shownFor, shown := w.shownFor, w.shown
	w.shownMutex.Unlock()

	if shown == "" || w.State.Buffer != shownFor || w.State.Pending != (PendingCode{}) {
		return false
	}
	content := w.State.Buffer.Content
	from := len(content)
	to := upTo(shown, from)
	if to <= from || to > len(shown) {
		return false
	}
	if to == len(shown) {
		// Accepting the whole suggestion also corrects the case of the typed
		// code, if the suggestion matched case-insensitively.
		content = shown
	} else {
		content += shown[from:to]
	}
	w.State.Buffer = CodeBuffer{Content: content, Dot: len(content)}
	return true
}

// Returns the part of the suggested full code to show after the code, or ""
// if the suggestion is not valid for the code.
func suggestionSuffix(code, full string) string {
	if len(full) <= len(code) || !strings.EqualFold(full[:len(code)], code) {
		return ""
	}
	return full[len(code):]
}

func (w *codeArea) setShownSuggestion(buf CodeBuffer, sug string) {
	w.shownMutex.Lock()
	defer w.shownMutex.Unlock()
	w.shownFor, w.shown = buf, sug
}

func (w *codeArea) resetInserts() {
	w.inserts = ""
	w.lastCodeBuffer = CodeBuffer{}
}

func (w *codeArea) handlePasteSetting(start bool) bool {
	w.resetInserts()
	if start {
		w.pasting = true
	} else {
		text := w.pasteBuffer.String()
		if w.QuotePaste() {
			text = parse.Quote(text)
		}
		w.MutateState(func(s *CodeAreaState) { s.Buffer.InsertAtDot(text) })

		w.pasting = false
		w.pasteBuffer = bytes.Buffer{}
	}
	return true
}

// Tries to expand a simple abbreviation. This function assumes the state mutex is held.
func (w *codeArea) expandSimpleAbbr() {
	var abbr, full string
	// Find the longest matching abbreviation.
	w.SimpleAbbreviations(func(a, f string) {
		if strings.HasSuffix(w.inserts, a) && len(a) > len(abbr) {
			abbr, full = a, f
		}
	})
	if len(abbr) > 0 {
		buf := &w.State.Buffer
		*buf = CodeBuffer{
			Content: buf.Content[:buf.Dot-len(abbr)] + full + buf.Content[buf.Dot:],
			Dot:     buf.Dot - len(abbr) + len(full),
		}
		w.resetInserts()
	}
}

var commandRegex = regexp.MustCompile(`(?:^|[^^]\n|\||;|{\s|\()\s*([\p{L}\p{M}\p{N}!%+,\-./:@\\_<>*]+)(\s)$`)

// Tries to expand a command abbreviation. This function assumes the state mutex
// is held.
//
// We use a regex rather than parse.Parse() because dealing with the latter
// requires a lot of code. A simple regex is far simpler and good enough for
// this use case. The regex essentially matches commands at the start of the
// line (with potential leading whitespace) and similarly after the opening
// brace of a lambda or pipeline char.
//
// This only handles bareword commands.
func (w *codeArea) expandCommandAbbr() {
	buf := &w.State.Buffer
	if buf.Dot < len(buf.Content) {
		// Command abbreviations are only expanded when inserting at the end of the buffer.
		return
	}

	// See if there is something that looks like a bareword at the end of the buffer.
	matches := commandRegex.FindStringSubmatch(buf.Content)
	if len(matches) == 0 {
		return
	}

	// Find an abbreviation matching the command.
	command, whitespace := matches[1], matches[2]
	var expansion string
	w.CommandAbbreviations(func(a, e string) {
		if a == command {
			expansion = e
		}
	})
	if expansion == "" {
		return
	}

	// We found a matching abbreviation -- replace it with its expansion.
	newContent := buf.Content[:buf.Dot-len(command)-1] + expansion + whitespace
	*buf = CodeBuffer{
		Content: newContent,
		Dot:     len(newContent),
	}
	w.resetInserts()
}

// Try to expand a small word abbreviation. This function assumes the state mutex is held.
func (w *codeArea) expandSmallWordAbbr(trigger rune, categorizer func(rune) int) {
	buf := &w.State.Buffer
	if buf.Dot < len(buf.Content) {
		// Word abbreviations are only expanded when inserting at the end of the buffer.
		return
	}
	triggerLen := len(string(trigger))
	if triggerLen >= len(w.inserts) {
		// Only the trigger has been inserted, or a simple abbreviation was just
		// expanded. In either case, there is nothing to expand.
		return
	}
	// The trigger is only used to determine word boundary; when considering
	// what to expand, we only consider the part that was inserted before it.
	inserts := w.inserts[:len(w.inserts)-triggerLen]

	var abbr, full string
	// Find the longest matching abbreviation.
	w.SmallWordAbbreviations(func(a, f string) {
		if len(a) <= len(abbr) {
			// This abbreviation can't be the longest.
			return
		}
		if !strings.HasSuffix(inserts, a) {
			// This abbreviation was not inserted.
			return
		}
		// Verify the trigger rune creates a word boundary.
		r, _ := utf8.DecodeLastRuneInString(a)
		if categorizer(trigger) == categorizer(r) {
			return
		}
		// Verify the rune preceding the abbreviation, if any, creates a word
		// boundary.
		if len(buf.Content) > len(a)+triggerLen {
			r1, _ := utf8.DecodeLastRuneInString(buf.Content[:len(buf.Content)-len(a)-triggerLen])
			r2, _ := utf8.DecodeRuneInString(a)
			if categorizer(r1) == categorizer(r2) {
				return
			}
		}
		abbr, full = a, f
	})
	if len(abbr) > 0 {
		*buf = CodeBuffer{
			Content: buf.Content[:buf.Dot-len(abbr)-triggerLen] + full + string(trigger),
			Dot:     buf.Dot - len(abbr) + len(full),
		}
		w.resetInserts()
	}
}

func (w *codeArea) handleKeyEvent(key ui.Key) bool {
	isFuncKey := key.Mod != 0 || key.Rune < 0
	if w.pasting {
		if isFuncKey {
			// TODO: Notify the user of the error, or insert the original
			// character as is.
		} else {
			w.pasteBuffer.WriteRune(key.Rune)
		}
		return true
	}

	if w.Bindings.Handle(w, term.KeyEvent(key)) {
		return true
	}

	// We only implement essential keybindings here. Other keybindings can be
	// added via handler overlays.
	switch key {
	case ui.K('\n'):
		w.resetInserts()
		w.Submit()
		return true
	case ui.K(ui.Backspace), ui.K('H', ui.Ctrl):
		w.resetInserts()
		w.MutateState(func(s *CodeAreaState) {
			c := &s.Buffer
			// Remove the last rune.
			_, chop := utf8.DecodeLastRuneInString(c.Content[:c.Dot])
			*c = CodeBuffer{
				Content: c.Content[:c.Dot-chop] + c.Content[c.Dot:],
				Dot:     c.Dot - chop,
			}
		})
		return true
	default:
		if isFuncKey || !unicode.IsGraphic(key.Rune) {
			w.resetInserts()
			return false
		}
		w.StateMutex.Lock()
		defer w.StateMutex.Unlock()
		if w.lastCodeBuffer != w.State.Buffer {
			// Something has happened between the last insert and this one;
			// reset the state.
			w.resetInserts()
		}
		s := string(key.Rune)
		w.State.Buffer.InsertAtDot(s)
		w.inserts += s
		w.lastCodeBuffer = w.State.Buffer
		if parse.IsWhitespace(key.Rune) {
			w.expandCommandAbbr()
		}
		w.expandSimpleAbbr()
		w.expandSmallWordAbbr(key.Rune, CategorizeSmallWord)
		return true
	}
}

// IsAlnum determines if the rune is an alphanumeric character.
func IsAlnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

// CategorizeSmallWord determines if the rune is whitespace, alphanum, or
// something else.
func CategorizeSmallWord(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case IsAlnum(r):
		return 1
	default:
		return 2
	}
}
