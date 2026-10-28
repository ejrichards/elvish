package store

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"uuid"

	bolt "go.etcd.io/bbolt"
	. "src.elv.sh/pkg/store/storedefs"
)

func init() {
	initDB["initialize series tables"] = func(tx *bolt.Tx) error {
		for _, name := range []string{bucketSeries, bucketIDs} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}
}

// Layout of keys in the series bucket: 16 bytes of host ID, one byte of
// series kind, and the index as a big-endian uint64. The last key under a
// (host, kind) prefix is the tail of that series.
//
// Indexes are assigned inside the write transaction, so they are contiguous
// without a counter bucket or a retry loop: the daemon is bbolt's only
// writer. Records are JSON for debuggability (msgpack would save about 40%)
// with times as integer nanoseconds, since parsing RFC 3339 strings was most
// of the cost of decoding all metadata at startup.
//
// An entry gets its add record only when the command finishes
// (CommitCmdMeta), so a record is never served half done; between AddCmd
// and completion the entry exists in the projection only. Ingest applies a
// batch in one transaction, so one fsync per batch rather than per record,
// which measured at about 1.7 ms each.
const (
	seriesPrefixLen = 16 + 1
	seriesKeyLen    = seriesPrefixLen + 8
)

var kindBytes = map[string]byte{KindCmd: 1}
var kindNames = map[byte]string{1: KindCmd}

// Value in the IDs bucket for an entry that has been deleted.
var deletedMarker = []byte{0}

func seriesPrefix(host, kind string) ([]byte, error) {
	id, err := uuid.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("bad host ID %q: %w", host, err)
	}
	k, ok := kindBytes[kind]
	if !ok {
		return nil, fmt.Errorf("bad series kind %q", kind)
	}
	return append(id[:], k), nil
}

// Always returns a fresh slice: bbolt keeps references to keys until the
// transaction commits, so keys must not share a backing array.
func seriesKey(prefix []byte, idx uint64) []byte {
	key := make([]byte, 0, seriesKeyLen)
	key = append(key, prefix...)
	return binary.BigEndian.AppendUint64(key, idx)
}

func seriesKeyIdx(key []byte) uint64 {
	return binary.BigEndian.Uint64(key[seriesPrefixLen:])
}

func seriesKeyPrefixInfo(key []byte) (host, kind string) {
	var id uuid.UUID
	copy(id[:], key[:16])
	return id.String(), kindNames[key[16]]
}

// Returns the index of the last record of a series, or 0 if the series is
// empty. Leaves the cursor positioned at the tail key, or at the first key
// after the series if it is empty.
func seriesTail(c *bolt.Cursor, prefix []byte) uint64 {
	// Seek past the series, then step back.
	k, _ := c.Seek(seriesKey(prefix, ^uint64(0)))
	if k == nil {
		k, _ = c.Last()
	} else if !bytes.Equal(k, seriesKey(prefix, ^uint64(0))) {
		k, _ = c.Prev()
	}
	if k != nil && bytes.HasPrefix(k, prefix) {
		return seriesKeyIdx(k)
	}
	return 0
}

func putRecord(tx *bolt.Tx, prefix []byte, r Record) error {
	v, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return tx.Bucket([]byte(bucketSeries)).Put(seriesKey(prefix, r.Idx), v)
}

// CommitCmdMeta sets the metadata of an entry and appends a record for it to
// its host's series if it has none yet.
func (s *dbStore) CommitCmdMeta(meta CmdMeta) (CmdMeta, error) {
	err := s.db.Update(func(tx *bolt.Tx) error {
		key := marshalSeq(uint64(meta.Seq))
		text := tx.Bucket([]byte(bucketCmd)).Get(key)
		if text == nil {
			return ErrNoMatchingCmd
		}
		metaBucket := tx.Bucket([]byte(bucketCmdMeta))
		var existing CmdMeta
		if v := metaBucket.Get(key); v != nil {
			if err := json.Unmarshal(v, &existing); err != nil {
				return err
			}
		}
		if existing.Idx != 0 {
			// Already committed; only update the metadata.
			meta.Idx = existing.Idx
		} else {
			if meta.ID == "" {
				return errors.New("cannot commit metadata without an ID")
			}
			prefix, err := seriesPrefix(meta.Host, KindCmd)
			if err != nil {
				return err
			}
			c := tx.Bucket([]byte(bucketSeries)).Cursor()
			meta.Idx = seriesTail(c, prefix) + 1
			if err := putRecord(tx, prefix, addRecord(meta, string(text))); err != nil {
				return err
			}
			if err := tx.Bucket([]byte(bucketIDs)).Put([]byte(meta.ID), key); err != nil {
				return err
			}
		}
		v, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return metaBucket.Put(key, v)
	})
	if err == nil {
		s.idx.update(func() { s.idx.setMeta(meta) })
	}
	return meta, err
}

