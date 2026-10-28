package store

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
	"unsafe"

	bolt "go.etcd.io/bbolt"
	. "src.elv.sh/pkg/store/storedefs"
)

// An in-memory index of the command history, so that queries never read the
// database. It is built in the background the first time it is needed, by a
// query or by Store.WarmIndex, and kept current by every write afterwards.
// Until then writes don't touch it: a daemon that only ever serves a sync
// never builds it. Entries are stored in columns, oldest first, with strings
// that repeat (directories, hosts, sessions) interned, so that the index
// costs tens of bytes per entry plus the text.
//
// The index belongs to the daemon process, which is the only one with the
// database open, so it exists once per machine however many shells run.
type cmdIndex struct {
	mu sync.RWMutex
	// Closed when the index has been built and pending updates applied.
	ready chan struct{}
	// Whether a build has started, and whether it has finished.
	started, built bool
	// Updates that arrived while the index was being built. Replayed once
	// the build finishes; they are idempotent, since the build may already
	// have seen their effects. Updates from before the build starts are not
	// kept: the build reads them from the database.
	pending []func()

	seqs []uint32
	// Text of all entries, concatenated; entry i is text[off[i]:off[i+1]].
	text []byte
	off  []uint32
	// Metadata columns. hasMeta is false for entries without metadata, whose
	// other columns are zero.
	hasMeta  []bool
	times    []int64
	durs     []float64
	exits    []int16
	dirs     []uint32
	hosts    []uint16
	sessions []uint32
	deleted  []bool

	dirTab, hostTab, sessTab stringTable
}

// Interns strings, so that each distinct value is stored once.
type stringTable struct {
	strs []string
	ids  map[string]uint32
}

func (t *stringTable) id(s string) uint32 {
	if id, ok := t.ids[s]; ok {
		return id
	}
	if t.ids == nil {
		t.ids = map[string]uint32{}
	}
	id := uint32(len(t.strs))
	t.strs = append(t.strs, s)
	t.ids[s] = id
	return id
}

func (t *stringTable) lookup(s string) (uint32, bool) {
	id, ok := t.ids[s]
	return id, ok
}

func newCmdIndex() *cmdIndex {
	return &cmdIndex{ready: make(chan struct{}), off: []uint32{0}}
}

// Starts building the index in the background, unless a build has already
// started. The wait group is used to register the build as an outstanding
// operation on the store.
func (ix *cmdIndex) start(db *bolt.DB, wg *sync.WaitGroup) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.started {
		return
	}
	// Set before the build goroutine runs, so that any write from now on is
	// queued and replayed; the build's transaction may or may not see it.
	ix.started = true
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ix.build(db); err != nil {
			logger.Printf("failed to build command index: %v", err)
		}
	}()
}

