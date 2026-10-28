package modes

import (
	"strings"
	"testing"
	"time"

	"src.elv.sh/pkg/cli"
	. "src.elv.sh/pkg/cli/clitest"
	"src.elv.sh/pkg/cli/histutil"
	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/cli/tk"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/ui"
)

// Returns a Search function over an in-memory store, with the filter text
// treated like the editor does: a substring match, case-insensitive when the
// filter is all lowercase, with fuzzy matches of the filter after the exact
// ones.
func memSearch(st histutil.Store, metas []storedefs.CmdMeta) func(string, bool) ([]storedefs.CmdHit, error) {
	var bySeq map[int]storedefs.CmdMeta
	if metas != nil {
		bySeq = map[int]storedefs.CmdMeta{}
		for _, m := range metas {
			bySeq[m.Seq] = m
		}
	}
	return func(filter string, dedup bool) ([]storedefs.CmdHit, error) {
		cmds, err := st.AllCmds()
		if err != nil {
			return nil, err
		}
		fold := filter == strings.ToLower(filter)
		q := storedefs.CmdQuery{
			Text:  storedefs.TextQuery{Op: storedefs.QSubstring, Arg: filter, Fold: fold},
			Dedup: dedup,
		}
		if filter != "" {
			q.Fuzzy = storedefs.TextQuery{Op: storedefs.QFuzzy, Arg: filter, Fold: fold}
		}
		return storedefs.SearchSlice(cmds, bySeq, q)
	}
}

