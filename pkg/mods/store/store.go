package store

import (
	"fmt"
	"time"

	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/eval/vals"
	"src.elv.sh/pkg/store/storedefs"
)

func Ns(s storedefs.Store) *eval.Ns {
	return eval.BuildNsNamed("store").
		AddGoFns(map[string]any{
			"next-cmd-seq": s.NextCmdSeq,
			"add-cmd":      s.AddCmd,
			"del-cmd":      s.DelCmd,
			"cmd":          s.Cmd,
			"cmds":         s.CmdsWithSeq,
			"next-cmd":     s.NextCmd,
			"prev-cmd":     s.PrevCmd,

			"host-id": s.HostID,
			"set-cmd-meta": func(m vals.Map) error {
				meta, err := scanCmdMeta(m)
				if err != nil {
					return err
				}
				return s.SetCmdMeta(meta)
			},
			"cmd-meta": func(seq int) (vals.Map, error) {
				meta, err := s.CmdMeta(seq)
				return cmdMetaToMap(meta), err
			},
			"cmds-meta": func(from, upto int) ([]vals.Map, error) {
				metas, err := s.CmdsMeta(from, upto)
				return cmdMetasToMaps(metas), err
			},
			"deleted-cmds": func(from, upto int) ([]vals.Map, error) {
				metas, err := s.DeletedCmds(from, upto)
				return cmdMetasToMaps(metas), err
			},
			"commit-cmd-meta": func(m vals.Map) (vals.Map, error) {
				meta, err := scanCmdMeta(m)
				if err != nil {
					return nil, err
				}
				meta, err = s.CommitCmdMeta(meta)
				return cmdMetaToMap(meta), err
			},

			"status": func() (vals.Map, error) {
				status, err := s.Status()
				return statusToMap(status), err
			},
			"records": func(host, kind string, from, limit int) ([]vals.Map, error) {
				records, err := s.Records(host, kind, uint64(from), limit)
				maps := make([]vals.Map, len(records))
				for i, r := range records {
					maps[i] = recordToMap(r)
				}
				return maps, err
			},
			"ingest": func(list vals.List) error {
				var records []storedefs.Record
				var scanErr error
				vals.Iterate(list, func(v any) bool {
					m, ok := v.(vals.Map)
					if !ok {
						scanErr = fmt.Errorf("bad record %s: must be map", vals.ReprPlain(v))
						return false
					}
					r, err := scanRecord(m)
					if err != nil {
						scanErr = err
						return false
					}
					records = append(records, r)
					return true
				})
				if scanErr != nil {
					return scanErr
				}
				return s.Ingest(records)
			},
			"reset-series": s.ResetSeries,

			"search-cmds": func(m vals.Map) ([]vals.Map, error) {
				q, err := scanCmdQuery(m)
				if err != nil {
					return nil, err
				}
				hits, err := s.SearchCmds(q)
				maps := make([]vals.Map, len(hits))
				for i, h := range hits {
					maps[i] = cmdHitToMap(h)
				}
				return maps, err
			},

			"add-dir": func(dir string) error { return s.AddDir(dir, 1) },
			"del-dir": s.DelDir,
			"dirs":    func() ([]storedefs.Dir, error) { return s.Dirs(storedefs.NoBlacklist) },
		}).Ns()
}

func cmdMetaToMap(m storedefs.CmdMeta) vals.Map {
	return vals.MakeMap(
		"seq", m.Seq,
		"id", m.ID,
		"time", formatTime(m.Time),
		"duration", m.Duration,
		"exit", m.Exit,
		"host", m.Host,
		"dir", m.Dir,
		"session", m.Session,
		"idx", int(m.Idx),
	)
}

func recordToMap(r storedefs.Record) vals.Map {
	return vals.MakeMap(
		"v", r.Version,
		"kind", r.Kind,
		"host", r.Host,
		"idx", int(r.Idx),
		"op", r.Op,
		"id", r.ID,
		"time", formatTime(r.Time),
		"text", r.Text,
		"duration", r.Duration,
		"exit", r.Exit,
		"dir", r.Dir,
		"session", r.Session,
		"target", r.Target,
	)
}

