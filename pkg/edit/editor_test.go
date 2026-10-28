package edit

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"src.elv.sh/pkg/must"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/ui"
)

func TestEditor_AddsHistoryAfterAccepting(t *testing.T) {
	f := setup(t)

	feedInput(f.TTYCtrl, "echo x\n")
	f.Wait()

	testCommands(t, f.Store, storedefs.Cmd{Text: "echo x", Seq: 1})
}

func TestEditor_DoesNotAddEmptyCommandToHistory(t *testing.T) {
	f := setup(t)

	feedInput(f.TTYCtrl, "\n")
	f.Wait()

	testCommands(t, f.Store /* no commands */)
}

func TestEditor_RecordsCmdMeta(t *testing.T) {
	f := setup(t)

	feedInput(f.TTYCtrl, "echo x\n")
	f.Wait()

	meta, err := f.Store.CmdMeta(1)
	if err != nil {
		t.Fatalf("CmdMeta(1) => error %v", err)
	}
	host := must.OK1(f.Store.HostID())
	wd := must.OK1(os.Getwd())
	if meta.Seq != 1 || meta.ID == "" || meta.Time.IsZero() ||
		meta.Host != host || meta.Dir != wd || meta.Session == "" ||
		meta.Duration != 0 || meta.Exit != 0 {
		t.Errorf("got meta %+v, want seq 1, non-empty id and session, host %q, dir %q",
			meta, host, wd)
	}

	// Completion of the command records its duration and exit status, and
	// commits the entry to this host's series.
	f.Editor.RunAfterCommandHooks(parse.Source{Code: "echo x"}, 1.5, errors.New("bad"))
	meta2 := must.OK1(f.Store.CmdMeta(1))
	meta.Duration, meta.Exit, meta.Idx = 1.5, 1, 1
	if !meta2.Time.Equal(meta.Time) {
		t.Errorf("got time %v, want %v", meta2.Time, meta.Time)
	}
	meta.Time, meta2.Time = time.Time{}, time.Time{}
	if !reflect.DeepEqual(meta2, meta) {
		t.Errorf("got meta %+v, want %+v", meta2, meta)
	}

	// Completion of a different command is ignored.
	f.Editor.RunAfterCommandHooks(parse.Source{Code: "echo y"}, 2.5, nil)
	meta3 := must.OK1(f.Store.CmdMeta(1))
	if meta3.Duration != 1.5 || meta3.Exit != 1 {
		t.Errorf("got duration %v exit %v, want 1.5 1", meta3.Duration, meta3.Exit)
	}
}

func TestEditor_Notify(t *testing.T) {
	f := setup(t)
	f.Editor.Notify(ui.T("note"))
	f.TTYCtrl.TestMsg(t, ui.T("note"))
}

func testCommands(t *testing.T, store storedefs.Store, wantCmds ...storedefs.Cmd) {
	t.Helper()
	cmds, err := store.CmdsWithSeq(0, 1024)
	if err != nil {
		panic(err)
	}
	if !reflect.DeepEqual(cmds, wantCmds) {
		t.Errorf("got cmds %v, want %v", cmds, wantCmds)
	}
}
