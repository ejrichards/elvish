package sync

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/store"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/testutil"
)

func TestSync_EveryPeerRelays(t *testing.T) {
	a, b, hub := newStore(t), newStore(t), newStore(t)
	addCmds(t, a, "a1", "a2")
	addCmds(t, b, "b1")

	checkSync(t, a, hub, Result{Pulled: 0, Pushed: 2})
	// b gets a's series from the hub, not from a.
	checkSync(t, b, hub, Result{Pulled: 2, Pushed: 1})
	checkSync(t, a, hub, Result{Pulled: 1, Pushed: 0})
	checkSync(t, a, hub, Result{})

	for name, st := range map[string]storedefs.Store{"a": a, "b": b, "hub": hub} {
		if got := texts(t, st); !sameSet(got, []string{"a1", "a2", "b1"}) {
			t.Errorf("%s has %v, want a1 a2 b1", name, got)
		}
	}
	if sa, sb := status(t, a), status(t, b); !reflect.DeepEqual(sa, sb) {
		t.Errorf("status differs: a %v, b %v", sa, sb)
	}
}

func TestSync_DeletesPropagate(t *testing.T) {
	a, b := newStore(t), newStore(t)
	seqs := addCmds(t, a, "keep", "gone")
	checkSync(t, a, b, Result{Pushed: 2})

	// b deletes a's entry; a learns of it on the next sync.
	bSeq := seqOf(t, b, "gone")
	if err := b.DelCmd(bSeq); err != nil {
		t.Fatal(err)
	}
	checkSync(t, a, b, Result{Pulled: 1})
	if _, err := a.Cmd(seqs[1]); !errors.Is(err, storedefs.ErrNoMatchingCmd) {
		t.Errorf("a still has deleted entry: %v", err)
	}
	if got := texts(t, a); !sameSet(got, []string{"keep"}) {
		t.Errorf("a has %v, want keep", got)
	}
}

func TestSync_Batches(t *testing.T) {
	testutil.Set(t, &BatchSize, 2)
	a, b := newStore(t), newStore(t)
	addCmds(t, a, "1", "2", "3", "4", "5")
	addCmds(t, b, "x", "y", "z")
	checkSync(t, a, b, Result{Pulled: 3, Pushed: 5})
	checkSync(t, a, b, Result{})
	if got := texts(t, b); !sameSet(got, []string{"1", "2", "3", "4", "5", "x", "y", "z"}) {
		t.Errorf("b has %v", got)
	}
}

func TestSync_StopsAtFirstError(t *testing.T) {
	testutil.Set(t, &BatchSize, 1)
	a, b := newStore(t), newStore(t)
	addCmds(t, a, "1", "2", "3")
	failing := &failAfter{Peer: b, n: 2}
	res, err := Sync(a, failing)
	if err == nil || !errors.Is(err, errInjected) {
		t.Fatalf("Sync => %v, want injected error", err)
	}
	if res.Pushed != 2 {
		t.Errorf("Pushed = %d, want 2", res.Pushed)
	}
	// Running again continues from where it stopped.
	checkSync(t, a, b, Result{Pushed: 1})
}

func TestSync_StatusDisagreesWithRecords(t *testing.T) {
	a := newStore(t)
	addCmds(t, a, "1")
	host, _ := a.HostID()
	lying := &fakePeer{
		status:  storedefs.Status{host: {storedefs.KindCmd: 5}},
		records: nil,
	}
	if _, err := Sync(a, lying); err == nil {
		t.Error("Sync with a peer whose Records are empty => nil, want error")
	}
}

