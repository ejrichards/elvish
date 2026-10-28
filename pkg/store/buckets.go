package store

const (
	bucketCmd = "cmd"
	bucketDir = "dir"
	// Metadata of command history entries, keyed by sequence number.
	bucketCmdMeta = "cmdmeta"
	// Metadata of deleted command history entries, keyed by sequence number.
	bucketCmdDel = "cmddel"
	// Records of all series, keyed by host ID, kind and index; see record.go.
	bucketSeries = "series"
	// Command history entry ID to sequence number, or to a deleted marker.
	bucketIDs = "ids"
)

// The following buckets were used before and are thus reserved:
// "schema"
