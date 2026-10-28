package daemon

import (
	"syscall"

	"src.elv.sh/pkg/daemon/internal/api"
	"src.elv.sh/pkg/store/storedefs"
)

// A net/rpc service for the daemon.
type service struct {
	version int
	store   storedefs.Store
	err     error
}

// Implementations of RPC methods.

// Version returns the API version number.
func (s *service) Version(req *api.VersionRequest, res *api.VersionResponse) error {
	res.Version = s.version
	return nil
}

// Pid returns the process ID of the daemon.
func (s *service) Pid(req *api.PidRequest, res *api.PidResponse) error {
	res.Pid = syscall.Getpid()
	return nil
}

func (s *service) NextCmdSeq(req *api.NextCmdSeqRequest, res *api.NextCmdSeqResponse) error {
	if s.err != nil {
		return s.err
	}
	seq, err := s.store.NextCmdSeq()
	res.Seq = seq
	return err
}

func (s *service) AddCmd(req *api.AddCmdRequest, res *api.AddCmdResponse) error {
	if s.err != nil {
		return s.err
	}
	seq, err := s.store.AddCmd(req.Text)
	res.Seq = seq
	return err
}

func (s *service) DelCmd(req *api.DelCmdRequest, res *api.DelCmdResponse) error {
	if s.err != nil {
		return s.err
	}
	err := s.store.DelCmd(req.Seq)
	return err
}

func (s *service) Cmd(req *api.CmdRequest, res *api.CmdResponse) error {
	if s.err != nil {
		return s.err
	}
	text, err := s.store.Cmd(req.Seq)
	res.Text = text
	return err
}

func (s *service) CmdsWithSeq(req *api.CmdsWithSeqRequest, res *api.CmdsWithSeqResponse) error {
	if s.err != nil {
		return s.err
	}
	cmds, err := s.store.CmdsWithSeq(req.From, req.Upto)
	res.Cmds = cmds
	return err
}

func (s *service) NextCmd(req *api.NextCmdRequest, res *api.NextCmdResponse) error {
	if s.err != nil {
		return s.err
	}
	cmd, err := s.store.NextCmd(req.From, req.Prefix)
	res.Seq, res.Text = cmd.Seq, cmd.Text
	return err
}

func (s *service) PrevCmd(req *api.PrevCmdRequest, res *api.PrevCmdResponse) error {
	if s.err != nil {
		return s.err
	}
	cmd, err := s.store.PrevCmd(req.Upto, req.Prefix)
	res.Seq, res.Text = cmd.Seq, cmd.Text
	return err
}

func (s *service) HostID(req *api.HostIDRequest, res *api.HostIDResponse) error {
	if s.err != nil {
		return s.err
	}
	id, err := s.store.HostID()
	res.ID = id
	return err
}

func (s *service) SetCmdMeta(req *api.SetCmdMetaRequest, res *api.SetCmdMetaResponse) error {
	if s.err != nil {
		return s.err
	}
	return s.store.SetCmdMeta(req.Meta)
}

func (s *service) CmdMeta(req *api.CmdMetaRequest, res *api.CmdMetaResponse) error {
	if s.err != nil {
		return s.err
	}
	meta, err := s.store.CmdMeta(req.Seq)
	res.Meta = meta
	return err
}

func (s *service) CmdsMeta(req *api.CmdsMetaRequest, res *api.CmdsMetaResponse) error {
	if s.err != nil {
		return s.err
	}
	metas, err := s.store.CmdsMeta(req.From, req.Upto)
	res.Metas = metas
	return err
}

func (s *service) DeletedCmds(req *api.DeletedCmdsRequest, res *api.DeletedCmdsResponse) error {
	if s.err != nil {
		return s.err
	}
	metas, err := s.store.DeletedCmds(req.From, req.Upto)
	res.Metas = metas
	return err
}

func (s *service) CommitCmdMeta(req *api.CommitCmdMetaRequest, res *api.CommitCmdMetaResponse) error {
	if s.err != nil {
		return s.err
	}
	meta, err := s.store.CommitCmdMeta(req.Meta)
	res.Meta = meta
	return err
}

func (s *service) Status(req *api.StatusRequest, res *api.StatusResponse) error {
	if s.err != nil {
		return s.err
	}
	status, err := s.store.Status()
	res.Status = status
	return err
}

func (s *service) Records(req *api.RecordsRequest, res *api.RecordsResponse) error {
	if s.err != nil {
		return s.err
	}
	records, err := s.store.Records(req.Host, req.Kind, req.From, req.Limit)
	res.Records = records
	return err
}

func (s *service) Ingest(req *api.IngestRequest, res *api.IngestResponse) error {
	if s.err != nil {
		return s.err
	}
	return s.store.Ingest(req.Records)
}

func (s *service) SearchCmds(req *api.SearchCmdsRequest, res *api.SearchCmdsResponse) error {
	if s.err != nil {
		return s.err
	}
	hits, err := s.store.SearchCmds(req.Query)
	res.Hits = hits
	return err
}

func (s *service) AddDir(req *api.AddDirRequest, res *api.AddDirResponse) error {
	if s.err != nil {
		return s.err
	}
	return s.store.AddDir(req.Dir, req.IncFactor)
}

func (s *service) DelDir(req *api.DelDirRequest, res *api.DelDirResponse) error {
	if s.err != nil {
		return s.err
	}
	return s.store.DelDir(req.Dir)
}

func (s *service) Dirs(req *api.DirsRequest, res *api.DirsResponse) error {
	if s.err != nil {
		return s.err
	}
	dirs, err := s.store.Dirs(req.Blacklist)
	res.Dirs = dirs
	return err
}
