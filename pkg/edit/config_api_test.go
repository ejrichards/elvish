package edit

import (
	"testing"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/ui"
)

func TestBeforeReadline(t *testing.T) {
	f := setup(t, rc(
		`var called = 0`,
		`set edit:before-readline = [ { set called = (+ $called 1) } ]`))

	// Wait for UI to stabilize so that we can be sure that before-readline hooks
	// have been called.
	f.TestTTY(t, "~> ", term.DotHere)

	testGlobal(t, f.Evaler, "called", 1)
}

func TestAfterReadline(t *testing.T) {
	f := setup(t)

	evals(f.Evaler,
		`var called = 0`,
		`var called-with = ''`,
		`set edit:after-readline = [
		{|code| set called = (+ $called 1); set called-with = $code } ]`)

	// Wait for UI to stabilize so that we can be sure that after-readline hooks
	// are *not* called.
	f.TestTTY(t, "~> ", term.DotHere)
	testGlobal(t, f.Evaler, "called", "0")

	// Input "test code", press Enter and wait until the editor is done.
	feedInput(f.TTYCtrl, "test code\n")
	f.Wait()

	testGlobals(t, f.Evaler, map[string]any{
		"called":      1,
		"called-with": "test code",
	})
}

func TestAddCmdFilters(t *testing.T) {
	cases := []struct {
		name        string
		rc          string
		input       string
		wantHistory []storedefs.Cmd
	}{
		// TODO: Enable the following two tests once error output can
		// be tested.
		// {
		// 	name:        "non callable item",
		// 	rc:          "edit:add-cmd-filters = [$false]",
		// 	input:       "echo\n",
		// 	wantHistory: []string{"echo"},
		// },
		// {
		// 	name:        "callback outputs nothing",
		// 	rc:          "edit:add-cmd-filters = [{|_| }]",
		// 	input:       "echo\n",
		// 	wantHistory: []string{"echo"},
		// },
		{
			name:        "callback outputs true",
			rc:          "set edit:add-cmd-filters = [{|_| put $true }]",
			input:       "echo\n",
			wantHistory: []storedefs.Cmd{{Text: "echo", Seq: 1}},
		},
		{
			name:        "callback outputs false",
			rc:          "set edit:add-cmd-filters = [{|_| put $false }]",
			input:       "echo\n",
			wantHistory: nil,
		},
		{
			name:        "false-true chain",
			rc:          "set edit:add-cmd-filters = [{|_| put $false } {|_| put $true }]",
			input:       "echo\n",
			wantHistory: nil,
		},
		{
			name:        "true-false chain",
			rc:          "set edit:add-cmd-filters = [{|_| put $true } {|_| put $false }]",
			input:       "echo\n",
			wantHistory: nil,
		},
		{
			name:        "positive",
			rc:          "set edit:add-cmd-filters = [{|cmd| ==s $cmd echo }]",
			input:       "echo\n",
			wantHistory: []storedefs.Cmd{{Text: "echo", Seq: 1}},
		},
		{
			name:        "negative",
			rc:          "set edit:add-cmd-filters = [{|cmd| ==s $cmd echo }]",
			input:       "echo x\n",
			wantHistory: nil,
		},
		{
			name:        "default value",
			rc:          "",
			input:       " echo\n",
			wantHistory: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t, rc(c.rc))

			feedInput(f.TTYCtrl, c.input)
			f.Wait()

			testCommands(t, f.Store, c.wantHistory...)
		})
	}
}

func TestHistoryFilter(t *testing.T) {
	cases := []struct {
		name        string
		rc          string
		input       string
		wantHistory []storedefs.Cmd
	}{
		{
			name:        "default value",
			rc:          "",
			input:       "cd foo\n",
			wantHistory: []storedefs.Cmd{{Text: "cd foo", Seq: 1}},
		},
		{
			name:        "match excludes",
			rc:          "set edit:history-filter = ['^cd ']",
			input:       "cd foo\n",
			wantHistory: nil,
		},
		{
			name:        "no match keeps",
			rc:          "set edit:history-filter = ['^cd ']",
			input:       "echo cd foo\n",
			wantHistory: []storedefs.Cmd{{Text: "echo cd foo", Seq: 1}},
		},
		{
			name:        "anchored pattern",
			rc:          "set edit:history-filter = ['^ls$']",
			input:       "ls -l\n",
			wantHistory: []storedefs.Cmd{{Text: "ls -l", Seq: 1}},
		},
		{
			name:        "any pattern matches",
			rc:          "set edit:history-filter = ['^cd ' '^exit$']",
			input:       "exit\n",
			wantHistory: nil,
		},
		{
			name:        "invalid pattern skipped",
			rc:          "set edit:history-filter = ['(' '^cd ']",
			input:       "cd foo\n",
			wantHistory: nil,
		},
		{
			name:        "non-string skipped",
			rc:          "set edit:history-filter = [$true '^cd ']",
			input:       "cd foo\n",
			wantHistory: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t, rc(c.rc))

			feedInput(f.TTYCtrl, c.input)
			f.Wait()

			testCommands(t, f.Store, c.wantHistory...)
		})
	}
}

func TestHistoryFilter_RunsBeforeAddCmdFilters(t *testing.T) {
	f := setup(t, rc(
		`var called = $false`,
		`set edit:history-filter = ['^cd ']`,
		`set edit:add-cmd-filters = [{|_| set called = $true; put $true }]`,
	))

	feedInput(f.TTYCtrl, "cd foo\n")
	f.Wait()
	testCommands(t, f.Store)
	testGlobal(t, f.Evaler, "called", false)
}

func TestAddCmdFilters_SkipsRemainingOnFalse(t *testing.T) {
	f := setup(t, rc(
		`var called = $false`,
		`set @edit:add-cmd-filters = {|_| put $false } {|_| called = $true; put $true }`,
	))

	feedInput(f.TTYCtrl, "echo\n")
	f.Wait()
	testCommands(t, f.Store)
	testGlobal(t, f.Evaler, "called", false)
}

func TestGlobalBindings(t *testing.T) {
	f := setup(t, rc(
		`var called = $false`,
		`set edit:global-binding[Ctrl-X] = { set called = $true }`,
	))

	f.TTYCtrl.Inject(term.K('X', ui.Ctrl))
	f.TTYCtrl.Inject(term.K(ui.Enter))
	f.Wait()

	testGlobal(t, f.Evaler, "called", true)
}
