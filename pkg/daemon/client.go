package daemon

import (
	"errors"
	"io"
	"net"
	"sync"

	"src.elv.sh/pkg/daemon/daemondefs"
	"src.elv.sh/pkg/daemon/internal/api"
	"src.elv.sh/pkg/rpc"
	"src.elv.sh/pkg/store/storedefs"
)

const retriesOnShutdown = 3

var (
	// ErrDaemonUnreachable is returned when the daemon cannot be reached after
	// several retries.
	ErrDaemonUnreachable = errors.New("daemon offline")
)

// APIVersion is the version of the daemon API spoken by this package. A daemon
// reporting a smaller version is outdated.
const APIVersion = api.Version

// Implementation of the Client interface.
type client struct {
	sockPath  string
	rpcClient *rpc.Client
	waits     sync.WaitGroup
}

// NewClient creates a new Client instance that talks to the socket. Connection
// creation is deferred to the first request.
func NewClient(sockPath string) daemondefs.Client {
	return &client{sockPath, nil, sync.WaitGroup{}}
}

// NewClientConn creates a new Client instance that talks to a daemon over an
// existing connection, such as the standard streams of an "elvish -relay"
// process. Closing the client closes the connection. The client cannot
// reconnect: if the connection shuts down, requests fail with
// [ErrDaemonUnreachable].
func NewClientConn(conn io.ReadWriteCloser) daemondefs.Client {
	return &client{"", rpc.NewClient(conn), sync.WaitGroup{}}
}

// SockPath returns the socket path that the Client talks to. If the client is
// nil, it returns an empty string.
func (c *client) SockPath() string {
	return c.sockPath
}

// ResetConn resets the current connection. A new connection will be established
// the next time a request is made. If the client is nil, it does nothing.
func (c *client) ResetConn() error {
	if c.rpcClient == nil {
		return nil
	}
	rc := c.rpcClient
	c.rpcClient = nil
	return rc.Close()
}

// Close waits for all outstanding requests to finish and close the connection.
// If the client is nil, it does nothing and returns nil.
func (c *client) Close() error {
	c.waits.Wait()
	return c.ResetConn()
}

func (c *client) call(f string, req, res any) error {
	c.waits.Add(1)
	defer c.waits.Done()

	for attempt := 0; attempt < retriesOnShutdown; attempt++ {
		if c.rpcClient == nil {
			if c.sockPath == "" {
				// Created by NewClientConn; there is nothing to redial.
				break
			}
			conn, err := net.Dial("unix", c.sockPath)
			if err != nil {
				return err
			}
			c.rpcClient = rpc.NewClient(conn)
		}

		err := c.rpcClient.Call(api.ServiceName+"."+f, req, res)
		if err == rpc.ErrShutdown {
			// Clear rpcClient so as to reconnect next time
			c.rpcClient = nil
			continue
		} else {
			return err
		}
	}
	return ErrDaemonUnreachable
}

// Convenience methods for RPC methods. These are quite repetitive; when the
// number of RPC calls grow above some threshold, a code generator should be
// written to generate them.

func (c *client) Version() (int, error) {
	req := &api.VersionRequest{}
	res := &api.VersionResponse{}
	err := c.call("Version", req, res)
	return res.Version, err
}

func (c *client) Pid() (int, error) {
	req := &api.PidRequest{}
	res := &api.PidResponse{}
	err := c.call("Pid", req, res)
	return res.Pid, err
}

func (c *client) NextCmdSeq() (int, error) {
	req := &api.NextCmdRequest{}
	res := &api.NextCmdSeqResponse{}
	err := c.call("NextCmdSeq", req, res)
	return res.Seq, err
}

func (c *client) AddCmd(text string) (int, error) {
	req := &api.AddCmdRequest{Text: text}
	res := &api.AddCmdResponse{}
	err := c.call("AddCmd", req, res)
	return res.Seq, err
}

func (c *client) DelCmd(seq int) error {
	req := &api.DelCmdRequest{Seq: seq}
	res := &api.DelCmdResponse{}
	err := c.call("DelCmd", req, res)
	return err
}

func (c *client) Cmd(seq int) (string, error) {
	req := &api.CmdRequest{Seq: seq}
	res := &api.CmdResponse{}
	err := c.call("Cmd", req, res)
	return res.Text, err
}

func (c *client) CmdsWithSeq(from, upto int) ([]storedefs.Cmd, error) {
	req := &api.CmdsWithSeqRequest{From: from, Upto: upto}
	res := &api.CmdsWithSeqResponse{}
	err := c.call("CmdsWithSeq", req, res)
	return res.Cmds, err
}

