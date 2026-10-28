package edit

import (
	"sync"

	"src.elv.sh/pkg/cli/histutil"
	"src.elv.sh/pkg/store/storedefs"
)

// A wrapper of histutil.Store that is concurrency-safe and supports an
// additional FastForward method.
//
// The view of the database is frozen when the store is created and on each
// FastForward: commands added by other sessions since are not seen until the
// next FastForward, while commands added by this session always are. Queries
// go to the database, which holds the index, with that view as a bound; only
// a "no daemon" session searches its own commands in memory.
type histStore struct {
	m  sync.Mutex
	db storedefs.Store
	hs histutil.Store
	// Incremented whenever the content of the store may have changed. Used
	// for invalidating caches derived from the store.
	gen uint64
	// Sequence number bound of the frozen view: commands with sequence
	// numbers at or above it are not seen unless added by this session.
	upto int
	// Sequence numbers of commands added since the last FastForward.
	added []int
}

func newHistStore(db storedefs.Store) (*histStore, error) {
	hs, err := histutil.NewHybridStore(db)
	s := &histStore{db: db, hs: hs}
	if db != nil && err == nil {
		s.upto, err = db.NextCmdSeq()
	}
	return s, err
}

func (s *histStore) AddCmd(cmd storedefs.Cmd) (int, error) {
	s.m.Lock()
	defer s.m.Unlock()
	s.gen++
	seq, err := s.hs.AddCmd(cmd)
	if err == nil {
		s.added = append(s.added, seq)
	}
	return seq, err
}

// Generation returns a number that changes whenever the content of the store
// may have changed.
func (s *histStore) Generation() uint64 {
	s.m.Lock()
	defer s.m.Unlock()
	return s.gen
}

// Search runs a query over the commands in the store's view. The query's
// Upto and Own fields are set by this method.
func (s *histStore) Search(q storedefs.CmdQuery) ([]storedefs.CmdHit, error) {
	s.m.Lock()
	db, hs := s.db, s.hs
	q.Upto = s.upto
	q.Own = append([]int(nil), s.added...)
	s.m.Unlock()
	if db != nil {
		return db.SearchCmds(q)
	}
	// Without a database, the store only has this session's commands.
	cmds, err := hs.AllCmds()
	if err != nil {
		return nil, err
	}
	q.Upto, q.Own = 0, nil
	return storedefs.SearchSlice(cmds, nil, q)
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
	s.added = nil
	if s.db != nil && err == nil {
		s.upto, err = s.db.NextCmdSeq()
	}
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