// A host that loses its database but keeps its host ID, and commits before
// syncing again, has a series that shares indexes with the hub's copy but
// not records. Neither direction is a fast-forward, so that series is
// refused; the others still sync.
func TestSync_RefusesDivergedSeries(t *testing.T) {
	testutil.InTempDir(t)
	os.Mkdir("a", 0700)
	a := must.OK1(store.NewStore("a/db.bolt"))
	hub, b := newStore(t), newStore(t)
	addCmds(t, a, "old 1", "old 2")
	addCmds(t, b, "b 1")
	checkSync(t, a, hub, Result{Pushed: 2})
	checkSync(t, b, hub, Result{Pulled: 2, Pushed: 1})

	// Lose the database, keep the host ID, commit before syncing.
	must.OK(a.Close())
	must.OK(os.Remove("a/db.bolt"))
	a = must.OK1(store.NewStore("a/db.bolt"))
	t.Cleanup(func() { a.Close() })
	addCmds(t, a, "new 1")

	res, err := Sync(a, hub)
	if !errors.Is(err, ErrDiverged) {
		t.Fatalf("Sync => %v, want diverged", err)
	}
	// b's series still came through; a's own series was left alone.
	if res != (Result{Pulled: 1}) {
		t.Errorf("Sync => %+v, want 1 pulled", res)
	}
	if got := texts(t, a); !sameSet(got, []string{"new 1", "b 1"}) {
		t.Errorf("a has %v, want new 1 and b 1", got)
	}
	host := must.OK1(a.HostID())
	if tail := status(t, hub)[host][storedefs.KindCmd]; tail != 2 {
		t.Errorf("hub's copy of a's series has tail %d, want 2 untouched", tail)
	}
	// The other direction refuses too.
	if _, err := Sync(hub, a); !errors.Is(err, ErrDiverged) {
		t.Errorf("Sync(hub, a) => %v, want diverged", err)
	}

	// A reset gets a back in step without losing the command it ran after
	// the wipe: the hub's copy comes back, "new 1" is committed after it
	// and pushed. An entry that was never committed goes along.
	seqNever := must.OK1(a.AddCmd("never committed"))
	must.OK(a.SetCmdMeta(storedefs.CmdMeta{Seq: seqNever, ID: storedefs.NewID(), Host: host}))
	rr, err := Reset(a, hub)
	if err != nil {
		t.Fatalf("Reset => %v", err)
	}
	want := ResetResult{Removed: 1, Recommitted: 2, Result: Result{Pulled: 2, Pushed: 2}}
	if rr != want {
		t.Errorf("Reset => %+v, want %+v", rr, want)
	}
	if got := texts(t, a); !sameSet(got, []string{"old 1", "old 2", "new 1", "never committed", "b 1"}) {
		t.Errorf("a has %v after reset", got)
	}
	if got := texts(t, hub); !sameSet(got, []string{"old 1", "old 2", "new 1", "never committed", "b 1"}) {
		t.Errorf("hub has %v after reset", got)
	}
	if sa, sh := status(t, a), status(t, hub); !reflect.DeepEqual(sa, sh) || sa[host][storedefs.KindCmd] != 4 {
		t.Errorf("status after reset: a %v, hub %v, want equal with a's tail 4", sa, sh)
	}
	records := must.OK1(a.Records(host, storedefs.KindCmd, 1, 0))
	if len(records) != 4 || records[2].Text != "new 1" || records[3].Text != "never committed" {
		t.Errorf("a's series after reset is %+v, want old 1, old 2, new 1, never committed", records)
	}
	// Everything is a fast-forward again, and a reset of a store in step is
	// a no-op apart from the round trip.
	checkSync(t, a, hub, Result{})
	checkSync(t, b, hub, Result{Pulled: 2})
	if rr, err := Reset(a, hub); err != nil || rr != (ResetResult{Removed: 4, Result: Result{Pulled: 4}}) {
		t.Errorf("second Reset => (%+v, %v), want 4 removed and pulled back", rr, err)
	}

	// Deleting the host ID as well makes a new host, which syncs cleanly.
	must.OK(a.Close())
	must.OK(os.Remove("a/db.bolt"))
	must.OK(os.Remove("a/host-id"))
	a = must.OK1(store.NewStore("a/db.bolt"))
	addCmds(t, a, "fresh")
	checkSync(t, a, hub, Result{Pulled: 5, Pushed: 1})
	if got := texts(t, a); !sameSet(got, []string{"fresh", "old 1", "old 2", "new 1", "never committed", "b 1"}) {
		t.Errorf("fresh host has %v", got)
	}
}

func TestSync_StatusErrors(t *testing.T) {
	a := newStore(t)
	bad := &fakePeer{err: errInjected}
	if _, err := Sync(a, bad); !errors.Is(err, errInjected) {
		t.Errorf("Sync(a, bad) => %v, want injected", err)
	}
	if _, err := Sync(bad, a); !errors.Is(err, errInjected) {
		t.Errorf("Sync(bad, a) => %v, want injected", err)
	}
}

// Helpers.

var errInjected = errors.New("injected")

func newStore(t *testing.T) store.DBStore {
	return store.MustTempStore(t)
}

// Adds commands to a store as its own host, committing each so it has a
// record. Returns the sequence numbers.
func addCmds(t *testing.T, st storedefs.Store, texts ...string) []int {
	t.Helper()
	host, err := st.HostID()
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for i, text := range texts {
		seq, err := st.AddCmd(text)
		if err != nil {
			t.Fatal(err)
		}
		_, err = st.CommitCmdMeta(storedefs.CmdMeta{
			Seq: seq, ID: storedefs.NewID(), Host: host, Dir: "/",
			Time: time.Date(2026, 10, 28, 0, 0, i, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	return seqs
}

func checkSync(t *testing.T, local, remote Peer, want Result) {
	t.Helper()
	got, err := Sync(local, remote)
	if err != nil {
		t.Fatalf("Sync => error %v", err)
	}
	if got != want {
		t.Errorf("Sync => %+v, want %+v", got, want)
	}
}

func texts(t *testing.T, st storedefs.Store) []string {
	t.Helper()
	next, err := st.NextCmdSeq()
	if err != nil {
		t.Fatal(err)
	}
	cmds, err := st.CmdsWithSeq(0, next)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cmds {
		out = append(out, c.Text)
	}
	return out
}

func seqOf(t *testing.T, st storedefs.Store, text string) int {
	t.Helper()
	next, _ := st.NextCmdSeq()
	cmds, _ := st.CmdsWithSeq(0, next)
	for _, c := range cmds {
		if c.Text == text {
			return c.Seq
		}
	}
	t.Fatalf("no entry %q", text)
	return 0
}

func status(t *testing.T, st storedefs.Store) storedefs.Status {
	t.Helper()
	s, err := st.Status()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// A peer whose Ingest fails after n successful calls.
type failAfter struct {
	Peer
	n, calls int
}

func (p *failAfter) Ingest(records []storedefs.Record) error {
	p.calls++
	if p.calls > p.n {
		return fmt.Errorf("ingest %d: %w", p.calls, errInjected)
	}
	return p.Peer.Ingest(records)
}

// A peer with canned responses.
type fakePeer struct {
	status  storedefs.Status
	records []storedefs.Record
	err     error
}

func (p *fakePeer) Status() (storedefs.Status, error) { return p.status, p.err }

func (p *fakePeer) Records(host, kind string, from uint64, limit int) ([]storedefs.Record, error) {
	return p.records, p.err
}

func (p *fakePeer) Ingest([]storedefs.Record) error { return p.err }
