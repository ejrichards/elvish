package store

import (
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/store/storedefs"
)

// Metadata that can't be decoded is skipped by the index build, and the
// entry is still indexed without it.
func TestIndexBuild_SkipsUndecodableMeta(t *testing.T) {
	st := MustTempStore(t)
	s := st.(*dbStore)
	seq := must.OK1(st.AddCmd("old format"))
	seq2 := must.OK1(st.AddCmd("fine"))
	must.OK(st.SetCmdMeta(storedefs.CmdMeta{Seq: seq2, ID: "ok", Dir: "/d"}))
	// Overwrite the first entry's metadata with the old format behind the
	// index's back.
	must.OK(s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketCmdMeta)).Put(
			marshalSeq(uint64(seq)), []byte(`{"seq":1,"time":"2026-10-28T14:36:02Z"}`))
	}))

	ix := newCmdIndex()
	if err := ix.build(s.db); err != nil {
		t.Fatalf("build => %v, want nil", err)
	}
	hits := must.OK1(ix.search(storedefs.CmdQuery{}))
	if len(hits) != 2 || hits[1].Cmd.Text != "old format" || !hits[1].Meta.Time.IsZero() ||
		hits[0].Meta.Dir != "/d" {
		t.Errorf("got hits %+v, want both entries, the first without metadata", hits)
	}
}

// A failed build leaves the index empty but usable, and does not break the
// lock it holds.
func TestIndexBuild_ErrorLeavesUsableIndex(t *testing.T) {
	st := MustTempStore(t)
	s := st.(*dbStore)
	must.OK1(st.AddCmd("x"))
	// Close the database so the build's transaction fails.
	must.OK(s.db.Close())

	ix := newCmdIndex()
	if err := ix.build(s.db); err == nil || !errors.Is(err, bolt.ErrDatabaseNotOpen) {
		t.Fatalf("build => %v, want database not open", err)
	}
	// A query must still work and the index must still accept updates.
	if hits, err := ix.search(storedefs.CmdQuery{}); err != nil || len(hits) != 0 {
		t.Errorf("search after failed build => (%v, %v), want none", hits, err)
	}
	ix.update(func() { ix.appendCmd(1, "after") })
	if hits, _ := ix.search(storedefs.CmdQuery{}); len(hits) != 1 || hits[0].Cmd.Text != "after" {
		t.Errorf("search after update => %+v, want the added entry", hits)
	}
}
