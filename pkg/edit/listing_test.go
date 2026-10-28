package edit

import (
	"testing"
	"time"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/ui"
)

func TestListingBuiltins(t *testing.T) {
	// Use the custom listing mode since it doesn't require special setup. The
	// builtins work the same across all listing modes.

	f := setup(t)
	evals(f.Evaler,
		`fn item {|x| put [&to-show=$x &to-accept=$x &to-filter=$x] }`,
		`edit:listing:start-custom [(item 1) (item 2) (item 3)]`)
	buf1 := f.MakeBuffer(
		"~> \n",
		" LISTING  ", Styles,
		"********* ", term.DotHere, "\n",
		"1                                                 ", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		"2                                                 \n",
		"3                                                 ",
	)
	f.TTYCtrl.TestBuffer(t, buf1)

	evals(f.Evaler, "edit:listing:down", "edit:redraw")
	buf2 := f.MakeBuffer(
		"~> \n",
		" LISTING  ", Styles,
		"********* ", term.DotHere, "\n",
		"1                                                 \n",
		"2                                                 \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		"3                                                 ",
	)
	f.TTYCtrl.TestBuffer(t, buf2)

	evals(f.Evaler, "edit:listing:down", "edit:redraw")
	buf3 := f.MakeBuffer(
		"~> \n",
		" LISTING  ", Styles,
		"********* ", term.DotHere, "\n",
		"1                                                 \n",
		"2                                                 \n",
		"3                                                 ", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
	)
	f.TTYCtrl.TestBuffer(t, buf3)

	evals(f.Evaler, "edit:listing:down", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf3)

	evals(f.Evaler, "edit:listing:down-cycle", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf1)

	evals(f.Evaler, "edit:listing:up", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf1)

	evals(f.Evaler, "edit:listing:up-cycle", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf3)

	evals(f.Evaler, "edit:listing:page-up", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf1)

	evals(f.Evaler, "edit:listing:page-down", "edit:redraw")
	f.TTYCtrl.TestBuffer(t, buf3)
}

// Smoke tests for individual addons.

func TestHistlistAddon(t *testing.T) {
	f := setup(t, storeOp(func(s storedefs.Store) {
		s.AddCmd("ls")
		s.AddCmd("echo")
		s.AddCmd("ls")
		s.AddCmd("LS")
		// Entries 1 and 4 have no metadata, as if recorded by an older
		// version. The times are far enough in the past that the "ago"
		// column is stable while the test runs.
		s.SetCmdMeta(storedefs.CmdMeta{
			Seq: 2, Time: time.Now().Add(-2 * time.Hour), Duration: 0.022, Exit: 0})
		s.SetCmdMeta(storedefs.CmdMeta{
			Seq: 3, Time: time.Now().Add(-3 * 24 * time.Hour), Duration: 1.5, Exit: 2})
	}))

	f.TTYCtrl.Inject(term.K('R', ui.Ctrl))
	f.TestTTY(t,
		"~> \n",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere,
		"                 Ctrl-D dedup\n", Styles,
		"                 ++++++      ",
		"   2   22ms   2h ago echo\n", Styles,
		"       vvvv   ////// vvvv",
		"   3   1.5s   3d ago ls\n", Styles,
		"       !!!!   ////// vv",
		"   4                 LS                           ", Styles,
		"+++++++++++++++++++++GG+++++++++++++++++++++++++++",
	)

	evals(f.Evaler, `edit:histlist:toggle-dedup`)
	f.TestTTY(t,
		"~> \n",
		" HISTORY  ", Styles,
		"********* ", term.DotHere,
		"                            Ctrl-D dedup\n", Styles,
		"                            ++++++      ",
		"   1                 ls\n", Styles,
		"                     vv",
		"   2   22ms   2h ago echo\n", Styles,
		"       vvvv   ////// vvvv",
		"   3   1.5s   3d ago ls\n", Styles,
		"       !!!!   ////// vv",
		"   4                 LS                           ", Styles,
		"+++++++++++++++++++++GG+++++++++++++++++++++++++++",
	)

	evals(f.Evaler, `edit:histlist:toggle-dedup`)

	// Filtering is case-insensitive when filter is all lower case.
	f.TTYCtrl.Inject(term.K('l'))
	f.TestTTY(t,
		"~> \n",
		" HISTORY (dedup on)  l", Styles,
		"********************  ", term.DotHere,
		"                Ctrl-D dedup\n", Styles,
		"                ++++++      ",
		"   3   1.5s   3d ago ls\n", Styles,
		"       !!!!   ////// vv",
		"   4                 LS                           ", Styles,
		"+++++++++++++++++++++GG+++++++++++++++++++++++++++",
	)

	// Filtering is case-sensitive when filter is not all lower case.
	f.TTYCtrl.Inject(term.K(ui.Backspace), term.K('L'))
	f.TestTTY(t,
		"~> \n",
		" HISTORY (dedup on)  L", Styles,
		"********************  ", term.DotHere,
		"                Ctrl-D dedup\n", Styles,
		"                ++++++      ",
		"   4                 LS                           ", Styles,
		"+++++++++++++++++++++GG+++++++++++++++++++++++++++",
	)
}