func addRecord(meta CmdMeta, text string) Record {
	return Record{
		Version: RecordVersion, Kind: KindCmd, Host: meta.Host, Idx: meta.Idx,
		Op: OpAdd, ID: meta.ID, Time: meta.Time,
		Text: text, Duration: meta.Duration, Exit: meta.Exit,
		Dir: meta.Dir, Session: meta.Session,
	}
}

func metaOfRecord(r Record, seq int) CmdMeta {
	return CmdMeta{
		Seq: seq, ID: r.ID, Time: r.Time, Duration: r.Duration, Exit: r.Exit,
		Host: r.Host, Dir: r.Dir, Session: r.Session, Idx: r.Idx,
	}
}

// Appends a record to the deleting host's series for the deletion of the
// entry with the given metadata, and marks its ID as deleted. Must be called
// within a writable transaction. Entries without an ID have no record of
// their own and cannot be referred to, so nothing is recorded for them.
func recordDeletion(tx *bolt.Tx, host string, meta CmdMeta) error {
	if meta.ID == "" {
		return nil
	}
	prefix, err := seriesPrefix(host, KindCmd)
	if err != nil {
		return err
	}
	c := tx.Bucket([]byte(bucketSeries)).Cursor()
	r := Record{
		Version: RecordVersion, Kind: KindCmd, Host: host,
		Idx: seriesTail(c, prefix) + 1, Op: OpDel, ID: NewID(),
		Time: time.Now(), Target: meta.ID,
	}
	if err := putRecord(tx, prefix, r); err != nil {
		return err
	}
	return tx.Bucket([]byte(bucketIDs)).Put([]byte(meta.ID), deletedMarker)
}

// Sets the index in the metadata of the entry at key to idx, if it differs.
// Must be called within a writable transaction.
func restoreIdx(tx *bolt.Tx, key []byte, idx uint64, updates *[]func(*cmdIndex)) error {
	metas := tx.Bucket([]byte(bucketCmdMeta))
	v := metas.Get(key)
	if v == nil {
		return nil
	}
	var meta CmdMeta
	if err := json.Unmarshal(v, &meta); err != nil {
		return err
	}
	if meta.Idx == idx {
		return nil
	}
	meta.Idx = idx
	v, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := metas.Put(key, v); err != nil {
		return err
	}
	*updates = append(*updates, func(ix *cmdIndex) { ix.setMeta(meta) })
	return nil
}

// ResetSeries removes every record of a series from the store, and clears
// the index of the entries those records had added, so that they are
// uncommitted again. The entries themselves, and any deletions, stay as
// they are. Returns the number of records removed.
//
// This is how a host whose copy of a series has diverged from its peers'
// gets back in step: reset the series, ingest the peers' copy, which
// restores the index of entries it has, then commit the rest anew.
func (s *dbStore) ResetSeries(host, kind string) (int, error) {
	prefix, err := seriesPrefix(host, kind)
	if err != nil {
		return 0, err
	}
	n := 0
	var updates []func(*cmdIndex)
	err = s.db.Update(func(tx *bolt.Tx) error {
		series := tx.Bucket([]byte(bucketSeries))
		ids := tx.Bucket([]byte(bucketIDs))
		var keys [][]byte
		var added []string
		c := series.Cursor()
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			keys = append(keys, append([]byte(nil), k...))
			var r Record
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			if r.Op == OpAdd {
				added = append(added, r.ID)
			}
		}
		for _, k := range keys {
			if err := series.Delete(k); err != nil {
				return err
			}
		}
		for _, id := range added {
			if key := ids.Get([]byte(id)); len(key) == 8 {
				if err := restoreIdx(tx, key, 0, &updates); err != nil {
					return err
				}
			}
		}
		n = len(keys)
		return nil
	})
	if err == nil {
		s.idx.update(func() {
			for _, f := range updates {
				f(s.idx)
			}
		})
	}
	return n, err
}

