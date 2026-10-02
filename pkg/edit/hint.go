package edit

import (
	"context"
	"fmt"
	"os"
	"sync"

	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/ui"
)

// A hintSink collects the notes from code run by a background autosuggestion
// lookup, such as arg completers calling edit:notify to show the usage of the
// argument being completed. The collected notes are shown as the hint above
// the code area, instead of as notes that would repeat on every keystroke.
//
// A nil *hintSink means that the code is not run in the background, and notes
// should be shown as usual.
type hintSink struct {
	mu    sync.Mutex
	notes []ui.Text
}

func (s *hintSink) add(t ui.Text) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes = append(s.notes, t)
}

// Returns the collected notes, separated by newlines.
func (s *hintSink) text() ui.Text {
	s.mu.Lock()
	defer s.mu.Unlock()
	tb := new(ui.TextBuilder)
	for i, note := range s.notes {
		if i > 0 {
			tb.WriteText(ui.T("\n"))
		}
		tb.WriteText(note)
	}
	return tb.Text()
}

// Implements notifier, so that a hintSink can be used in place of the editor
// for notes about matchers.

func (s *hintSink) notifyf(format string, args ...any) {
	s.add(ui.T(fmt.Sprintf(format, args...)))
}

func (s *hintSink) notifyError(ctx string, e error) {
	s.notifyf("[%v error] %v", ctx, e)
}

type hintSinkKey struct{}

// Returns the context to run user code with: one carrying the sink if it's not
// nil, or nil to use the default.
func hintSinkContext(ctx context.Context, sink *hintSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, hintSinkKey{}, sink)
}

// Returns the hintSink carried by the context, or nil.
func hintSinkFrom(ctx context.Context) *hintSink {
	sink, _ := ctx.Value(hintSinkKey{}).(*hintSink)
	return sink
}

// Returns the stderr to run user code with: user code run in the background
// must not write onto the terminal.
func hintSinkStderr(sink *hintSink) *os.File {
	if sink != nil {
		return eval.DevNull
	}
	return os.Stderr
}