func TestHistlist_Fuzzy(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore(
		// 0                1              2
		"nixos-rebuild", "echo rebuld", "ls")
	startHistlist(f.App, HistlistSpec{
		Search: memSearch(st, nil), ScopeLabel: func() string { return "dir" }})

	// Entries containing the filter are listed as usual, with fuzzy matches
	// above them, marked by a dimmed sequence number. The scope label is
	// shown in the mode line.
	f.TTY.Inject(term.K('r'), term.K('e'), term.K('b'), term.K('u'), term.K('l'), term.K('d'))
	f.TestTTY(t,
		"\n",
		"   0 nixos-rebuild\n", Styles,
		"ggggg             ",
		"   1 echo rebuld                                  \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on) (dir)  rebuld", Styles,
		"**************************       ", term.DotHere)

	// With only fuzzy matches, the best is selected: "ou" is a tighter match
	// in "echo rebuld" than in "nixos-rebuild".
	for range 6 {
		f.TTY.Inject(term.K(ui.Backspace))
	}
	f.TTY.Inject(term.K('o'), term.K('u'))
	f.TestTTY(t,
		"\n",
		"   0 nixos-rebuild\n", Styles,
		"ggggg             ",
		"   1 echo rebuld                                  \n", Styles,
		"kkkkk+++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on) (dir)  ou", Styles,
		"**************************   ", term.DotHere)

	// An empty scope label shows nothing.
	f.App.PopAddon()
	startHistlist(f.App, HistlistSpec{
		Search: memSearch(st, nil), ScopeLabel: func() string { return "" }})
	f.TestTTY(t,
		"\n",
		"   0 nixos-rebuild\n",
		"   1 echo rebuld\n",
		"   2 ls                                           \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere)
}

func TestNewHistlist_NoStore(t *testing.T) {
	f := Setup()
	defer f.Stop()

	_, err := NewHistlist(f.App, HistlistSpec{})
	if err != errNoHistoryStore {
		t.Errorf("want errNoHistoryStore")
	}
}

func TestNewHistlist_FocusedWidgetNotCodeArea(t *testing.T) {
	testFocusedWidgetNotCodeArea(t, func(app cli.App) error {
		st := histutil.NewMemStore("foo")
		_, err := NewHistlist(app, HistlistSpec{Search: memSearch(st, nil)})
		return err
	})
}

type faultyStore struct{}

func (s faultyStore) AllCmds() ([]storedefs.Cmd, error) { return nil, errMock }

func TestNewHistlist_StoreError(t *testing.T) {
	f := Setup()
	defer f.Stop()

	search := func(string, bool) ([]storedefs.CmdHit, error) { return faultyStore{}.AllCmds2() }
	_, err := NewHistlist(f.App, HistlistSpec{Search: search})
	if err.Error() != "db error: mock error" {
		t.Errorf("want db error")
	}
}

func (s faultyStore) AllCmds2() ([]storedefs.CmdHit, error) { return nil, errMock }

func TestHistlist(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore(
		// 0    1      2
		"foo", "bar", "baz")
	startHistlist(f.App, HistlistSpec{Search: memSearch(st, nil)})

	// Test initial UI - last item selected, with the filter below the list.
	f.TestTTY(t,
		"\n",
		"   0 foo\n",
		"   1 bar\n",
		"   2 baz                                          \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere)

	// Test filtering.
	f.TTY.Inject(term.K('b'))
	f.TestTTY(t,
		"\n",
		"   1 bar\n",
		"   2 baz                                          \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  b", Styles,
		"********************  ", term.DotHere)

	// Test accepting.
	f.TTY.Inject(term.K(ui.Enter))
	f.TestTTY(t, "baz", term.DotHere)

	// Test starting when there is already some text: the filter starts as
	// that text, and accepting replaces it.
	st.AddCmd(storedefs.Cmd{Text: "baz2"})
	startHistlist(f.App, HistlistSpec{Search: memSearch(st, nil)})
	f.TestTTY(t,
		"baz\n",
		"   2 baz\n",
		"   0 baz2                                         \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  baz", Styles,
		"********************    ", term.DotHere)
	f.TTY.Inject(term.K(ui.Enter))
	f.TestTTY(t, "baz2", term.DotHere)
}

func TestHistlist_MultiLineBuffer(t *testing.T) {
	f := Setup(WithSpec(func(spec *cli.AppSpec) {
		// The dot is on the middle line.
		spec.CodeAreaState.Buffer = tk.CodeBuffer{Content: "a\nfo\nc", Dot: 3}
	}))
	defer f.Stop()

	st := histutil.NewMemStore("foo", "bar")
	startHistlist(f.App, HistlistSpec{Search: memSearch(st, nil)})
	// The filter starts as the line the dot is on.
	f.TestTTY(t,
		"a\n", "fo\n", "c\n",
		"   0 foo                                          \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  fo", Styles,
		"********************   ", term.DotHere)
	// Accepting replaces only that line, and leaves the dot at its end.
	f.TTY.Inject(term.K(ui.Enter))
	f.TestTTY(t, "a\n", "foo", term.DotHere, "\n", "c")
}

func TestHistlist_Meta(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore(
		// 0    1      2      3
		"foo", "bar", "baz", "box")
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	metas := []storedefs.CmdMeta{
		// Succeeded.
		{Seq: 1, Time: now.Add(-20 * time.Second), Duration: 0.022, Exit: 0},
		// Failed.
		{Seq: 2, Time: now.Add(-3 * time.Hour), Duration: 90.5, Exit: 1},
		// Not finished yet.
		{Seq: 3, Time: now.Add(-5 * time.Minute)},
	}
	startHistlist(f.App, HistlistSpec{
		Search:       memSearch(st, metas),
		ShowMeta:     true,
		Now:          func() time.Time { return now },
		HighlightCmd: func(code string) ui.Text { return ui.T(code, ui.FgMagenta) },
	})

	f.TestTTY(t,
		"\n",
		"   0                 foo\n", Styles,
		"                     ---",
		"   1   22ms  20s ago bar\n", Styles,
		"       vvvv  /////// ---",
		"   2  1m30s   3h ago baz\n", Styles,
		"      !!!!!   ////// ---",
		"   3          5m ago box                          \n", Styles,
		"++++++++++++++######+XXX++++++++++++++++++++++++++",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere)

	// Metadata and highlighting are kept when filtering.
	f.TTY.Inject(term.K('b'))
	f.TestTTY(t,
		"\n",
		"   1   22ms  20s ago bar\n", Styles,
		"       vvvv  /////// ---",
		"   2  1m30s   3h ago baz\n", Styles,
		"      !!!!!   ////// ---",
		"   3          5m ago box                          \n", Styles,
		"++++++++++++++######+XXX++++++++++++++++++++++++++",
		" HISTORY (dedup on)  b", Styles,
		"********************  ", term.DotHere)
}

func TestHistlist_HighlightWithoutMeta(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore("foo", "bar")
	startHistlist(f.App, HistlistSpec{
		Search:       memSearch(st, nil),
		HighlightCmd: func(code string) ui.Text { return ui.T(code, ui.FgMagenta) },
	})
	f.TestTTY(t,
		"\n",
		"   0 foo\n", Styles,
		"     ---",
		"   1 bar                                          \n", Styles,
		"+++++XXX++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere)
}

func TestHistlist_SearchErrorWhileFiltering(t *testing.T) {
	f := Setup()
	defer f.Stop()

	// An error from a later search is reported and leaves the list empty.
	st := histutil.NewMemStore("foo")
	search := func(filter string, dedup bool) ([]storedefs.CmdHit, error) {
		if filter != "" {
			return nil, errMock
		}
		return memSearch(st, nil)(filter, dedup)
	}
	startHistlist(f.App, HistlistSpec{Search: search})
	f.TTY.Inject(term.K('x'))
	f.TestTTY(t,
		"\n",
		" HISTORY (dedup on)  x", Styles,
		"********************  ", term.DotHere)
	f.TTY.TestMsg(t, ui.Concat(ui.T("error:", ui.FgRed), ui.T(" mock error")))
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0.0000005, "500ns"},
		{0.000012, "12µs"},
		{0.0004, "400µs"},
		{0.022, "22ms"},
		{0.9999, "999ms"},
		{1.5, "1.5s"},
		{20, "20s"},
		{90.5, "1m30s"},
		{3599, "59m59s"},
		{7500, "2h5m"},
	}
	for _, test := range tests {
		if got := formatDuration(test.seconds); got != test.want {
			t.Errorf("formatDuration(%v) = %q, want %q", test.seconds, got, test.want)
		}
	}
}

func TestFormatAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s ago"},
		{20 * time.Second, "20s ago"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{364 * 24 * time.Hour, "364d ago"},
		{400 * 24 * time.Hour, "1y ago"},
	}
	for _, test := range tests {
		if got := formatAgo(test.d); got != test.want {
			t.Errorf("formatAgo(%v) = %q, want %q", test.d, got, test.want)
		}
	}
}