// Builds the index from the database. Writes that happen meanwhile are queued
// and applied after.
func (ix *cmdIndex) build(db *bolt.DB) error {
	ix.mu.Lock()
	ix.started = true
	ix.mu.Unlock()
	// Metadata that doesn't decode (for example written in an older format)
	// is skipped: the entry is indexed without it. One bad record must not
	// cost the whole history.
	badMeta := 0
	err := db.View(func(tx *bolt.Tx) error {
		cmds := tx.Bucket([]byte(bucketCmd)).Cursor()
		metas := tx.Bucket([]byte(bucketCmdMeta)).Cursor()
		mk, mv := metas.First()
		for k, v := cmds.First(); k != nil; k, v = cmds.Next() {
			seq := unmarshalSeq(k)
			ix.appendCmd(seq, string(v))
			// Metadata is keyed by the same sequence numbers, so one pass
			// over both buckets suffices.
			for mk != nil && unmarshalSeq(mk) < seq {
				mk, mv = metas.Next()
			}
			if mk != nil && unmarshalSeq(mk) == seq {
				var meta CmdMeta
				if err := json.Unmarshal(mv, &meta); err != nil {
					badMeta++
				} else {
					ix.setMeta(meta)
				}
			}
		}
		return nil
	})
	if badMeta > 0 {
		logger.Printf("command index: skipped metadata of %d entries that could not be decoded", badMeta)
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err != nil {
		// Leave the index empty but usable; queries will see nothing from
		// before this point.
		ix.reset()
	}
	ix.clip()
	for _, f := range ix.pending {
		f()
	}
	ix.pending = nil
	ix.built = true
	close(ix.ready)
	return err
}

// Frees the spare capacity that growing the columns by appending leaves
// behind, which can be as much as the data itself. Later appends grow them
// again, but far fewer entries are added after the build than during it.
func (ix *cmdIndex) clip() {
	ix.seqs = slices.Clip(ix.seqs)
	ix.text = slices.Clip(ix.text)
	ix.off = slices.Clip(ix.off)
	ix.hasMeta = slices.Clip(ix.hasMeta)
	ix.times = slices.Clip(ix.times)
	ix.durs = slices.Clip(ix.durs)
	ix.exits = slices.Clip(ix.exits)
	ix.dirs = slices.Clip(ix.dirs)
	ix.hosts = slices.Clip(ix.hosts)
	ix.sessions = slices.Clip(ix.sessions)
	ix.deleted = slices.Clip(ix.deleted)
}

// Empties the index. Must be called with ix.mu held; only the data is
// touched, never the lock or the ready channel.
func (ix *cmdIndex) reset() {
	ix.seqs, ix.text, ix.off = nil, nil, []uint32{0}
	ix.hasMeta, ix.times, ix.durs, ix.exits = nil, nil, nil, nil
	ix.dirs, ix.hosts, ix.sessions, ix.deleted = nil, nil, nil, nil
	ix.dirTab, ix.hostTab, ix.sessTab = stringTable{}, stringTable{}, stringTable{}
}

// Applies an update to the index, or queues it if the index is still being
// built. Updates must be idempotent. Before a build has started the update
// is dropped, since the build will read its effect from the database.
func (ix *cmdIndex) update(f func()) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	switch {
	case !ix.started:
	case !ix.built:
		ix.pending = append(ix.pending, f)
	default:
		f()
	}
}

// Position of the entry with the given sequence number, or -1.
func (ix *cmdIndex) pos(seq uint64) int {
	i := sort.Search(len(ix.seqs), func(i int) bool { return uint64(ix.seqs[i]) >= seq })
	if i < len(ix.seqs) && uint64(ix.seqs[i]) == seq {
		return i
	}
	return -1
}

// Appends an entry, unless one with the same or a higher sequence number
// already exists.
func (ix *cmdIndex) appendCmd(seq uint64, text string) {
	if n := len(ix.seqs); n > 0 && uint64(ix.seqs[n-1]) >= seq {
		return
	}
	ix.seqs = append(ix.seqs, uint32(seq))
	ix.text = append(ix.text, text...)
	ix.off = append(ix.off, uint32(len(ix.text)))
	ix.hasMeta = append(ix.hasMeta, false)
	ix.times = append(ix.times, 0)
	ix.durs = append(ix.durs, 0)
	ix.exits = append(ix.exits, 0)
	ix.dirs = append(ix.dirs, 0)
	ix.hosts = append(ix.hosts, 0)
	ix.sessions = append(ix.sessions, 0)
	ix.deleted = append(ix.deleted, false)
}

func (ix *cmdIndex) setMeta(meta CmdMeta) {
	i := ix.pos(uint64(meta.Seq))
	if i < 0 {
		return
	}
	ix.hasMeta[i] = true
	if meta.Time.IsZero() {
		ix.times[i] = 0
	} else {
		ix.times[i] = meta.Time.UnixNano()
	}
	ix.durs[i] = meta.Duration
	ix.exits[i] = int16(meta.Exit)
	ix.dirs[i] = ix.dirTab.id(meta.Dir)
	ix.hosts[i] = uint16(ix.hostTab.id(meta.Host))
	ix.sessions[i] = ix.sessTab.id(meta.Session)
}

