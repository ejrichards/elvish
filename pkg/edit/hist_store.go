package edit

import (
	"sync"

	"src.elv.sh/pkg/cli/histutil"
	"src.elv.sh/pkg/store/storedefs"
)

// A wrapper of histutil.Store that is concurrency-safe and supports an
// additional FastForward method.
type histStore struct {
	m  sync.Mutex
	db storedefs.Store
	hs histutil.Store
	// Incremented whenever the content of the store may have changed. Used
	// for invalidating caches derived from the store.
	gen uint64
	// Incremented on each FastForward.
	ffGen uint64
	// Commands added since the last FastForward.
	added []storedefs.Cmd
}

func newHistStore(db storedefs.Store) (*histStore, error) {
	hs, err := histutil.NewHybridStore(db)
	return &histStore{db: db, hs: hs}, err
}

func (s *histStore) AddCmd(cmd storedefs.Cmd) (int, error) {
	s.m.Lock()
	defer s.m.Unlock()
	s.gen++
	seq, err := s.hs.AddCmd(cmd)
	s.added = append(s.added, storedefs.Cmd{Text: cmd.Text, Seq: seq})
	return seq, err
}

// Generation returns a number that changes whenever the content of the store
// may have changed.
func (s *histStore) Generation() uint64 {
	s.m.Lock()
	defer s.m.Unlock()
	return s.gen
}

// A snapshot of all commands in the store, in oldest to newest order.
type histSnapshot struct {
	ffGen  uint64
	cmds   []storedefs.Cmd
	nAdded int
}

// Snapshot returns a snapshot of all commands in the store. If old is a
// snapshot taken since the last FastForward, it is updated incrementally with
// the commands added since (which is cheap); otherwise all commands are
// retrieved from the underlying store.
func (s *histStore) Snapshot(old *histSnapshot) (*histSnapshot, error) {
	s.m.Lock()
	defer s.m.Unlock()
	if old != nil && old.ffGen == s.ffGen {
		if len(s.added) == old.nAdded {
			return old, nil
		}
		// Make a copy to avoid mutating the old snapshot's backing array.
		cmds := make([]storedefs.Cmd, 0, len(old.cmds)+len(s.added)-old.nAdded)
		cmds = append(cmds, old.cmds...)
		cmds = append(cmds, s.added[old.nAdded:]...)
		return &histSnapshot{s.ffGen, cmds, len(s.added)}, nil
	}
	cmds, err := s.hs.AllCmds()
	if err != nil {
		return nil, err
	}
	return &histSnapshot{s.ffGen, cmds, len(s.added)}, nil
}

// AllCmds returns a slice of all interactive commands in oldest to newest order.
func (s *histStore) AllCmds() ([]storedefs.Cmd, error) {
	s.m.Lock()
	defer s.m.Unlock()
	return s.hs.AllCmds()
}

func (s *histStore) Cursor(prefix string) histutil.Cursor {
	s.m.Lock()
	defer s.m.Unlock()
	return cursor{&s.m, histutil.NewDedupCursor(s.hs.Cursor(prefix))}
}

func (s *histStore) FastForward() error {
	s.m.Lock()
	defer s.m.Unlock()
	hs, err := histutil.NewHybridStore(s.db)
	s.hs = hs
	s.gen++
	s.ffGen++
	s.added = nil
	return err
}

type cursor struct {
	m *sync.Mutex
	c histutil.Cursor
}

func (c cursor) Prev() {
	c.m.Lock()
	defer c.m.Unlock()
	c.c.Prev()
}

func (c cursor) Next() {
	c.m.Lock()
	defer c.m.Unlock()
	c.c.Next()
}

func (c cursor) Get() (storedefs.Cmd, error) {
	c.m.Lock()
	defer c.m.Unlock()
	return c.c.Get()
}