// Status returns the tail index of every series.
func (s *dbStore) Status() (Status, error) {
	status := Status{}
	err := s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucketSeries)).Cursor()
		for k, _ := c.First(); k != nil; {
			prefix := append([]byte(nil), k[:seriesPrefixLen]...)
			tail := seriesTail(c, prefix)
			host, kind := seriesKeyPrefixInfo(prefix)
			if status[host] == nil {
				status[host] = map[string]uint64{}
			}
			status[host][kind] = tail
			// Move to the first key of the next series.
			k, _ = c.Seek(seriesKey(prefix, ^uint64(0)))
			if k != nil && bytes.HasPrefix(k, prefix) {
				k, _ = c.Next()
			}
		}
		return nil
	})
	return status, err
}

// Records returns records of one series from the given index.
func (s *dbStore) Records(host, kind string, from uint64, limit int) ([]Record, error) {
	prefix, err := seriesPrefix(host, kind)
	if err != nil {
		return nil, err
	}
	var records []Record
	err = s.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket([]byte(bucketSeries)).Cursor()
		for k, v := c.Seek(seriesKey(prefix, from)); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			if limit > 0 && len(records) >= limit {
				break
			}
			var r Record
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			records = append(records, r)
		}
		return nil
	})
	return records, err
}

// Ingest appends records to their series and applies them to the command
// history.
func (s *dbStore) Ingest(records []Record) error {
	// Index updates are collected and applied only once the transaction has
	// committed, since it may still fail.
	var updates []func(*cmdIndex)
	err := s.db.Update(func(tx *bolt.Tx) error {
		updates = updates[:0]
		for _, r := range records {
			if err := ingestRecord(tx, r, &updates); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		s.idx.update(func() {
			for _, f := range updates {
				f(s.idx)
			}
		})
	}
	return err
}

func ingestRecord(tx *bolt.Tx, r Record, updates *[]func(*cmdIndex)) error {
	if r.Version != RecordVersion {
		return fmt.Errorf("record %s: unsupported version %d", r.ID, r.Version)
	}
	if r.ID == "" {
		return errors.New("record without an ID")
	}
	prefix, err := seriesPrefix(r.Host, r.Kind)
	if err != nil {
		return fmt.Errorf("record %s: %w", r.ID, err)
	}
	c := tx.Bucket([]byte(bucketSeries)).Cursor()
	if tail := seriesTail(c, prefix); r.Idx != tail+1 {
		return fmt.Errorf("record %s: index %d does not follow series tail %d",
			r.ID, r.Idx, tail)
	}
	ids := tx.Bucket([]byte(bucketIDs))
	switch r.Op {
	case OpAdd:
		if err := putRecord(tx, prefix, r); err != nil {
			return err
		}
		if key := ids.Get([]byte(r.ID)); key != nil {
			// Already present or deleted; the record is kept for the series
			// but not projected again. A present entry does get the record's
			// index, which a ResetSeries may have cleared.
			if len(key) == 8 {
				return restoreIdx(tx, key, r.Idx, updates)
			}
			return nil
		}
		cmds := tx.Bucket([]byte(bucketCmd))
		seq, err := cmds.NextSequence()
		if err != nil {
			return err
		}
		key := marshalSeq(seq)
		if err := cmds.Put(key, []byte(r.Text)); err != nil {
			return err
		}
		meta := metaOfRecord(r, int(seq))
		v, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if err := tx.Bucket([]byte(bucketCmdMeta)).Put(key, v); err != nil {
			return err
		}
		*updates = append(*updates, func(ix *cmdIndex) {
			ix.appendCmd(seq, r.Text)
			ix.setMeta(meta)
		})
		return ids.Put([]byte(r.ID), key)
	case OpDel:
		if r.Target == "" {
			return fmt.Errorf("record %s: deletion without a target", r.ID)
		}
		if err := putRecord(tx, prefix, r); err != nil {
			return err
		}
		if key := ids.Get([]byte(r.Target)); len(key) == 8 {
			if err := tombstoneCmdMeta(tx, key); err != nil {
				return err
			}
			if err := tx.Bucket([]byte(bucketCmd)).Delete(key); err != nil {
				return err
			}
			seq := unmarshalSeq(key)
			*updates = append(*updates, func(ix *cmdIndex) { ix.delCmd(seq) })
		}
		return ids.Put([]byte(r.Target), deletedMarker)
	default:
		return fmt.Errorf("record %s: bad operation %q", r.ID, r.Op)
	}
}
