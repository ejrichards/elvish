//go:build unix

package sys

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals() chan os.Signal {
	// This catches every signal regardless of whether it is ignored.
	sigCh := make(chan os.Signal, sigsChanBufferSize)
	signal.Notify(sigCh)
	// The Go runtime uses SIGURG to preempt goroutines that have run for a
	// while, and a catch-all Notify gets a copy of every one. Nothing in
	// Elvish is interested in it, and relaying it is harmful: the editor
	// treats every signal as an input event and redraws afterwards, so a
	// redraw slow enough to be preempted triggers another redraw, and the
	// cursor flickers for as long as the screen stays expensive to draw.
	// Reset only affects delivery to the channel; the runtime keeps its own
	// handler, so preemption still works.
	signal.Reset(syscall.SIGURG)
	// Calling signal.Notify will reset the signal ignore status, so we need to
	// call signal.Ignore every time we call signal.Notify.
	//
	// TODO: Remove this if, and when, job control is implemented. This
	// handles the case of running an external command from an interactive
	// prompt.
	//
	// See https://b.elv.sh/988.
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU, syscall.SIGTSTP)
	return sigCh
}
