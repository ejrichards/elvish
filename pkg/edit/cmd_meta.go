package edit

import (
	"errors"
	"os"
	"sync"
	"time"

	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/store/storedefs"
)

// Records metadata for commands added to the history. Metadata is written
// twice: once when the command is added (ID, start time, host, directory and
// session) and once after it finishes (duration and exit status). The second
// write commits the entry, giving it a record in this host's series.
type cmdMetaTracker struct {
	st      storedefs.Store
	session string

	m sync.Mutex
	// Cached result of st.HostID; empty until successfully fetched.
	host string
	// Metadata of the most recently added command whose completion has not
	// been recorded yet, and its text.
	pending     *storedefs.CmdMeta
	pendingText string
}

func newCmdMetaTracker(st storedefs.Store) *cmdMetaTracker {
	return &cmdMetaTracker{st: st, session: storedefs.NewID()}
}

// Must be called with t.m held.
func (t *cmdMetaTracker) hostID() string {
	if t.host == "" && t.st != nil {
		if id, err := t.st.HostID(); err == nil {
			t.host = id
		}
	}
	return t.host
}

// Host returns the ID of this host, or "" if there is no store or it can't
// be fetched.
func (t *cmdMetaTracker) Host() string {
	t.m.Lock()
	defer t.m.Unlock()
	return t.hostID()
}

// Session returns the ID of this interactive session.
func (t *cmdMetaTracker) Session() string { return t.session }

// Records the initial metadata of a command that has just been added to the
// history with the given sequence number.
func (t *cmdMetaTracker) cmdAdded(text string, seq int) {
	if t.st == nil {
		return
	}
	dir, _ := os.Getwd()
	t.m.Lock()
	defer t.m.Unlock()
	meta := storedefs.CmdMeta{
		Seq: seq, ID: storedefs.NewID(), Time: time.Now(),
		Host: t.hostID(), Dir: dir, Session: t.session,
	}
	t.pending, t.pendingText = &meta, text
	// TODO: Report the error.
	_ = t.st.SetCmdMeta(meta)
}

// Records the completion of the most recently added command, if src is that
// command. It has the signature of an Editor.AfterCommand callback.
func (t *cmdMetaTracker) cmdFinished(src parse.Source, duration float64, err error) {
	if t.st == nil {
		return
	}
	t.m.Lock()
	defer t.m.Unlock()
	if t.pending == nil || t.pendingText != src.Code {
		return
	}
	meta := *t.pending
	t.pending = nil
	meta.Duration = duration
	meta.Exit = exitStatus(err)
	// Committing appends the entry's record to this host's series, which is
	// what gets synced. The record carries the final duration and exit
	// status; the in-progress entry only ever exists in this store.
	// TODO: Report the error.
	_, _ = t.st.CommitCmdMeta(meta)
}

// Maps the error returned from evaluating a command to an exit status: 0 for
// success, the exit status of the last failing external command in a
// pipeline, 128 plus the signal number for a command killed by a signal, 130
// for an interrupted command, and 1 for any other exception.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	switch e := eval.Reason(err).(type) {
	case eval.ExternalCmdExit:
		ws := e.WaitStatus
		switch {
		case ws.Exited():
			return ws.ExitStatus()
		case ws.Signaled():
			return 128 + int(ws.Signal())
		default:
			return 1
		}
	case eval.PipelineError:
		status := 1
		for _, exc := range e.Errors {
			if exc == nil || exc.Reason() == nil {
				continue
			}
			if s := exitStatus(exc); s != 0 {
				status = s
			}
		}
		return status
	}
	if errors.Is(eval.Reason(err), eval.ErrInterrupted) {
		return 130
	}
	return 1
}
