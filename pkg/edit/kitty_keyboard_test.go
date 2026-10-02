package edit

import (
	"sync/atomic"
	"testing"
	"time"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/edit/complete"
	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/eval/vals"
	"src.elv.sh/pkg/ui"
)

func TestKittyInterruptsPrompt(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	f := setup(t, func(f *fixture) {
		f.Evaler.ExtendGlobal(eval.BuildNs().AddGoFn("first-prompt", func() bool {
			first := calls.Add(1) == 1
			if first {
				close(started)
			}
			return first
		}))
	}, rc(`set edit:prompt = { if (first-prompt) { sleep 60 }; put '~> ' }`))
	t.Cleanup(eval.Interrupt)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("prompt did not start")
	}
	f.TTYCtrl.Inject(term.K('C', ui.Ctrl))
	f.TestTTY(t, "~> ", term.DotHere)
	for _, msg := range f.TTYCtrl.MsgHistory() {
		if len(msg) != 0 {
			t.Errorf("interrupted prompt notified: %v", msg)
		}
	}
	evals(f.Evaler, `var excs = (count $edit:exceptions)`)
	testGlobal(t, f.Evaler, "excs", 0)
}

func TestInterruptedForegroundEvaluationDoesNotNotify(t *testing.T) {
	for _, test := range []struct {
		name, config, input string
		key                 term.KeyEvent
	}{
		{"binding", `set edit:insert:binding[Ctrl-X] = { started; sleep 60 }`, "", term.K('X', ui.Ctrl)},
		{"generator", `set edit:completion:arg-completer[echo] = {|@args| started; sleep 60 }`, "echo ", term.K(ui.Tab)},
		{"matcher", `set edit:completion:arg-completer[echo] = {|@args| put candidate }
			set edit:completion:matcher[''] = {|seed| started; sleep 60 }`, "echo ", term.K(ui.Tab)},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			f := setup(t, func(f *fixture) {
				f.Evaler.ExtendGlobal(eval.BuildNs().AddGoFn("started", func() { close(started) }))
			}, rc(`set edit:autosuggestion:enabled = $false`, test.config))
			t.Cleanup(eval.Interrupt)
			feedInput(f.TTYCtrl, test.input)
			f.TTYCtrl.Inject(test.key)
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("evaluation did not start")
			}
			// Synchronous evaluations use legacy input and are interrupted
			// through their listening contexts while the event loop is blocked.
			eval.Interrupt()
			f.TTYCtrl.Inject(term.K(ui.Enter))
			f.Wait()
			for _, msg := range f.TTYCtrl.MsgHistory() {
				if len(msg) != 0 {
					t.Errorf("interrupted evaluation notified: %v", msg)
				}
			}
			evals(f.Evaler, `var excs = (count $edit:exceptions)`)
			testGlobal(t, f.Evaler, "excs", 0)
		})
	}
}

func TestInterruptedBackgroundMatcherDoesNotNotify(t *testing.T) {
	started := make(chan struct{})
	matcher := eval.NewGoFn("blocking-matcher", func(fm *eval.Frame, seed string) error {
		close(started)
		<-fm.Context().Done()
		return eval.ErrInterrupted
	})
	sink := new(hintSink)
	filter := adaptMatcherMap(sink, eval.NewEvaler(), sink, vals.EmptyMap.Assoc("", matcher))
	done := make(chan []complete.RawItem, 1)
	go func() { done <- filter("argument", "", []complete.RawItem{complete.PlainItem("candidate")}) }()
	t.Cleanup(eval.Interrupt)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("matcher did not start")
	}
	eval.Interrupt()
	select {
	case items := <-done:
		if len(items) != 0 {
			t.Errorf("interrupted matcher returned %v", items)
		}
	case <-time.After(time.Second):
		t.Fatal("matcher did not stop")
	}
	if hint := sink.text(); len(hint) != 0 {
		t.Errorf("interrupted matcher left a hint: %v", hint)
	}
}

func TestKittyInterruptsBackgroundCompletion(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	f := setup(t, func(f *fixture) {
		f.Evaler.ExtendGlobal(eval.BuildNs().AddGoFn("blocking-completer", func(fm *eval.Frame, args ...any) error {
			close(started)
			<-fm.Context().Done()
			close(stopped)
			return eval.ErrInterrupted
		}))
	}, rc(`set edit:completion:arg-completer[echo] = $blocking-completer~`))
	t.Cleanup(eval.Interrupt)
	feedInput(f.TTYCtrl, "echo x")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("completion did not start")
	}
	f.TTYCtrl.Inject(term.K('\\', ui.Ctrl))
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Ctrl-Backslash did not cancel the completion context")
	}
}
