package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
	"src.elv.sh/pkg/testutil"
)

// MustTempStore returns a Store backed by a file in a temporary directory for
// testing. The Store and the directory will be cleaned up properly after the
// test is finished.
func MustTempStore(c testutil.Cleanuper) DBStore {
	dir, err := os.MkdirTemp("", "elvish.test")
	if err != nil {
		panic(fmt.Sprintf("create temp dir: %v", err))
	}
	db, err := bolt.Open(filepath.Join(dir, "db.bolt"), 0644, &bolt.Options{
		Timeout: time.Second, NoSync: true, NoFreelistSync: true})
	if err != nil {
		panic(fmt.Sprintf("open boltdb: %v", err))
	}

	st, err := NewStoreFromDB(db)
	if err != nil {
		panic(fmt.Sprintf("create Store instance: %v", err))
	}
	c.Cleanup(func() {
		st.Close()
		err = os.RemoveAll(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to remove temp dir:", err)
		}
	})
	return st
}
