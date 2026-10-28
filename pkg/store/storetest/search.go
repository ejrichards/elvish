package storetest

import (
	"testing"
	"time"

	"src.elv.sh/pkg/store/storedefs"
)

// TestSearch tests the query functionality of a Store. It must be run on a
// fresh store.
func TestSearch(t *testing.T, store storedefs.Store) {
	host, err := store.HostID()
	if err != nil {
		t.Fatalf("store.HostID() => error %v", err)
	}
	t0 := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	add := func(text string, meta *storedefs.CmdMeta) int {
		t.Helper()
		seq, err := store.AddCmd(text)
		if err != nil {
			t.Fatalf("store.AddCmd(%q) => error %v", text, err)
		}
		if meta != nil {
			meta.Seq = seq
			if err := store.SetCmdMeta(*meta); err != nil {
				t.Fatalf("store.SetCmdMeta => error %v", err)
			}
		}
		return seq
	}
	// Seq 1 has no metadata.
	s1 := add("ls", nil)
	s2 := add("echo hi", &storedefs.CmdMeta{ID: "a", Time: t0, Dir: "/a", Host: host, Session: "x", Exit: 0, Duration: 1})
	s3 := add("ls", &storedefs.CmdMeta{ID: "b", Time: t0.Add(time.Minute), Dir: "/b", Host: host, Session: "x", Exit: 1})
	s4 := add("git status", &storedefs.CmdMeta{ID: "c", Time: t0.Add(2 * time.Minute), Dir: "/a", Host: host, Session: "y", Exit: 0})
	s5 := add("echo\n  git log", &storedefs.CmdMeta{ID: "d", Time: t0.Add(3 * time.Minute), Dir: "/a", Host: host, Session: "y", Exit: 0})
	_ = s1
	// Warming the index returns at once; queries below wait for the build.
	if err := store.WarmIndex(); err != nil {
		t.Fatalf("store.WarmIndex() => error %v", err)
	}

	search := func(q storedefs.CmdQuery) []int {
		t.Helper()
		hits, err := store.SearchCmds(q)
		if err != nil {
			t.Fatalf("store.SearchCmds(%+v) => error %v", q, err)
		}
		seqs := make([]int, len(hits))
		for i, h := range hits {
			seqs[i] = h.Cmd.Seq
		}
		return seqs
	}
	check := func(name string, got, want []int) {
		t.Helper()
		if !equalInts(got, want) {
			t.Errorf("%s: got seqs %v, want %v", name, got, want)
		}
	}
	sub := func(s string) storedefs.TextQuery {
		return storedefs.TextQuery{Op: storedefs.QSubstring, Arg: s}
	}

	// Everything, newest first.
	check("all", search(storedefs.CmdQuery{}), []int{s5, s4, s3, s2, s1})
	// Dedup keeps the newest occurrence.
	check("dedup", search(storedefs.CmdQuery{Dedup: true}), []int{s5, s4, s3, s2})
	// Limit and paging.
	check("limit", search(storedefs.CmdQuery{Limit: 2}), []int{s5, s4})
	check("before", search(storedefs.CmdQuery{Limit: 2, Before: s4}), []int{s3, s2})
	// Text queries.
	check("substring", search(storedefs.CmdQuery{Text: sub("git")}), []int{s5, s4})
	check("fold", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QSubstring, Arg: "GIT", Fold: true}}), []int{s5, s4})
	check("prefix", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QPrefix, Arg: "git"}}), []int{s4})
	check("line-prefix", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QLinePrefix, Arg: "git"}}), []int{s5, s4})
	check("regexp", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QRegexp, Arg: "^(ls|echo hi)$"}}), []int{s3, s2, s1})
	check("and", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QAnd, Subs: []storedefs.TextQuery{sub("git"), sub("log")}}}), []int{s5})
	check("or", search(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QOr, Subs: []storedefs.TextQuery{sub("hi"), sub("log")}}}), []int{s5, s2})
	if _, err := store.SearchCmds(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QRegexp, Arg: "("}}); err == nil {
		t.Errorf("bad regexp: got nil error")
	}
	if _, err := store.SearchCmds(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: "nope"}}); err == nil {
		t.Errorf("bad op: got nil error")
	}
	// The secondary fuzzy query adds the entries the primary query misses,
	// after its matches and ranked by match quality rather than recency.
	fuzzy := func(s string) storedefs.TextQuery {
		return storedefs.TextQuery{Op: storedefs.QFuzzy, Arg: s}
	}
	check("fuzzy", search(storedefs.CmdQuery{Text: sub("hi"), Fuzzy: fuzzy("hi")}), []int{s2, s5})
	// "ei" is a tighter match in "echo hi" than in "echo\n  git log".
	check("fuzzy ranked", search(storedefs.CmdQuery{Text: sub("zzz"), Fuzzy: fuzzy("ei")}), []int{s2, s5})
	check("fuzzy limit", search(storedefs.CmdQuery{Text: sub("zzz"), Fuzzy: fuzzy("ei"), Limit: 1}), []int{s2})
	// Fuzzy matches are deduplicated, keeping the newest occurrence.
	check("fuzzy dedup", search(storedefs.CmdQuery{Text: sub("status"), Fuzzy: fuzzy("s")}), []int{s4, s3})
	fhits, ferr := store.SearchCmds(storedefs.CmdQuery{Text: sub("hi"), Fuzzy: fuzzy("hi")})
	if ferr != nil || len(fhits) != 2 || fhits[0].Fuzzy || !fhits[1].Fuzzy || fhits[1].Meta.Dir != "/a" {
		t.Errorf("fuzzy hits are (%+v, %v), want an exact hit then a fuzzy hit with metadata", fhits, ferr)
	}
	if _, err := store.SearchCmds(storedefs.CmdQuery{Fuzzy: storedefs.TextQuery{Op: "nope"}}); err == nil {
		t.Errorf("bad fuzzy op: got nil error")
	}
	// Metadata filters exclude entries without metadata.
	check("dir", search(storedefs.CmdQuery{Dir: "/a"}), []int{s5, s4, s2})
	check("dir none", search(storedefs.CmdQuery{Dir: "/nope"}), nil)
	check("host", search(storedefs.CmdQuery{Host: host}), []int{s5, s4, s3, s2})
	check("session", search(storedefs.CmdQuery{Session: "x"}), []int{s3, s2})
	check("exit ok", search(storedefs.CmdQuery{Exit: storedefs.ExitOK}), []int{s5, s4, s2})
	check("exit fail", search(storedefs.CmdQuery{Exit: storedefs.ExitFail}), []int{s3})
	check("since", search(storedefs.CmdQuery{Since: t0.Add(2 * time.Minute)}), []int{s5, s4})
	check("until", search(storedefs.CmdQuery{Until: t0.Add(2 * time.Minute)}), []int{s3, s2})
	// View bounds.
	check("upto", search(storedefs.CmdQuery{Upto: s4}), []int{s3, s2, s1})
	check("upto own", search(storedefs.CmdQuery{Upto: s4, Own: []int{s5}}), []int{s5, s3, s2, s1})

	// Hits carry text and metadata.
	hits, err := store.SearchCmds(storedefs.CmdQuery{Text: sub("status")})
	if err != nil || len(hits) != 1 {
		t.Fatalf("search status => (%v, %v), want one hit", hits, err)
	}
	// Hits don't carry the ID or the index.
	wantMeta := storedefs.CmdMeta{Seq: s4, Time: t0.Add(2 * time.Minute), Dir: "/a", Host: host, Session: "y"}
	if hits[0].Cmd.Text != "git status" || !equalMeta(hits[0].Meta, wantMeta) {
		t.Errorf("hit is %+v, want text \"git status\" and meta %+v", hits[0], wantMeta)
	}
	hits, _ = store.SearchCmds(storedefs.CmdQuery{Text: storedefs.TextQuery{Op: storedefs.QPrefix, Arg: "ls"}, Before: s2})
	if len(hits) != 1 || hits[0].Meta.ID != "" || !hits[0].Meta.Time.IsZero() {
		t.Errorf("hit without metadata is %+v, want zero meta", hits)
	}

	// Metadata updates are reflected: committing sets duration and exit.
	meta, _ := store.CmdMeta(s4)
	meta.Duration, meta.Exit = 2.5, 3
	if _, err := store.CommitCmdMeta(meta); err != nil {
		t.Fatalf("CommitCmdMeta => %v", err)
	}
	check("exit fail after commit", search(storedefs.CmdQuery{Exit: storedefs.ExitFail}), []int{s4, s3})
	hits, _ = store.SearchCmds(storedefs.CmdQuery{Text: sub("status")})
	if len(hits) != 1 || hits[0].Meta.Duration != 2.5 {
		t.Errorf("hit after commit is %+v, want duration 2.5", hits)
	}

	// Deletions are reflected.
	if err := store.DelCmd(s3); err != nil {
		t.Fatalf("DelCmd => %v", err)
	}
	check("after delete", search(storedefs.CmdQuery{Text: sub("ls")}), []int{s1})

	// Ingested entries and deletions are reflected.
	records := []storedefs.Record{{
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: foreignHost,
		Idx: 1, Op: storedefs.OpAdd, ID: "f", Time: t0.Add(time.Hour), Text: "make", Dir: "/f",
	}, {
		Version: storedefs.RecordVersion, Kind: storedefs.KindCmd, Host: foreignHost,
		Idx: 2, Op: storedefs.OpDel, ID: "g", Time: t0.Add(time.Hour), Target: "c",
	}}
	if err := store.Ingest(records); err != nil {
		t.Fatalf("Ingest => %v", err)
	}
	next, _ := store.NextCmdSeq()
	check("after ingest", search(storedefs.CmdQuery{}), []int{next - 1, s5, s2, s1})
	check("ingested dir", search(storedefs.CmdQuery{Dir: "/f"}), []int{next - 1})
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
