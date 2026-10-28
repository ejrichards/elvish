// Package storedefs contains definitions of the store API.
//
// It is a separate package so that packages that only depend on the store API
// does not need to depend on the concrete implementation.
package storedefs

import (
	"encoding/json"
	"errors"
	"time"
	"uuid"
)

// NoBlacklist is an empty blacklist, to be used in GetDirs.
var NoBlacklist = map[string]struct{}{}

// ErrNoMatchingCmd is the error returned when a LastCmd or FirstCmd query
// completes with no result.
var ErrNoMatchingCmd = errors.New("no matching command line")

// ErrNoCmdMeta is the error returned when querying the metadata of a command
// history entry that has none.
var ErrNoCmdMeta = errors.New("no metadata for command line")

// Store is an interface satisfied by the storage service.
type Store interface {
	NextCmdSeq() (int, error)
	AddCmd(text string) (int, error)
	DelCmd(seq int) error
	Cmd(seq int) (string, error)
	CmdsWithSeq(from, upto int) ([]Cmd, error)
	NextCmd(from int, prefix string) (Cmd, error)
	PrevCmd(upto int, prefix string) (Cmd, error)

	// HostID returns a stable identifier for the store, generated the first
	// time it is requested. It identifies the host that owns the store when
	// command history is shared with other hosts.
	HostID() (string, error)
	// SetCmdMeta sets the metadata of the command history entry identified by
	// meta.Seq, replacing any existing metadata. The entry itself must exist.
	SetCmdMeta(meta CmdMeta) error
	// CmdMeta returns the metadata of the command history entry with the given
	// sequence number. It returns ErrNoCmdMeta if the entry has no metadata.
	CmdMeta(seq int) (CmdMeta, error)
	// CmdsMeta returns the metadata of all command history entries with
	// sequence numbers in [from, upto) that have metadata.
	CmdsMeta(from, upto int) ([]CmdMeta, error)
	// DeletedCmds returns the metadata of deleted command history entries
	// with sequence numbers in [from, upto). Only entries that had metadata
	// when deleted are recorded.
	DeletedCmds(from, upto int) ([]CmdMeta, error)
	// CommitCmdMeta sets the metadata of an entry like SetCmdMeta, and if the
	// entry has no record in its host's series yet, appends one and assigns
	// its index. It returns the metadata as stored, with Idx set. Once an
	// entry has a record, further commits only update the metadata.
	CommitCmdMeta(meta CmdMeta) (CmdMeta, error)
	// Status returns the tail index of every series in the store.
	Status() (Status, error)
	// Records returns up to limit records of the series identified by host
	// and kind, starting at index from. A limit of 0 or less means no limit.
	Records(host, kind string, from uint64, limit int) ([]Record, error)
	// Ingest appends records to their series and applies them to the
	// command history, all in one transaction. Each record must extend its
	// series contiguously, i.e. its index must be the series tail plus one.
	Ingest(records []Record) error
	// SearchCmds returns the command history entries matching the query,
	// newest first, with their metadata.
	SearchCmds(q CmdQuery) ([]CmdHit, error)

	AddDir(dir string, incFactor float64) error
	DelDir(dir string) error
	Dirs(blacklist map[string]struct{}) ([]Dir, error)
}

// CmdMeta is metadata about an entry in the command history.
type CmdMeta struct {
	// Sequence number of the entry in the store.
	Seq int `json:"seq"`
	// Globally unique ID of the entry, generated with NewID.
	ID string `json:"id"`
	// When the command started.
	Time time.Time `json:"time"`
	// How long the command ran, in seconds. Zero until the command finishes.
	Duration float64 `json:"duration"`
	// Exit status of the command. Zero until the command finishes.
	Exit int `json:"exit"`
	// ID of the host the command was run on, as returned by Store.HostID.
	Host string `json:"host"`
	// Working directory the command was run in.
	Dir string `json:"dir"`
	// ID of the interactive session the command was run in.
	Session string `json:"session"`
	// Index of the entry's record in its host's series. Zero until the entry
	// is committed with Store.CommitCmdMeta or created by Store.Ingest.
	Idx uint64 `json:"idx"`
}

// Record is an entry in the append-only series of a host. Series are the
// source of truth for syncing; the command history is a projection of them.
type Record struct {
	// Format version of the record. Currently always RecordVersion.
	Version int `json:"v"`
	// Kind of the series the record belongs to. Currently always KindCmd.
	Kind string `json:"kind"`
	// ID of the host that owns the series.
	Host string `json:"host"`
	// Index of the record in its series, starting at 1 and contiguous.
	Idx uint64 `json:"idx"`
	// Operation: OpAdd or OpDel.
	Op string `json:"op"`
	// Globally unique ID of the record. For OpAdd this is the ID of the
	// command history entry it creates.
	ID string `json:"id"`
	// When the command started (OpAdd) or was deleted (OpDel).
	Time time.Time `json:"time"`

	// Fields of OpAdd records; see CmdMeta.
	Text     string  `json:"text,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	Exit     int     `json:"exit,omitempty"`
	Dir      string  `json:"dir,omitempty"`
	Session  string  `json:"session,omitempty"`

	// Field of OpDel records: ID of the deleted command history entry.
	Target string `json:"target,omitempty"`
}

// Record format version, series kinds and operations.
const (
	// Version 1 stored times as RFC 3339 strings; version 2 stores them as
	// nanoseconds since the Unix epoch, which is much cheaper to decode.
	RecordVersion = 2

	KindCmd = "cmd"

	OpAdd = "add"
	OpDel = "del"
)

// Times are stored in JSON as integer nanoseconds since the Unix epoch, with
// 0 for the zero time. Decoding an integer is several times cheaper than
// parsing the RFC 3339 string time.Time produces by default, and reading all
// metadata at once is a common operation.

func (m CmdMeta) MarshalJSON() ([]byte, error) {
	type alias CmdMeta
	return json.Marshal(struct {
		alias
		Time int64 `json:"time"`
	}{alias(m), timeToJSON(m.Time)})
}

func (m *CmdMeta) UnmarshalJSON(b []byte) error {
	type alias CmdMeta
	aux := struct {
		*alias
		Time int64 `json:"time"`
	}{alias: (*alias)(m)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	m.Time = timeFromJSON(aux.Time)
	return nil
}

func (r Record) MarshalJSON() ([]byte, error) {
	type alias Record
	return json.Marshal(struct {
		alias
		Time int64 `json:"time"`
	}{alias(r), timeToJSON(r.Time)})
}

func (r *Record) UnmarshalJSON(b []byte) error {
	type alias Record
	aux := struct {
		*alias
		Time int64 `json:"time"`
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	r.Time = timeFromJSON(aux.Time)
	return nil
}

func timeToJSON(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func timeFromJSON(ns int64) time.Time {
	return TimeFromNanos(ns)
}

// TimeFromNanos converts nanoseconds since the Unix epoch to a time in UTC,
// with 0 meaning the zero time.
func TimeFromNanos(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

// Status maps host ID to series kind to the index of the last record in that
// series. Series with no records are absent.
type Status map[string]map[string]uint64

// NewID returns a new random, time-ordered ID in the form of a UUID version 7.
func NewID() string {
	return uuid.NewV7().String()
}

// Dir is an entry in the directory history.
type Dir struct {
	Path  string
	Score float64
}

// Cmd is an entry in the command history.
type Cmd struct {
	Text string
	Seq  int
}
