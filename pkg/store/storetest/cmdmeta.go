package storetest

import (
	"reflect"
	"testing"
	"time"

	"src.elv.sh/pkg/store/storedefs"
)

// TestCmdMeta tests the command metadata functionality of a Store. It may be
// run after TestCmd on the same store.
func TestCmdMeta(t *testing.T, store storedefs.Store) {
	host, err := store.HostID()
	if host == "" || err != nil {
		t.Fatalf("store.HostID() => (%q, %v), want (non-empty, nil)", host, err)
	}
	if host2, err := store.HostID(); host2 != host || err != nil {
		t.Errorf("store.HostID() => (%q, %v), want (%q, nil)", host2, err, host)
	}

	seq, err := store.AddCmd("meta cmd")
	if err != nil {
		t.Fatalf("store.AddCmd => error %v", err)
	}

	if meta, err := store.CmdMeta(seq); !matchErr(err, storedefs.ErrNoCmdMeta) {
		t.Errorf("store.CmdMeta(%v) => (%v, %v), want (_, %v)",
			seq, meta, err, storedefs.ErrNoCmdMeta)
	}

	want := storedefs.CmdMeta{
		Seq: seq, ID: "id", Time: time.Date(2026, 10, 27, 1, 2, 3, 4000, time.UTC),
		Duration: 1.5, Exit: 2, Host: host, Dir: "/d", Session: "s",
	}
	if err := store.SetCmdMeta(want); err != nil {
		t.Fatalf("store.SetCmdMeta(%v) => %v, want nil", want, err)
	}
	if got, err := store.CmdMeta(seq); !equalMeta(got, want) || err != nil {
		t.Errorf("store.CmdMeta(%v) => (%v, %v), want (%v, nil)", seq, got, err, want)
	}
	if got, err := store.CmdsMeta(seq, seq+1); !equalMetas(got, []storedefs.CmdMeta{want}) || err != nil {
		t.Errorf("store.CmdsMeta(%v, %v) => (%v, %v), want ([%v], nil)",
			seq, seq+1, got, err, want)
	}

	bad := want
	bad.Seq = seq + 100
	if err := store.SetCmdMeta(bad); !matchErr(err, storedefs.ErrNoMatchingCmd) {
		t.Errorf("store.SetCmdMeta(%v) => %v, want %v", bad, err, storedefs.ErrNoMatchingCmd)
	}

	// Deleting a command with metadata records a tombstone.
	if err := store.DelCmd(seq); err != nil {
		t.Fatalf("store.DelCmd(%v) => %v, want nil", seq, err)
	}
	if meta, err := store.CmdMeta(seq); !matchErr(err, storedefs.ErrNoCmdMeta) {
		t.Errorf("store.CmdMeta(%v) => (%v, %v), want (_, %v)",
			seq, meta, err, storedefs.ErrNoCmdMeta)
	}
	if got, err := store.CmdsMeta(seq, seq+1); len(got) != 0 || err != nil {
		t.Errorf("store.CmdsMeta(%v, %v) => (%v, %v), want ([], nil)", seq, seq+1, got, err)
	}
	if got, err := store.DeletedCmds(seq, seq+1); !equalMetas(got, []storedefs.CmdMeta{want}) || err != nil {
		t.Errorf("store.DeletedCmds(%v, %v) => (%v, %v), want ([%v], nil)",
			seq, seq+1, got, err, want)
	}

	// Deleting a command without metadata records nothing.
	seq2, err := store.AddCmd("no meta cmd")
	if err != nil {
		t.Fatalf("store.AddCmd => error %v", err)
	}
	if err := store.DelCmd(seq2); err != nil {
		t.Fatalf("store.DelCmd(%v) => %v, want nil", seq2, err)
	}
	if got, err := store.DeletedCmds(seq2, seq2+1); len(got) != 0 || err != nil {
		t.Errorf("store.DeletedCmds(%v, %v) => (%v, %v), want ([], nil)", seq2, seq2+1, got, err)
	}
}

func equalMeta(a, b storedefs.CmdMeta) bool {
	if !a.Time.Equal(b.Time) {
		return false
	}
	a.Time, b.Time = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

func equalMetas(a, b []storedefs.CmdMeta) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalMeta(a[i], b[i]) {
			return false
		}
	}
	return true
}
