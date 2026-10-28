// Package sync implements syncing of the storage daemon's series between
// hosts, and the "elvish -sync" and "elvish -relay" subprograms.
//
// The model is git's: every host holds a full copy of every series, and
// syncing is a short-lived process on each end with no server in between.
// The client ("elvish -sync remote") runs "elvish -relay" on the remote over
// ssh, which exposes the remote's daemon on the ssh session's standard
// streams. The client then speaks the daemon's own RPC to both daemons and
// copies records in whichever direction each series is behind.
package sync

import (
	"errors"
	"fmt"
	"sort"

	"src.elv.sh/pkg/store/storedefs"
)

// ErrDiverged is returned, wrapped, when the two peers' copies of a series
// share an index but not the record at it, so neither is a prefix of the
// other and nothing can be copied without losing records. It happens when
// a host loses its database, keeps its host ID, and commits new commands
// before syncing again. Other series are still synced.
var ErrDiverged = errors.New("series diverged")

// Peer is one end of a sync: either a store or a daemon client, local or
// remote. It is the subset of [storedefs.Store] the sync needs.
type Peer interface {
	Status() (storedefs.Status, error)
	Records(host, kind string, from uint64, limit int) ([]storedefs.Record, error)
	Ingest(records []storedefs.Record) error
}

// Result counts the records moved by a sync.
type Result struct {
	Pulled, Pushed int
}

// BatchSize is the number of records moved per Records and Ingest call. Each
// Ingest is one database transaction on the receiving end.
var BatchSize = 1000

// Sync brings two peers to the same state. For every series either peer has,
// the peer with the shorter series receives the records it is missing from
// the other, in order. Series are the source of truth and append-only, so
// there is nothing to merge. Any series may be copied in either direction,
// not only the series of the two hosts involved: every peer relays.
//
// Before copying, the record both sides have at the shorter tail is
// compared; if it differs, the series has diverged (see [ErrDiverged]), it
// is skipped, and the error is returned once every other series is done.
//
// Sync returns on the first other error. Records already ingested stay
// ingested; running Sync again continues from where it stopped.
func Sync(local, remote Peer) (Result, error) {
	var res Result
	localStatus, err := local.Status()
	if err != nil {
		return res, fmt.Errorf("local status: %w", err)
	}
	remoteStatus, err := remote.Status()
	if err != nil {
		return res, fmt.Errorf("remote status: %w", err)
	}
	var diverged []error
	for _, s := range diff(localStatus, remoteStatus) {
		var n int
		var err error
		switch {
		case s.local < s.remote:
			n, err = copyRecords(remote, local, s.host, s.kind, s.local, s.remote)
			res.Pulled += n
			if err != nil {
				err = fmt.Errorf("pull %s/%s: %w", s.host, s.kind, err)
			}
		case s.local > s.remote:
			n, err = copyRecords(local, remote, s.host, s.kind, s.remote, s.local)
			res.Pushed += n
			if err != nil {
				err = fmt.Errorf("push %s/%s: %w", s.host, s.kind, err)
			}
		}
		if errors.Is(err, ErrDiverged) {
			diverged = append(diverged, err)
		} else if err != nil {
			return res, err
		}
	}
	return res, errors.Join(diverged...)
}

type series struct {
	host, kind    string
	local, remote uint64
}

// Lists every series either side has, with both tails, in a stable order.
func diff(local, remote storedefs.Status) []series {
	var all []series
	seen := map[[2]string]bool{}
	add := func(status storedefs.Status) {
		for host, kinds := range status {
			for kind := range kinds {
				key := [2]string{host, kind}
				if seen[key] {
					continue
				}
				seen[key] = true
				all = append(all, series{
					host, kind, local[host][kind], remote[host][kind]})
			}
		}
	}
	add(local)
	add(remote)
	sort.Slice(all, func(i, j int) bool {
		if all[i].host != all[j].host {
			return all[i].host < all[j].host
		}
		return all[i].kind < all[j].kind
	})
	return all
}

// Copies records (from, upto] of one series from src to dst in batches,
// after checking that both have the same record at index from. Returns the
// number of records ingested by dst.
func copyRecords(src, dst Peer, host, kind string, from, upto uint64) (int, error) {
	if from > 0 {
		if err := checkCommonRecord(src, dst, host, kind, from); err != nil {
			return 0, err
		}
	}
	n := 0
	for idx := from + 1; idx <= upto; {
		limit := BatchSize
		if remaining := upto - idx + 1; remaining < uint64(limit) {
			limit = int(remaining)
		}
		records, err := src.Records(host, kind, idx, limit)
		if err != nil {
			return n, err
		}
		if len(records) == 0 {
			return n, fmt.Errorf("no records from index %d, but status says tail is %d", idx, upto)
		}
		if err := dst.Ingest(records); err != nil {
			return n, err
		}
		n += len(records)
		idx = records[len(records)-1].Idx + 1
	}
	return n, nil
}

// Checks that both peers have the same record at the given index, which is
// the tail of the shorter copy. Record IDs are unique, so equal IDs mean the
// shorter copy is a prefix of the longer one and copying is a fast-forward.
func checkCommonRecord(a, b Peer, host, kind string, idx uint64) error {
	ra, err := a.Records(host, kind, idx, 1)
	if err != nil {
		return err
	}
	rb, err := b.Records(host, kind, idx, 1)
	if err != nil {
		return err
	}
	if len(ra) != 1 || len(rb) != 1 {
		return fmt.Errorf("no record at index %d, but status says both sides have it", idx)
	}
	if ra[0].Idx != idx || rb[0].Idx != idx || ra[0].ID != rb[0].ID {
		return fmt.Errorf("%w at index %d: records %s and %s; the host that lost its history must reset its series before syncing",
			ErrDiverged, idx, ra[0].ID, rb[0].ID)
	}
	return nil
}