func statusToMap(status storedefs.Status) vals.Map {
	m := vals.EmptyMap
	for host, kinds := range status {
		km := vals.EmptyMap
		for kind, idx := range kinds {
			km = km.Assoc(kind, int(idx))
		}
		m = m.Assoc(host, km)
	}
	return m
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func cmdMetasToMaps(metas []storedefs.CmdMeta) []vals.Map {
	maps := make([]vals.Map, len(metas))
	for i, m := range metas {
		maps[i] = cmdMetaToMap(m)
	}
	return maps
}

func scanCmdMeta(m vals.Map) (storedefs.CmdMeta, error) {
	var meta storedefs.CmdMeta
	var timeStr string
	var idx int
	fields := map[string]any{
		"seq":      &meta.Seq,
		"id":       &meta.ID,
		"time":     &timeStr,
		"duration": &meta.Duration,
		"exit":     &meta.Exit,
		"host":     &meta.Host,
		"dir":      &meta.Dir,
		"session":  &meta.Session,
		"idx":      &idx,
	}
	if err := scanFields(m, fields); err != nil {
		return meta, err
	}
	if idx < 0 {
		return meta, fmt.Errorf("bad value for idx: must be non-negative")
	}
	meta.Idx = uint64(idx)
	t, err := parseTime(timeStr)
	meta.Time = t
	return meta, err
}

func scanRecord(m vals.Map) (storedefs.Record, error) {
	var r storedefs.Record
	var timeStr string
	var idx int
	fields := map[string]any{
		"v":        &r.Version,
		"kind":     &r.Kind,
		"host":     &r.Host,
		"idx":      &idx,
		"op":       &r.Op,
		"id":       &r.ID,
		"time":     &timeStr,
		"text":     &r.Text,
		"duration": &r.Duration,
		"exit":     &r.Exit,
		"dir":      &r.Dir,
		"session":  &r.Session,
		"target":   &r.Target,
	}
	if err := scanFields(m, fields); err != nil {
		return r, err
	}
	if idx < 0 {
		return r, fmt.Errorf("bad value for idx: must be non-negative")
	}
	r.Idx = uint64(idx)
	t, err := parseTime(timeStr)
	r.Time = t
	return r, err
}

func cmdHitToMap(h storedefs.CmdHit) vals.Map {
	return vals.MakeMap(
		"seq", h.Cmd.Seq,
		"text", h.Cmd.Text,
		"time", formatTime(h.Meta.Time),
		"duration", h.Meta.Duration,
		"exit", h.Meta.Exit,
		"host", h.Meta.Host,
		"dir", h.Meta.Dir,
		"session", h.Meta.Session,
	)
}

func scanCmdQuery(m vals.Map) (storedefs.CmdQuery, error) {
	var q storedefs.CmdQuery
	var substring, prefix, regexp, sinceStr, untilStr string
	var fold bool
	fields := map[string]any{
		"substring": &substring,
		"prefix":    &prefix,
		"regexp":    &regexp,
		"fold":      &fold,
		"dir":       &q.Dir,
		"host":      &q.Host,
		"session":   &q.Session,
		"exit":      &q.Exit,
		"since":     &sinceStr,
		"until":     &untilStr,
		"before":    &q.Before,
		"limit":     &q.Limit,
		"dedup":     &q.Dedup,
	}
	if err := scanFields(m, fields); err != nil {
		return q, err
	}
	var texts []storedefs.TextQuery
	if substring != "" {
		texts = append(texts, storedefs.TextQuery{Op: storedefs.QSubstring, Arg: substring, Fold: fold})
	}
	if prefix != "" {
		texts = append(texts, storedefs.TextQuery{Op: storedefs.QPrefix, Arg: prefix, Fold: fold})
	}
	if regexp != "" {
		texts = append(texts, storedefs.TextQuery{Op: storedefs.QRegexp, Arg: regexp})
	}
	q.Text = storedefs.TextQuery{Op: storedefs.QAnd, Subs: texts}
	if q.Exit != "" && q.Exit != storedefs.ExitOK && q.Exit != storedefs.ExitFail {
		return q, fmt.Errorf("bad value for exit: must be ok or fail")
	}
	var err error
	if q.Since, err = parseTime(sinceStr); err != nil {
		return q, fmt.Errorf("bad value for since: %v", err)
	}
	if q.Until, err = parseTime(untilStr); err != nil {
		return q, fmt.Errorf("bad value for until: %v", err)
	}
	return q, nil
}

// Scans the entries of a map into the Go variables pointed to by fields.
// Every key of the map must be present in fields; missing keys leave their
// variables untouched.
func scanFields(m vals.Map, fields map[string]any) error {
	for it := m.Iterator(); it.HasElem(); it.Next() {
		k, v := it.Elem()
		key, ok := k.(string)
		if !ok {
			return fmt.Errorf("bad key %s: must be string", vals.ReprPlain(k))
		}
		ptr, ok := fields[key]
		if !ok {
			return fmt.Errorf("unknown key %s", vals.ReprPlain(key))
		}
		if err := vals.ScanToGo(v, ptr); err != nil {
			return fmt.Errorf("bad value for %s: %v", key, err)
		}
	}
	return nil
}

// Parses an RFC 3339 time; an empty string is the zero time.
func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return t, fmt.Errorf("bad value for time: %v", err)
	}
	return t, nil
}
