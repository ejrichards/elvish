// Package api defines types and constants useful for the API between the daemon
// service and client.
package api

import (
	"src.elv.sh/pkg/store/storedefs"
)

// Version is the API version. It should be bumped (increased) any time the API
// changes: a client treats a daemon reporting a smaller version as outdated.
const Version = -92

// ServiceName is the name of the RPC service exposed by the daemon.
const ServiceName = "Daemon"

// Basic requests.

type VersionRequest struct{}

type VersionResponse struct {
	Version int
}

type PidRequest struct{}

type PidResponse struct {
	Pid int
}

// Cmd requests.

type NextCmdSeqRequest struct{}

type NextCmdSeqResponse struct {
	Seq int
}

type AddCmdRequest struct {
	Text string
}

type AddCmdResponse struct {
	Seq int
}

type DelCmdRequest struct {
	Seq int
}

type DelCmdResponse struct {
}

type CmdRequest struct {
	Seq int
}

type CmdResponse struct {
	Text string
}

type CmdsRequest struct {
	From int
	Upto int
}

type CmdsResponse struct {
	Cmds []string
}

type CmdsWithSeqRequest struct {
	From int
	Upto int
}

type CmdsWithSeqResponse struct {
	Cmds []storedefs.Cmd
}

type NextCmdRequest struct {
	From   int
	Prefix string
}

type NextCmdResponse struct {
	Seq  int
	Text string
}

type PrevCmdRequest struct {
	Upto   int
	Prefix string
}

type PrevCmdResponse struct {
	Seq  int
	Text string
}

// Cmd metadata requests.

type HostIDRequest struct{}

type HostIDResponse struct {
	ID string
}

type SetCmdMetaRequest struct {
	Meta storedefs.CmdMeta
}

type SetCmdMetaResponse struct{}

type CmdMetaRequest struct {
	Seq int
}

type CmdMetaResponse struct {
	Meta storedefs.CmdMeta
}

type CmdsMetaRequest struct {
	From int
	Upto int
}

type CmdsMetaResponse struct {
	Metas []storedefs.CmdMeta
}

type DeletedCmdsRequest struct {
	From int
	Upto int
}

type DeletedCmdsResponse struct {
	Metas []storedefs.CmdMeta
}

type CommitCmdMetaRequest struct {
	Meta storedefs.CmdMeta
}

type CommitCmdMetaResponse struct {
	Meta storedefs.CmdMeta
}

// Series requests.

type StatusRequest struct{}

type StatusResponse struct {
	Status storedefs.Status
}

type RecordsRequest struct {
	Host  string
	Kind  string
	From  uint64
	Limit int
}

type RecordsResponse struct {
	Records []storedefs.Record
}

type IngestRequest struct {
	Records []storedefs.Record
}

type IngestResponse struct{}

type SearchCmdsRequest struct {
	Query storedefs.CmdQuery
}

type SearchCmdsResponse struct {
	Hits []storedefs.CmdHit
}

// Dir requests.

type AddDirRequest struct {
	Dir       string
	IncFactor float64
}

type AddDirResponse struct{}

type DelDirRequest struct {
	Dir string
}

type DelDirResponse struct{}

type DirsRequest struct {
	Blacklist map[string]struct{}
}

type DirsResponse struct {
	Dirs []storedefs.Dir
}