func (ix *cmdIndex) delCmd(seq uint64) {
	if i := ix.pos(seq); i >= 0 {
		ix.deleted[i] = true
	}
}

// Text of entry i, as a view into the text buffer. The view is only valid
// while the lock is held, since appends may move the buffer.
func (ix *cmdIndex) textAt(i int) string {
	b := ix.text[ix.off[i]:ix.off[i+1]]
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}

func (ix *cmdIndex) metaAt(i int) CmdMeta {
	meta := CmdMeta{
		Seq: int(ix.seqs[i]), Duration: ix.durs[i], Exit: int(ix.exits[i]),
		Dir: ix.dirTab.strs[ix.dirs[i]], Host: ix.hostTab.strs[ix.hosts[i]],
		Session: ix.sessTab.strs[ix.sessions[i]],
	}
	if ix.times[i] != 0 {
		meta.Time = TimeFromNanos(ix.times[i])
	}
	return meta
}

// Runs a query. Waits for the index to be built first.
func (ix *cmdIndex) search(q CmdQuery) ([]CmdHit, error) {
	<-ix.ready
	m, err := q.Matcher()
	if err != nil {
		return nil, err
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	// Resolve interned filters once; a value that is not in a table matches
	// nothing.
	type idFilter struct {
		on bool
		id uint32
	}
	resolve := func(t *stringTable, s string) (idFilter, bool) {
		if s == "" {
			return idFilter{}, true
		}
		id, ok := t.lookup(s)
		return idFilter{true, id}, ok
	}
	dir, ok1 := resolve(&ix.dirTab, q.Dir)
	host, ok2 := resolve(&ix.hostTab, q.Host)
	sess, ok3 := resolve(&ix.sessTab, q.Session)
	if !ok1 || !ok2 || !ok3 {
		return nil, nil
	}
	otherMeta := q.Exit != "" || !q.Since.IsZero() || !q.Until.IsZero()

	start := len(ix.seqs) - 1
	if q.Before > 0 {
		start = sort.Search(len(ix.seqs), func(i int) bool { return int(ix.seqs[i]) >= q.Before }) - 1
	}
	var seen map[string]struct{}
	if q.Dedup {
		seen = map[string]struct{}{}
	}
	var hits []CmdHit
	for i := start; i >= 0; i-- {
		if ix.deleted[i] || !m.MatchSeq(int(ix.seqs[i])) {
			continue
		}
		if dir.on && (!ix.hasMeta[i] || ix.dirs[i] != dir.id) ||
			host.on && (!ix.hasMeta[i] || uint32(ix.hosts[i]) != host.id) ||
			sess.on && (!ix.hasMeta[i] || ix.sessions[i] != sess.id) {
			continue
		}
		if otherMeta {
			if !ix.hasMeta[i] {
				continue
			}
			meta := ix.metaAt(i)
			if !m.MatchMeta(&meta) {
				continue
			}
		}
		text := ix.textAt(i)
		if !m.MatchText(text) {
			continue
		}
		if seen != nil {
			if _, dup := seen[text]; dup {
				continue
			}
			seen[text] = struct{}{}
		}
		hit := CmdHit{Cmd: Cmd{Text: strings.Clone(text), Seq: int(ix.seqs[i])}}
		if ix.hasMeta[i] {
			hit.Meta = ix.metaAt(i)
		}
		hits = append(hits, hit)
		if q.Limit > 0 && len(hits) >= q.Limit {
			break
		}
	}
	return hits, nil
}

// SearchCmds returns the entries matching the query, newest first. The first
// query builds the index and waits for it.
func (s *dbStore) SearchCmds(q CmdQuery) ([]CmdHit, error) {
	s.idx.start(s.db, &s.wg)
	return s.idx.search(q)
}

// WarmIndex starts building the index in the background if it hasn't been
// built, so that the first query doesn't have to wait for it.
func (s *dbStore) WarmIndex() error {
	s.idx.start(s.db, &s.wg)
	return nil
}
