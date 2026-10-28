package edit

import (
	"errors"
	"testing"

	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/store"
)

func TestCmdMetaTracker_SessionIsSharedAcrossCommands(t *testing.T) {
	st := store.MustTempStore(t)
	tracker := newCmdMetaTracker(st)

	seq1 := must.OK1(st.AddCmd("echo x"))
	tracker.cmdAdded("echo x", seq1)
	seq2 := must.OK1(st.AddCmd("echo y"))
	tracker.cmdAdded("echo y", seq2)

	meta1 := must.OK1(st.CmdMeta(seq1))
	meta2 := must.OK1(st.CmdMeta(seq2))
	if meta1.Session == "" || meta1.Session != meta2.Session {
		t.Errorf("sessions are %q and %q, want equal and non-empty", meta1.Session, meta2.Session)
	}
	if meta1.ID == "" || meta1.ID == meta2.ID {
		t.Errorf("IDs are %q and %q, want distinct and non-empty", meta1.ID, meta2.ID)
	}
	host := must.OK1(st.HostID())
	if meta1.Host != host || meta2.Host != host {
		t.Errorf("hosts are %q and %q, want %q", meta1.Host, meta2.Host, host)
	}
}

func TestCmdMetaTracker_OnlyFinishesPendingCommand(t *testing.T) {
	st := store.MustTempStore(t)
	tracker := newCmdMetaTracker(st)

	seq := must.OK1(st.AddCmd("echo x"))
	tracker.cmdAdded("echo x", seq)
	// A different command finishing (e.g. rc.elv) does not touch the entry.
	tracker.cmdFinished(parse.Source{Code: "echo y"}, 1, nil)
	if meta := must.OK1(st.CmdMeta(seq)); meta.Duration != 0 {
		t.Errorf("got duration %v, want 0", meta.Duration)
	}
	tracker.cmdFinished(parse.Source{Code: "echo x"}, 2, nil)
	if meta := must.OK1(st.CmdMeta(seq)); meta.Duration != 2 {
		t.Errorf("got duration %v, want 2", meta.Duration)
	}
	// Finishing twice is ignored.
	tracker.cmdFinished(parse.Source{Code: "echo x"}, 3, nil)
	if meta := must.OK1(st.CmdMeta(seq)); meta.Duration != 2 {
		t.Errorf("got duration %v, want 2", meta.Duration)
	}
}

func TestCmdMetaTracker_NoStore(t *testing.T) {
	tracker := newCmdMetaTracker(nil)
	// Must not panic.
	tracker.cmdAdded("echo x", 1)
	tracker.cmdFinished(parse.Source{Code: "echo x"}, 1, nil)
}

func TestExitStatus(t *testing.T) {
	exc := func(reason error) error { return eval.NewException(reason, nil) }
	tests := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("x"), 1},
		{exc(errors.New("x")), 1},
		{exc(eval.ErrInterrupted), 130},
		{exc(eval.PipelineError{Errors: []eval.Exception{
			nil, eval.OK, eval.NewException(errors.New("x"), nil)}}), 1},
	}
	for _, test := range tests {
		if got := exitStatus(test.err); got != test.want {
			t.Errorf("exitStatus(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}
