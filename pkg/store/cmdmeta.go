package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"uuid"

	bolt "go.etcd.io/bbolt"
	. "src.elv.sh/pkg/store/storedefs"
)

func init() {
	initDB["initialize command metadata tables"] = func(tx *bolt.Tx) error {
		for _, name := range []string{bucketCmdMeta, bucketCmdDel} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}
}

// Name of the file holding the host ID, in the same directory as the database.
const hostIDFile = "host-id"

// HostID returns the host ID, reading it from the host ID file next to the
// database or generating and writing one if the file doesn't exist. The ID is
// kept outside the database so that it survives the database being deleted.
func (s *dbStore) HostID() (string, error) {
	s.hostIDMutex.Lock()
	defer s.hostIDMutex.Unlock()
	if s.hostID != "" {
		return s.hostID, nil
	}
	path := filepath.Join(filepath.Dir(s.db.Path()), hostIDFile)
	if content, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(content))
		if _, err := uuid.Parse(id); err == nil {
			s.hostID = id
			return id, nil
		}
		logger.Printf("ignoring invalid host ID in %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id := NewID()
	// Write to a temporary file and rename, so that a concurrent reader never
	// sees a partially written file.
	tmp, err := os.CreateTemp(filepath.Dir(path), hostIDFile+".*.tmp")
	if err != nil {
		return "", err
	}
	_, err = tmp.WriteString(id + "\n")
	if err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0600)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	s.hostID = id
	return id, nil
}

// SetCmdMeta sets the metadata of a command history entry. The index of the
// entry's record, if it has one, is kept; it is owned by the series.
func (s *dbStore) SetCmdMeta(meta CmdMeta) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		key := marshalSeq(uint64(meta.Seq))
		if tx.Bucket([]byte(bucketCmd)).Get(key) == nil {
			return ErrNoMatchingCmd
		}
		meta.Idx = 0
		if old := tx.Bucket([]byte(bucketCmdMeta)).Get(key); old != nil {
			var existing CmdMeta
			if err := json.Unmarshal(old, &existing); err != nil {
				return err
			}
			meta.Idx = existing.Idx
		}
		v, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return tx.Bucket([]byte(bucketCmdMeta)).Put(key, v)
	})
	if err == nil {
		s.idx.update(func() { s.idx.setMeta(meta) })
	}
	return err
}

// CmdMeta returns the metadata of a command history entry.
func (s *dbStore) CmdMeta(seq int) (CmdMeta, error) {
	var meta CmdMeta
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket([]byte(bucketCmdMeta)).Get(marshalSeq(uint64(seq)))
		if v == nil {
			return ErrNoCmdMeta
		}
		return json.Unmarshal(v, &meta)
	})
	return meta, err
}

// CmdsMeta returns the metadata of all command history entries in the
// specified range that have metadata.
func (s *dbStore) CmdsMeta(from, upto int) ([]CmdMeta, error) {
	return s.iterateMeta(bucketCmdMeta, from, upto)
}

// DeletedCmds returns the metadata of deleted command history entries in the
// specified range.
func (s *dbStore) DeletedCmds(from, upto int) ([]CmdMeta, error) {
	return s.iterateMeta(bucketCmdDel, from, upto)
}

func (s *dbStore) iterateMeta(bucket string, from, upto int) ([]CmdMeta, error) {
	var metas []CmdMeta
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucket)).Cursor()
		for k, v := c.Seek(marshalSeq(uint64(from))); k != nil && unmarshalSeq(k) < uint64(upto); k, v = c.Next() {
			var meta CmdMeta
			if err := json.Unmarshal(v, &meta); err != nil {
				return err
			}
			metas = append(metas, meta)
		}
		return nil
	})
	return metas, err
}

// Moves the metadata of a command history entry, if any, to the deleted
// bucket. Must be called within a writable transaction.
func tombstoneCmdMeta(tx *bolt.Tx, key []byte) error {
	meta := tx.Bucket([]byte(bucketCmdMeta))
	v := meta.Get(key)
	if v == nil {
		return nil
	}
	// Copy the value: it is only valid for the lifetime of the transaction
	// and Put on another bucket may remap pages.
	cp := append([]byte(nil), v...)
	if err := tx.Bucket([]byte(bucketCmdDel)).Put(key, cp); err != nil {
		return err
	}
	return meta.Delete(key)
}
