//go:build unix

package eval

import (
	"os"
	"os/signal"
	"syscall"

	"src.elv.sh/pkg/sys"
	"src.elv.sh/pkg/sys/eunix"
)

// Process control functions in Unix.

func putSelfInFg() error {
	if !sys.IsATTY(os.Stdin.Fd()) {
		return nil
	}
	// If Elvish is in the background, the tcsetpgrp call below will either fail
	// (if the process is in an orphaned process group) or stop the process.
	// Ignoring TTOU fixes that.
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	return eunix.Tcsetpgrp(0, syscall.Getpgrp())
}

// Returns the attributes to start an external command with. A background
// command gets its own process group. A command started with noTTY gets its
// own session, so that it has no controlling terminal: even if it opens
// /dev/tty, it can't put itself in the foreground process group of the
// terminal used by Elvish. (A new session implies a new process group.)
func makeSysProcAttr(bg, noTTY bool) *syscall.SysProcAttr {
	if noTTY {
		return &syscall.SysProcAttr{Setsid: true}
	}
	return &syscall.SysProcAttr{Setpgid: bg}
}
