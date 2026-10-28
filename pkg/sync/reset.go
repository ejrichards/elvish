package sync

import (
	"fmt"

	"src.elv.sh/pkg/store/storedefs"
)

// ResetResult counts what a Reset did.
type ResetResult struct {
	// Records of this host's series removed locally before the pull.
	Removed int
	// Entries committed anew after the pull: those this host added after
	// the point its series diverged, or had never committed.
	Recommitted int
	// Totals of the two syncs.
	Result
}

// Reset gets a host whose copy of its own series has diverged from the
// remote's (see [ErrDiverged]) back in step, without losing commands:
//
//  1. Remove the local copy of this host's series, leaving the entries it
//     added in the history as uncommitted.
//  2. Sync. The remote's copy of the series comes back, restoring the index
//     of every entry it knows; every other series syncs as usual.
//  3. Commit every entry of this host that is left uncommitted, which
//     appends them after the remote's records.
//  4. Sync again to push them.
//
// Deletions this host made after its series diverged are not re-recorded:
// they stay in effect locally but never reach other hosts.
func Reset(local storedefs.Store, remote Peer) (ResetResult, error) {
	var res ResetResult
	host, err := local.HostID()
	if err != nil {
		return res, fmt.Errorf("local host ID: %w", err)
	}
	res.Removed, err = local.ResetSeries(host, storedefs.KindCmd)
	if err != nil {
		return res, fmt.Errorf("reset series: %w", err)
	}
	r1, err := Sync(local, remote)
	res.Result = r1
	if err != nil {
		return res, err
	}
	res.Recommitted, err = recommit(local, host)
	if err != nil {
		return res, err
	}
	r2, err := Sync(local, remote)
	res.Pulled += r2.Pulled
	res.Pushed += r2.Pushed
	return res, err
}

// Commits every entry of the given host that has metadata but no record.
func recommit(local storedefs.Store, host string) (int, error) {
	next, err := local.NextCmdSeq()
	if err != nil {
		return 0, err
	}
	metas, err := local.CmdsMeta(0, next)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range metas {
		if m.Idx != 0 || m.ID == "" || m.Host != host {
			continue
		}
		if _, err := local.CommitCmdMeta(m); err != nil {
			return n, fmt.Errorf("commit entry %d: %w", m.Seq, err)
		}
		n++
	}
	return n, nil
}
