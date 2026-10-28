package storetest

import (
	"testing"
	"time"

	"src.elv.sh/pkg/store/storedefs"
)

// A host ID that is never the store's own.
const foreignHost = "01234567-89ab-7def-8123-456789abcdef"

// TestRecords tests the series functionality of a Store. It may be run after
// TestCmd and TestCmdMeta on the same store.
func TestRecords(t *testing.T, store storedefs.Store) {
	host, err := store.HostID()
	if err != nil {
		t.Fatalf("store.HostID() => error %v", err)
	}
	// Earlier suites may have left records in this host's series.
	tail0 := tail(t, store, host)
	if tail(t, store, foreignHost) != 0 {
		t.Fatalf("foreign series is not empty at start")
	}

	// Committing an entry appends a record and assigns an index.
	seq, err := store.AddCmd("committed cmd")
	if err != nil {
		t.Fatalf("store.AddCmd => error %v", err)
	}
	meta := storedefs.CmdMeta{
		Seq: seq, ID: "committed", Time: time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC),
		Host: host, Dir: "/d", Session: "s",
	}
	if err := store.SetCmdMeta(meta); err != nil {
		t.Fatalf("store.SetCmdMeta(%v) => %v, want nil", meta, err)
	}
	if got := mustMeta(t, store, seq); got.Idx != 0 {
		t.Errorf("Idx after SetCmdMeta is %v, want 0", got.Idx)
	}
	meta.Duration, meta.Exit = 1.5, 2
	got, err := store.CommitCmdMeta(meta)
	if err != nil || got.Idx != tail0+1 {
		t.Fatalf("store.CommitCmdMeta(%v) => (%v, %v), want Idx %v", meta, got, err, tail0+1)
	}
	if got := mustMeta(t, store, seq); got.Idx != tail0+1 || got.Duration != 1.5 {
		t.Errorf("meta after commit is %+v, want Idx %v, Duration 1.5", got, tail0+1)
	}
	if got := tail(t, store, host); got != tail0+1 {
		t.Errorf("tail after commit is %v, want %v", got, tail0+1)
	}
	records, err := store.Records(host, storedefs.KindCmd, tail0+1, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("store.Records => (%v, %v), want one record", records, err)
	}
	wantRecord := storedefs.Record{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: host,
		Idx: tail0 + 1, Op: storedefs.OpAdd, ID: "committed", Time: meta.Time,
		Text: "committed cmd", Duration: 1.5, Exit: 2, Dir: "/d", Session: "s",
	}
	if !equalRecord(records[0], wantRecord) {
		t.Errorf("record is %+v, want %+v", records[0], wantRecord)
	}

	// Committing again only updates the metadata.
	meta.Duration = 3
	if got, err := store.CommitCmdMeta(meta); err != nil || got.Idx != tail0+1 {
		t.Errorf("second CommitCmdMeta => (%v, %v), want Idx %v", got, err, tail0+1)
	}
	if got := tail(t, store, host); got != tail0+1 {
		t.Errorf("tail after second commit is %v, want %v", got, tail0+1)
	}
	if got := mustMeta(t, store, seq); got.Duration != 3 {
		t.Errorf("duration after second commit is %v, want 3", got.Duration)
	}
	// SetCmdMeta keeps the index.
	meta.Idx = 0
	if err := store.SetCmdMeta(meta); err != nil {
		t.Fatalf("store.SetCmdMeta(%v) => %v, want nil", meta, err)
	}
	if got := mustMeta(t, store, seq); got.Idx != tail0+1 {
		t.Errorf("Idx after SetCmdMeta is %v, want %v", got.Idx, tail0+1)
	}
	// Committing without an ID or host fails.
	if _, err := store.CommitCmdMeta(storedefs.CmdMeta{Seq: seq + 100, ID: "x", Host: host}); !matchErr(err, storedefs.ErrNoMatchingCmd) {
		t.Errorf("CommitCmdMeta of missing entry => %v, want %v", err, storedefs.ErrNoMatchingCmd)
	}
	seqNoID, _ := store.AddCmd("no id")
	if _, err := store.CommitCmdMeta(storedefs.CmdMeta{Seq: seqNoID, Host: host}); err == nil {
		t.Errorf("CommitCmdMeta without ID => nil, want error")
	}
	if _, err := store.CommitCmdMeta(storedefs.CmdMeta{Seq: seqNoID, ID: "x", Host: "bad"}); err == nil {
		t.Errorf("CommitCmdMeta with bad host => nil, want error")
	}

	// Ingesting an add record from another host creates an entry.
	foreign := storedefs.Record{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: foreignHost,
		Idx: 1, Op: storedefs.OpAdd, ID: "foreign", Time: meta.Time,
		Text: "foreign cmd", Duration: 0.5, Exit: 1, Dir: "/f", Session: "fs",
	}
	if err := store.Ingest([]storedefs.Record{foreign}); err != nil {
		t.Fatalf("store.Ingest => %v, want nil", err)
	}
	foreignSeq, err := store.NextCmdSeq()
	if err != nil {
		t.Fatal(err)
	}
	foreignSeq--
	if text, err := store.Cmd(foreignSeq); text != "foreign cmd" || err != nil {
		t.Errorf("ingested entry is (%q, %v), want (\"foreign cmd\", nil)", text, err)
	}
	wantMeta := storedefs.CmdMeta{
		Seq: foreignSeq, ID: "foreign", Time: meta.Time, Duration: 0.5, Exit: 1,
		Host: foreignHost, Dir: "/f", Session: "fs", Idx: 1,
	}
	if got := mustMeta(t, store, foreignSeq); !equalMeta(got, wantMeta) {
		t.Errorf("ingested meta is %+v, want %+v", got, wantMeta)
	}
	status, err := store.Status()
	if err != nil || status[host][storedefs.KindCmd] != tail0+1 || status[foreignHost][storedefs.KindCmd] != 1 {
		t.Errorf("store.Status() => (%v, %v), want %v: %v and %v: 1",
			status, err, host, tail0+1, foreignHost)
	}
	if records, err := store.Records(foreignHost, storedefs.KindCmd, 1, 0); err != nil || len(records) != 1 || !equalRecord(records[0], foreign) {
		t.Errorf("store.Records(foreign) => (%+v, %v), want [%+v]", records, err, foreign)
	}

	// Bad records are rejected and nothing is written.
	bad := []storedefs.Record{
		{Version: 99, Kind: storedefs.KindCmd, Host: foreignHost, Idx: 2, Op: storedefs.OpAdd, ID: "b"},
		{Version: 1, Kind: "nope", Host: foreignHost, Idx: 2, Op: storedefs.OpAdd, ID: "b"},
		{Version: 1, Kind: storedefs.KindCmd, Host: "nope", Idx: 2, Op: storedefs.OpAdd, ID: "b"},
		{Version: 1, Kind: storedefs.KindCmd, Host: foreignHost, Idx: 3, Op: storedefs.OpAdd, ID: "b"},
		{Version: 1, Kind: storedefs.KindCmd, Host: foreignHost, Idx: 2, Op: "nope", ID: "b"},
		{Version: 1, Kind: storedefs.KindCmd, Host: foreignHost, Idx: 2, Op: storedefs.OpAdd},
		{Version: 1, Kind: storedefs.KindCmd, Host: foreignHost, Idx: 2, Op: storedefs.OpDel, ID: "b"},
	}
	for _, r := range bad {
		if err := store.Ingest([]storedefs.Record{r}); err == nil {
			t.Errorf("store.Ingest(%+v) => nil, want error", r)
		}
	}
	// A batch is all or nothing.
	good := foreign
	good.Idx, good.ID, good.Text = 2, "good", "good cmd"
	if err := store.Ingest([]storedefs.Record{good, bad[0]}); err == nil {
		t.Errorf("store.Ingest(good, bad) => nil, want error")
	}
	if got := tail(t, store, foreignHost); got != 1 {
		t.Errorf("foreign tail after rejected batch is %v, want 1", got)
	}

	// A delete record from another host deletes the local entry.
	del := storedefs.Record{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: foreignHost,
		Idx: 2, Op: storedefs.OpDel, ID: "del", Time: meta.Time, Target: "committed",
	}
	if err := store.Ingest([]storedefs.Record{del}); err != nil {
		t.Fatalf("store.Ingest(del) => %v, want nil", err)
	}
	if _, err := store.Cmd(seq); !matchErr(err, storedefs.ErrNoMatchingCmd) {
		t.Errorf("deleted entry still present: %v", err)
	}
	if got, err := store.DeletedCmds(seq, seq+1); err != nil || len(got) != 1 || got[0].ID != "committed" {
		t.Errorf("store.DeletedCmds => (%v, %v), want the deleted entry", got, err)
	}
	// Deleting an unknown target is fine.
	del.Idx, del.ID, del.Target = 3, "del2", "unknown"
	if err := store.Ingest([]storedefs.Record{del}); err != nil {
		t.Errorf("store.Ingest(del unknown) => %v, want nil", err)
	}
	// An add record that arrives after its deletion is kept in the series
	// but not projected; neither is one whose ID is already present.
	next, _ := store.NextCmdSeq()
	zombie := foreign
	zombie.Idx, zombie.ID, zombie.Text = 4, "committed", "zombie"
	dup := foreign
	dup.Idx, dup.ID, dup.Text = 5, "foreign", "dup"
	if err := store.Ingest([]storedefs.Record{zombie, dup}); err != nil {
		t.Fatalf("store.Ingest(zombie, dup) => %v, want nil", err)
	}
	if next2, _ := store.NextCmdSeq(); next2 != next {
		t.Errorf("next seq after zombie is %v, want %v", next2, next)
	}
	if got := tail(t, store, foreignHost); got != 5 {
		t.Errorf("foreign tail is %v, want 5", got)
	}

	// Deleting locally records a delete in this host's series.
	if err := store.DelCmd(foreignSeq); err != nil {
		t.Fatalf("store.DelCmd(%v) => %v", foreignSeq, err)
	}
	records, err = store.Records(host, storedefs.KindCmd, tail0+2, 0)
	if err != nil || len(records) != 1 || records[0].Op != storedefs.OpDel ||
		records[0].Target != "foreign" || records[0].ID == "" || records[0].Time.IsZero() {
		t.Errorf("records after DelCmd => (%+v, %v), want one delete of \"foreign\"", records, err)
	}
	// Deleting an entry without metadata records nothing.
	if err := store.DelCmd(seqNoID); err != nil {
		t.Fatalf("store.DelCmd(%v) => %v", seqNoID, err)
	}
	if got := tail(t, store, host); got != tail0+2 {
		t.Errorf("tail after deleting entry without metadata is %v, want %v", got, tail0+2)
	}

	// Records honors from and limit.
	if records, err := store.Records(foreignHost, storedefs.KindCmd, 2, 2); err != nil ||
		len(records) != 2 || records[0].Idx != 2 || records[1].Idx != 3 {
		t.Errorf("store.Records(foreign, 2, 2) => (%+v, %v), want indexes 2 and 3", records, err)
	}
	if records, err := store.Records(foreignHost, storedefs.KindCmd, 6, 0); err != nil || len(records) != 0 {
		t.Errorf("store.Records(foreign, 6, 0) => (%+v, %v), want none", records, err)
	}
	if _, err := store.Records("bad", storedefs.KindCmd, 1, 0); err == nil {
		t.Errorf("store.Records(bad host) => nil error, want error")
	}

	// Resetting a series removes its records and uncommits the entries they
	// added; ingesting a record again restores the entry's index without
	// adding it again.
	seqAgain, _ := store.AddCmd("again")
	again, err := store.CommitCmdMeta(storedefs.CmdMeta{Seq: seqAgain, ID: "again", Host: host})
	if err != nil || again.Idx != tail0+3 {
		t.Fatalf("CommitCmdMeta(again) => (%+v, %v), want Idx %v", again, err, tail0+3)
	}
	if n, err := store.ResetSeries(host, storedefs.KindCmd); err != nil || n != int(tail0)+3 {
		t.Fatalf("store.ResetSeries => (%v, %v), want %v records removed", n, err, tail0+3)
	}
	if got := tail(t, store, host); got != 0 {
		t.Errorf("tail after reset is %v, want 0", got)
	}
	if got := mustMeta(t, store, seqAgain); got.Idx != 0 {
		t.Errorf("Idx after reset is %v, want 0", got.Idx)
	}
	next, _ = store.NextCmdSeq()
	again.Idx = 1
	if err := store.Ingest([]storedefs.Record{addRecordOf(again, "again")}); err != nil {
		t.Fatalf("store.Ingest(again) => %v", err)
	}
	if got := mustMeta(t, store, seqAgain); got.Idx != 1 {
		t.Errorf("Idx after re-ingest is %v, want 1", got.Idx)
	}
	if next2, _ := store.NextCmdSeq(); next2 != next {
		t.Errorf("re-ingest added an entry: next seq %v, was %v", next2, next)
	}
	if _, err := store.ResetSeries("bad", storedefs.KindCmd); err == nil {
		t.Errorf("store.ResetSeries(bad host) => nil error, want error")
	}
}

func addRecordOf(meta storedefs.CmdMeta, text string) storedefs.Record {
	return storedefs.Record{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: meta.Host, Idx: meta.Idx,
		Op: storedefs.OpAdd, ID: meta.ID, Time: meta.Time, Text: text,
		Duration: meta.Duration, Exit: meta.Exit, Dir: meta.Dir, Session: meta.Session,
	}
}

func tail(t *testing.T, store storedefs.Store, host string) uint64 {
	t.Helper()
	status, err := store.Status()
	if err != nil {
		t.Fatalf("store.Status() => error %v", err)
	}
	return status[host][storedefs.KindCmd]
}

func mustMeta(t *testing.T, store storedefs.Store, seq int) storedefs.CmdMeta {
	t.Helper()
	meta, err := store.CmdMeta(seq)
	if err != nil {
		t.Fatalf("store.CmdMeta(%v) => error %v", seq, err)
	}
	return meta
}

func equalRecord(a, b storedefs.Record) bool {
	if !a.Time.Equal(b.Time) {
		return false
	}
	a.Time, b.Time = time.Time{}, time.Time{}
	return a == b
}
