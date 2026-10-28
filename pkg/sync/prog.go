package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"src.elv.sh/pkg/daemon"
	"src.elv.sh/pkg/daemon/daemondefs"
	"src.elv.sh/pkg/prog"
	"src.elv.sh/pkg/shell"
)

// StampFile is the name of the file, beside the database, that is rewritten
// with the current time after every sync that pulled records. Interactive
// shells can watch its content or modification time to learn that the
// history has entries they have not seen.
const StampFile = "sync.stamp"

// Program is the subprogram for the -sync and -relay flags.
type Program struct {
	sync  bool
	reset bool
	relay bool
	paths *prog.DaemonPaths
}

func (p *Program) RegisterFlags(fs *prog.FlagSet) {
	fs.BoolVar(&p.sync, "sync", false,
		"Sync command history with the remote host given as the argument")
	fs.BoolVar(&p.reset, "reset", false,
		"With -sync: re-pull this host's own history from the remote after it diverged, then re-commit what was added since")
	fs.BoolVar(&p.relay, "relay", false,
		"Relay standard input and output to the storage daemon; what -sync runs over ssh")
	p.paths = fs.DaemonPaths()
}

func (p *Program) Run(fds [3]*os.File, args []string) error {
	switch {
	case p.sync && p.relay:
		return prog.BadUsage("-sync and -relay are mutually exclusive")
	case p.reset && !p.sync:
		return prog.BadUsage("-reset requires -sync")
	case p.relay:
		if len(args) > 0 {
			return prog.BadUsage("arguments are not allowed with -relay")
		}
		spawnCfg, err := shell.DaemonSpawnConfig(p.paths)
		if err != nil {
			return err
		}
		return Relay(fds, spawnCfg)
	case p.sync:
		if len(args) != 1 {
			return prog.BadUsage("-sync requires exactly one argument: the remote, as host or user@host")
		}
		spawnCfg, err := shell.DaemonSpawnConfig(p.paths)
		if err != nil {
			return err
		}
		return syncWith(fds, spawnCfg, args[0], p.reset)
	default:
		return prog.NextProgram()
	}
}

func syncWith(fds [3]*os.File, spawnCfg *daemondefs.SpawnConfig, remote string, reset bool) error {
	local, err := daemon.Activate(fds[2], spawnCfg)
	if err != nil {
		return fmt.Errorf("activate local daemon: %w", err)
	}
	defer local.Close()

	conn, err := dialSSH(remote, fds[2])
	if err != nil {
		return err
	}
	remoteClient := daemon.NewClientConn(conn)
	defer remoteClient.Close()

	if err := checkRemoteVersion(remoteClient); err != nil {
		return err
	}
	var res Result
	if reset {
		var rr ResetResult
		rr, err = Reset(local, remoteClient)
		res = rr.Result
		fmt.Fprintf(fds[1], "removed %d records of this host, re-committed %d entries\n",
			rr.Removed, rr.Recommitted)
	} else {
		res, err = Sync(local, remoteClient)
	}
	if res.Pulled > 0 {
		if err := stamp(filepath.Join(filepath.Dir(spawnCfg.DbPath), StampFile)); err != nil {
			fmt.Fprintln(fds[2], "Warning:", err)
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(fds[1], "pulled %d, pushed %d\n", res.Pulled, res.Pushed)
	return nil
}

// SyncClients checks that the remote daemon is not older than this program
// and then syncs with it. It is [Sync] for two daemon clients.
func SyncClients(local, remote daemondefs.Client) (Result, error) {
	if err := checkRemoteVersion(remote); err != nil {
		return Result{}, err
	}
	return Sync(local, remote)
}

func checkRemoteVersion(remote daemondefs.Client) error {
	version, err := remote.Version()
	if err != nil {
		return fmt.Errorf("remote daemon: %w", err)
	}
	if version < daemon.APIVersion {
		return fmt.Errorf(
			"remote daemon API version %d is older than local version %d; update elvish on the remote",
			version, daemon.APIVersion)
	}
	return nil
}

// Writes the current time to the file, creating it if needed. The content
// changes on every call, so a shell can compare it with what it last read,
// which is simpler than comparing modification times from a script.
func stamp(path string) error {
	return os.WriteFile(path, []byte(time.Now().Format(time.RFC3339Nano)+"\n"), 0600)
}