func TestHistlist_Dedup(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore(
		// 0    1      2
		"ls", "echo", "ls")

	// No dedup
	startHistlist(f.App,
		HistlistSpec{Search: memSearch(st, nil), Dedup: func() bool { return false }})
	f.TestTTY(t,
		"\n",
		"   0 ls\n",
		"   1 echo\n",
		"   2 ls                                           \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY  ", Styles,
		"********* ", term.DotHere)

	f.App.PopAddon()

	// With dedup
	startHistlist(f.App,
		HistlistSpec{Search: memSearch(st, nil), Dedup: func() bool { return true }})
	f.TestTTY(t,
		"\n",
		"   1 echo\n",
		"   2 ls                                           \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere)
}

func TestHistlist_CustomSearch(t *testing.T) {
	f := Setup()
	defer f.Stop()

	st := histutil.NewMemStore(
		// 0   1         2
		"vi", "elvish", "nvi")

	startHistlist(f.App, HistlistSpec{
		Search: func(filter string, dedup bool) ([]storedefs.CmdHit, error) {
			cmds, _ := st.AllCmds()
			return storedefs.SearchSlice(cmds, nil, storedefs.CmdQuery{
				Text: storedefs.TextQuery{Op: storedefs.QRegexp, Arg: filter}, Dedup: dedup})
		},
		FilterHighlighter: func(p string) (ui.Text, []ui.Text) {
			return ui.T(p, ui.Inverse), nil
		},
	})
	f.TTY.Inject(term.K('v'), term.K('i'), term.K('$'))
	f.TestTTY(t,
		"\n",
		"   0 vi\n",
		"   2 nvi                                          \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		" HISTORY (dedup on)  vi$", Styles,
		"******************** +++", term.DotHere)
}

func startHistlist(app cli.App, spec HistlistSpec) {
	w, err := NewHistlist(app, spec)
	startMode(app, w, err)
}
