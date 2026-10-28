package store

import (
	"testing"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/store/storedefs"
)

// A store that is only written to never builds the index; a query or
// WarmIndex starts the build, and starting it twice is a no-op.
func TestIndex_BuiltLazily(t *testing.T) {
	st := MustTempStore(t)
	s := st.(*dbStore)
	must.OK1(st.AddCmd("x"))
	seq := must.OK1(st.AddCmd("y"))
	must.OK(st.SetCmdMeta(storedefs.CmdMeta{Seq: seq, ID: "y", Dir: "/y"}))
	must.OK(st.DelCmd(seq - 1))
	if started(s) {
		t.Fatal("index build started without a query")
	}
	if len(s.idx.pending) != 0 {
		t.Errorf("updates queued before the build started: %d", len(s.idx.pending))
	}

	must.OK(st.WarmIndex())
	if !started(s) {
		t.Fatal("WarmIndex did not start the build")
	}
	must.OK(st.WarmIndex())
	hits := must.OK1(st.SearchCmds(storedefs.CmdQuery{}))
	if len(hits) != 1 || hits[0].Cmd.Text != "y" || hits[0].Meta.Dir != "/y" {
		t.Errorf("got hits %+v, want only y with its metadata", hits)
	}
}

func started(s *dbStore) bool {
	s.idx.mu.RLock()
	defer s.idx.mu.RUnlock()
	return s.idx.started
}
