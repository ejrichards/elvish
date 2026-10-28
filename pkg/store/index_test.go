package store_test

import (
	"path/filepath"
	"testing"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/store"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/store/storetest"
	"src.elv.sh/pkg/testutil"
)

func TestSearch(t *testing.T) {
	storetest.TestSearch(t, store.MustTempStore(t))
}

// The index is built from the database when the store is opened, so entries
// written by an earlier store are found.
func TestSearch_IndexBuiltFromDB(t *testing.T) {
	dir := testutil.InTempDir(t)
	dbPath := filepath.Join(dir, "db.bolt")

	st := must.OK1(store.NewStore(dbPath))
	seq := must.OK1(st.AddCmd("persisted"))
	must.OK(st.SetCmdMeta(storedefs.CmdMeta{Seq: seq, ID: "p", Dir: "/p"}))
	must.OK(st.Close())

	st = must.OK1(store.NewStore(dbPath))
	defer st.Close()
	// Writes that race with the build are applied after it.
	seq2 := must.OK1(st.AddCmd("after open"))
	hits := must.OK1(st.SearchCmds(storedefs.CmdQuery{}))
	if len(hits) != 2 || hits[0].Cmd.Seq != seq2 || hits[1].Cmd.Seq != seq ||
		hits[1].Cmd.Text != "persisted" || hits[1].Meta.Dir != "/p" {
		t.Errorf("got hits %+v, want seqs %v and %v with text and metadata", hits, seq2, seq)
	}
}