func (c *client) NextCmd(from int, prefix string) (storedefs.Cmd, error) {
	req := &api.NextCmdRequest{From: from, Prefix: prefix}
	res := &api.NextCmdResponse{}
	err := c.call("NextCmd", req, res)
	return storedefs.Cmd{Text: res.Text, Seq: res.Seq}, err
}

func (c *client) PrevCmd(upto int, prefix string) (storedefs.Cmd, error) {
	req := &api.PrevCmdRequest{Upto: upto, Prefix: prefix}
	res := &api.PrevCmdResponse{}
	err := c.call("PrevCmd", req, res)
	return storedefs.Cmd{Text: res.Text, Seq: res.Seq}, err
}

func (c *client) HostID() (string, error) {
	req := &api.HostIDRequest{}
	res := &api.HostIDResponse{}
	err := c.call("HostID", req, res)
	return res.ID, err
}

func (c *client) SetCmdMeta(meta storedefs.CmdMeta) error {
	req := &api.SetCmdMetaRequest{Meta: meta}
	res := &api.SetCmdMetaResponse{}
	return c.call("SetCmdMeta", req, res)
}

func (c *client) CmdMeta(seq int) (storedefs.CmdMeta, error) {
	req := &api.CmdMetaRequest{Seq: seq}
	res := &api.CmdMetaResponse{}
	err := c.call("CmdMeta", req, res)
	return res.Meta, err
}

func (c *client) CmdsMeta(from, upto int) ([]storedefs.CmdMeta, error) {
	req := &api.CmdsMetaRequest{From: from, Upto: upto}
	res := &api.CmdsMetaResponse{}
	err := c.call("CmdsMeta", req, res)
	return res.Metas, err
}

func (c *client) DeletedCmds(from, upto int) ([]storedefs.CmdMeta, error) {
	req := &api.DeletedCmdsRequest{From: from, Upto: upto}
	res := &api.DeletedCmdsResponse{}
	err := c.call("DeletedCmds", req, res)
	return res.Metas, err
}

func (c *client) CommitCmdMeta(meta storedefs.CmdMeta) (storedefs.CmdMeta, error) {
	req := &api.CommitCmdMetaRequest{Meta: meta}
	res := &api.CommitCmdMetaResponse{}
	err := c.call("CommitCmdMeta", req, res)
	return res.Meta, err
}

func (c *client) Status() (storedefs.Status, error) {
	req := &api.StatusRequest{}
	res := &api.StatusResponse{}
	err := c.call("Status", req, res)
	return res.Status, err
}

func (c *client) Records(host, kind string, from uint64, limit int) ([]storedefs.Record, error) {
	req := &api.RecordsRequest{Host: host, Kind: kind, From: from, Limit: limit}
	res := &api.RecordsResponse{}
	err := c.call("Records", req, res)
	return res.Records, err
}

func (c *client) Ingest(records []storedefs.Record) error {
	req := &api.IngestRequest{Records: records}
	res := &api.IngestResponse{}
	return c.call("Ingest", req, res)
}

func (c *client) ResetSeries(host, kind string) (int, error) {
	req := &api.ResetSeriesRequest{Host: host, Kind: kind}
	res := &api.ResetSeriesResponse{}
	err := c.call("ResetSeries", req, res)
	return res.Removed, err
}

func (c *client) SearchCmds(q storedefs.CmdQuery) ([]storedefs.CmdHit, error) {
	req := &api.SearchCmdsRequest{Query: q}
	res := &api.SearchCmdsResponse{}
	err := c.call("SearchCmds", req, res)
	return res.Hits, err
}

func (c *client) WarmIndex() error {
	req := &api.WarmIndexRequest{}
	res := &api.WarmIndexResponse{}
	return c.call("WarmIndex", req, res)
}

func (c *client) AddDir(dir string, incFactor float64) error {
	req := &api.AddDirRequest{Dir: dir, IncFactor: incFactor}
	res := &api.AddDirResponse{}
	err := c.call("AddDir", req, res)
	return err
}

func (c *client) DelDir(dir string) error {
	req := &api.DelDirRequest{Dir: dir}
	res := &api.DelDirResponse{}
	err := c.call("DelDir", req, res)
	return err
}

func (c *client) Dirs(blacklist map[string]struct{}) ([]storedefs.Dir, error) {
	req := &api.DirsRequest{Blacklist: blacklist}
	res := &api.DirsResponse{}
	err := c.call("Dirs", req, res)
	return res.Dirs, err
}
