package storedefs

import (
	"testing"
	"time"
)

func TestFuzzyMatcher_Match(t *testing.T) {
	tests := []struct {
		needle, text string
		fold         bool
		want         bool
	}{
		// Dropped characters match; the remaining ones are in order.
		{"rebuld", "sudo nixos-rebuild boot --flake .", true, true},
		{"nxrb", "sudo nixos-rebuild boot --flake .", true, true},
		// Transposed characters break the order.
		{"rebiuld", "sudo nixos-rebuild boot --flake .", true, false},
		{"", "anything", true, true},
		{"abc", "ab", true, false},
		{"abc", "", true, false},
		// Folding lowers the needle and the text.
		{"GIT", "git status", true, true},
		{"git", "GIT status", true, true},
		{"GIT", "git status", false, false},
		{"déjà", "echo déjà-vu", false, true},
		{"DÉJÀ", "echo déjà-vu", true, true},
	}
	for _, test := range tests {
		m := newFuzzyMatcher(test.needle, test.fold)
		if _, got := m.Match(test.text); got != test.want {
			t.Errorf("match %q in %q (fold %v) = %v, want %v",
				test.needle, test.text, test.fold, got, test.want)
		}
	}
}

func TestFuzzyMatcher_Score(t *testing.T) {
	// Each case has a text that must score higher than another for the same
	// needle.
	tests := []struct {
		needle, better, worse string
	}{
		// A contiguous match beats a scattered one (atuin's regression).
		{"foo bar", "foo bar --baz", "foo build-analyzer-report"},
		{"rebuild", "nixos-rebuild", "rxexbxuxixlxd"},
		// A word matched from its start beats its characters scattered over
		// the starts of several words.
		{"rebuild", "nixos-rebuild", "r e b u i l d"},
		// A match at the start of a word beats one inside a word.
		{"ls", "ls -la", "false"},
		{"gs", "git status", "logs"},
		// The tightest window is found even when an earlier, looser match
		// exists: "cm" in "echo commit" is better than "c...m" from "echo".
		{"cm", "echo commit", "echo m"},
	}
	for _, test := range tests {
		m := newFuzzyMatcher(test.needle, true)
		better, ok1 := m.Match(test.better)
		worse, ok2 := m.Match(test.worse)
		if !ok1 || !ok2 {
			t.Errorf("match %q: %q => %v, %q => %v, want both to match",
				test.needle, test.better, ok1, test.worse, ok2)
			continue
		}
		if better <= worse {
			t.Errorf("match %q: %q scores %d, %q scores %d, want the former higher",
				test.needle, test.better, better, test.worse, worse)
		}
	}
}

func TestFrecency(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if got := Frecency(0, now, now); got != 0 {
		t.Errorf("Frecency(0) = %d, want 0", got)
	}
	recent := Frecency(1, now.Add(-time.Minute), now)
	old := Frecency(1, now.Add(-60*24*time.Hour), now)
	unknown := Frecency(1, time.Time{}, now)
	if !(recent > old) || old != unknown {
		t.Errorf("recent %d, old %d, unknown %d; want recent > old = unknown", recent, old, unknown)
	}
	// Frequency adds with diminishing returns (doubling the count adds the
	// same amount, up to rounding), and is capped.
	f1, f2, f4 := Frecency(1, now, now), Frecency(2, now, now), Frecency(4, now, now)
	if !(f1 < f2 && f2 < f4) || (f4-f2)-(f2-f1) > 1 {
		t.Errorf("counts 1, 2, 4 score %d, %d, %d; want logarithmic growth", f1, f2, f4)
	}
	if got := Frecency(1000000, now, now); got != 200 {
		t.Errorf("Frecency(1000000) = %d, want 200 (both parts capped)", got)
	}
}