func TestLastCmdAddon(t *testing.T) {
	f := setup(t, storeOp(func(s storedefs.Store) {
		s.AddCmd("echo hello world")
	}))

	f.TTYCtrl.Inject(term.K(',', ui.Alt))
	f.TestTTY(t,
		"~> \n",
		" LASTCMD  ", Styles,
		"********* ", term.DotHere, "\n",
		"    echo hello world                              \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		"  0 echo\n",
		"  1 hello\n",
		"  2 world",
	)
}

func TestCustomListing_PassingList(t *testing.T) {
	f := setup(t)

	evals(f.Evaler,
		`var items = [[&to-filter=1 &to-accept=echo &to-show=echo]
		              [&to-filter=2  &to-accept=put &to-show=(styled put green)]]`,
		`edit:listing:start-custom $items &accept=$edit:insert-at-dot~ &caption=A`)
	f.TestTTY(t,
		"~> \n",
		"A ", Styles,
		"* ", term.DotHere, "\n",
		"echo                                              \n", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
		"put                                               ", Styles,
		"vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv",
	)
	// Filter - "put" will be selected.
	f.TTYCtrl.Inject(term.K('2'))
	// Accept.
	f.TTYCtrl.Inject(term.K('\n'))
	f.TestTTY(t,
		"~> put", Styles,
		"   vvv", term.DotHere,
	)
}

func TestCustomListing_PassingValueCallback(t *testing.T) {
	f := setup(t)

	evals(f.Evaler,
		`var f = {|q| put [&to-accept='q '$q &to-show=(styled 'q '$q blue)] }`,
		`edit:listing:start-custom $f &caption=A`)
	// Query.
	f.TTYCtrl.Inject(term.K('x'))
	f.TestTTY(t,
		"~> \n",
		"A x", Styles,
		"*  ", term.DotHere, "\n",
		"q x                                               ", Styles,
		"##################################################",
	)
	// No-op accept.
	f.TTYCtrl.Inject(term.K('\n'))
	f.TestTTY(t, "~> ", term.DotHere)
}

func TestCustomListing_PassingBytesCallback(t *testing.T) {
	f := setup(t)

	evals(f.Evaler,
		`var f = {|q| echo '# '$q }`,
		`edit:listing:start-custom $f &accept=$edit:insert-at-dot~ &caption=A `+
			`&binding=(edit:binding-table [&Ctrl-X=$edit:listing:accept~])`)
	// Test that the query function is used to generate candidates. Also test
	// the caption.
	f.TTYCtrl.Inject(term.K('x'))
	f.TestTTY(t,
		"~> \n",
		"A x", Styles,
		"*  ", term.DotHere, "\n",
		"# x                                               ", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
	)
	// Test both the binding and the accept callback.
	f.TTYCtrl.Inject(term.K('X', ui.Ctrl))
	f.TestTTY(t,
		"~> # x", Styles,
		"   ccc", term.DotHere)
}