func TestSearchSlice_Fuzzy(t *testing.T) {
	cmds := []Cmd{
		{Seq: 1, Text: "nixos-rebuild switch"},
		{Seq: 2, Text: "echo rebuld"},
		{Seq: 3, Text: "nixos-rebuild boot"},
		{Seq: 4, Text: "nixos-rebuild boot"},
		{Seq: 5, Text: "ls"},
	}
	sub := TextQuery{Op: QSubstring, Arg: "rebuld", Fold: true}
	fuzzy := TextQuery{Op: QFuzzy, Arg: "rebuld", Fold: true}

	search := func(name string, metas map[int]CmdMeta, q CmdQuery, wantSeqs []int, wantFuzzy []bool) {
		t.Helper()
		hits, err := SearchSlice(cmds, metas, q)
		if err != nil {
			t.Fatalf("%s: error %v", name, err)
		}
		seqs := make([]int, len(hits))
		fuzzies := make([]bool, len(hits))
		for i, h := range hits {
			seqs[i], fuzzies[i] = h.Cmd.Seq, h.Fuzzy
		}
		if !equal(seqs, wantSeqs) || !equal(fuzzies, wantFuzzy) {
			t.Errorf("%s: got seqs %v fuzzy %v, want %v %v", name, seqs, fuzzies, wantSeqs, wantFuzzy)
		}
	}

	// Without the secondary query, only the exact match is found.
	search("exact only", nil, CmdQuery{Text: sub, Dedup: true}, []int{2}, []bool{false})
	// The exact match comes first; the fuzzy matches follow. They score the
	// same, so the one run more often wins, as its newest occurrence.
	search("tiered", nil, CmdQuery{Text: sub, Fuzzy: fuzzy, Dedup: true},
		[]int{2, 4, 1}, []bool{false, true, true})
	// Fuzzy matches are deduplicated even without Dedup.
	search("no dedup", nil, CmdQuery{Text: sub, Fuzzy: fuzzy},
		[]int{2, 4, 1}, []bool{false, true, true})
	// The limit counts both tiers.
	search("limit", nil, CmdQuery{Text: sub, Fuzzy: fuzzy, Dedup: true, Limit: 2},
		[]int{2, 4}, []bool{false, true})
	search("limit exact", nil, CmdQuery{Text: sub, Fuzzy: fuzzy, Dedup: true, Limit: 1},
		[]int{2}, []bool{false})
	// Recency outweighs a small difference in count.
	now := time.Now()
	metas := map[int]CmdMeta{
		1: {Seq: 1, Time: now.Add(-time.Minute), ID: "x"},
		3: {Seq: 3, Time: now.Add(-100 * 24 * time.Hour)},
		4: {Seq: 4, Time: now.Add(-100 * 24 * time.Hour)},
	}
	search("frecency", metas, CmdQuery{Text: sub, Fuzzy: fuzzy, Dedup: true},
		[]int{2, 1, 4}, []bool{false, true, true})
	// Fuzzy hits carry metadata, without the ID.
	hits, _ := SearchSlice(cmds, metas, CmdQuery{Text: sub, Fuzzy: fuzzy, Dedup: true})
	if len(hits) < 2 || hits[1].Meta.Time != metas[1].Time || hits[1].Meta.ID != "" {
		t.Errorf("fuzzy hit meta is %+v, want time of seq 1 and no ID", hits[1].Meta)
	}
	// Metadata filters apply to fuzzy matches too.
	search("filtered", metas, CmdQuery{Text: sub, Fuzzy: fuzzy, Since: now.Add(-time.Hour)},
		[]int{1}, []bool{true})
	// A bad secondary query is an error.
	if _, err := SearchSlice(cmds, nil, CmdQuery{Fuzzy: TextQuery{Op: "nope"}}); err == nil {
		t.Errorf("bad fuzzy op: got nil error")
	}
}

func TestCompileScore(t *testing.T) {
	sub := TextQuery{Op: QSubstring, Arg: "git"}
	fz := TextQuery{Op: QFuzzy, Arg: "gs"}
	// An "and" sums; an "or" takes the best; other ops score 0.
	and, _ := TextQuery{Op: QAnd, Subs: []TextQuery{sub, fz}}.CompileScore()
	or, _ := TextQuery{Op: QOr, Subs: []TextQuery{sub, fz}}.CompileScore()
	f, _ := fz.CompileScore()
	s, _ := sub.CompileScore()
	fScore, _ := f("git status")
	if got, ok := and("git status"); !ok || got != fScore {
		t.Errorf("and => (%d, %v), want (%d, true)", got, ok, fScore)
	}
	if got, ok := and("ls"); ok || got != 0 {
		t.Errorf("and on non-match => (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := or("gs"); !ok || got != fScore-fScore+got {
		t.Errorf("or => (%d, %v), want a match", got, ok)
	}
	if got, ok := or("ls"); ok || got != 0 {
		t.Errorf("or on non-match => (%d, %v), want (0, false)", got, ok)
	}
	if got, ok := s("git status"); !ok || got != 0 {
		t.Errorf("substring => (%d, %v), want (0, true)", got, ok)
	}
	if _, err := (TextQuery{Op: QOr, Subs: []TextQuery{{Op: QRegexp, Arg: "("}}}).CompileScore(); err == nil {
		t.Errorf("bad regexp in or: got nil error")
	}
}

func equal[T comparable](a, b []T) bool {
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
