package storedefs

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// TextQuery is a query against the text of command history entries. It is a
// tree so that the editor's filter language can be evaluated where the
// entries are, in the daemon, instead of shipping the entries to the editor.
// The zero TextQuery matches everything.
type TextQuery struct {
	// One of the Q* constants.
	Op string
	// The pattern, for QSubstring, QRegexp, QPrefix and QLinePrefix.
	Arg string
	// Whether to ignore case, for QSubstring, QPrefix and QLinePrefix.
	Fold bool
	// Sub-queries, for QAnd and QOr.
	Subs []TextQuery
}

// Text query operations.
const (
	// All sub-queries match. With no sub-queries, matches everything.
	QAnd = "and"
	// Any sub-query matches.
	QOr = "or"
	// The text contains Arg.
	QSubstring = "substring"
	// The text matches the regular expression Arg.
	QRegexp = "regexp"
	// The text starts with Arg.
	QPrefix = "prefix"
	// Some line of the text, after leading spaces and tabs, starts with Arg.
	QLinePrefix = "line-prefix"
)

// Compile returns a function that matches the query against the text of an
// entry. It fails if the query contains an invalid regular expression or an
// unknown operation.
func (q TextQuery) Compile() (func(string) bool, error) {
	switch q.Op {
	case "", QAnd:
		subs, err := compileSubs(q.Subs)
		if err != nil {
			return nil, err
		}
		return func(s string) bool {
			for _, sub := range subs {
				if !sub(s) {
					return false
				}
			}
			return true
		}, nil
	case QOr:
		subs, err := compileSubs(q.Subs)
		if err != nil {
			return nil, err
		}
		return func(s string) bool {
			for _, sub := range subs {
				if sub(s) {
					return true
				}
			}
			return false
		}, nil
	case QSubstring:
		arg := q.Arg
		if q.Fold {
			arg = strings.ToLower(arg)
			return func(s string) bool { return strings.Contains(strings.ToLower(s), arg) }, nil
		}
		return func(s string) bool { return strings.Contains(s, arg) }, nil
	case QRegexp:
		re, err := regexp.Compile(q.Arg)
		if err != nil {
			return nil, err
		}
		return re.MatchString, nil
	case QPrefix:
		arg, fold := q.Arg, q.Fold
		return func(s string) bool { return hasPrefix(s, arg, fold) }, nil
	case QLinePrefix:
		arg, fold := q.Arg, q.Fold
		return func(s string) bool { return MatchingLine(s, arg, fold) != "" }, nil
	default:
		return nil, fmt.Errorf("unknown text query operation %q", q.Op)
	}
}

func compileSubs(subs []TextQuery) ([]func(string) bool, error) {
	fs := make([]func(string) bool, len(subs))
	for i, sub := range subs {
		f, err := sub.Compile()
		if err != nil {
			return nil, err
		}
		fs[i] = f
	}
	return fs, nil
}

// MatchingLine returns the first line of s that, after leading spaces and
// tabs, starts with prefix and is longer than it, or "" if there is none. The
// returned line has the leading spaces and tabs removed.
func MatchingLine(s, prefix string, fold bool) string {
	for s != "" {
		line := s
		if i := strings.IndexByte(s, '\n'); i != -1 {
			line, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		line = strings.TrimLeft(line, " \t")
		if len(line) > len(prefix) && hasPrefix(line, prefix, fold) {
			return line
		}
	}
	return ""
}

// Reports whether s starts with prefix, optionally ignoring case. When
// ignoring case, the prefix must end on a rune boundary of s.
func hasPrefix(s, prefix string, fold bool) bool {
	if !fold {
		return strings.HasPrefix(s, prefix)
	}
	if len(s) < len(prefix) {
		return false
	}
	if len(s) > len(prefix) && !utf8.RuneStart(s[len(prefix)]) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// CmdQuery is a query for command history entries. Results are the newest
// entries that match, in newest to oldest order.
type CmdQuery struct {
	// Query against the text of entries.
	Text TextQuery

	// Metadata filters. Empty values mean no filter. Entries without
	// metadata only pass filters that are not set.
	Dir, Host, Session string
	// ExitOK or ExitFail, or empty for no filter.
	Exit string
	// Only entries started at or after Since, and before Until, when the
	// respective bound is non-zero.
	Since, Until time.Time

	// Only entries with sequence numbers below Upto are considered, except
	// those listed in Own. This lets a shell keep the view of the history it
	// started with, plus its own commands. Upto of 0 means no bound.
	Upto int
	Own  []int
	// Only entries with sequence numbers below Before are considered, for
	// paging through results. 0 means no bound.
	Before int
	// Whether to skip entries whose text already appeared in a newer result.
	Dedup bool
	// Maximum number of results. 0 means no limit.
	Limit int
}

// Values of CmdQuery.Exit.
const (
	ExitOK   = "ok"
	ExitFail = "fail"
)

// CmdHit is a result of a CmdQuery.
type CmdHit struct {
	Cmd Cmd
	// Zero if the entry has no metadata. The ID and Idx fields are always
	// zero: they are not needed for display or filtering, and not keeping
	// them in the index saves tens of bytes per entry. Use Store.CmdMeta to
	// get them for a specific entry.
	Meta CmdMeta
}

// CmdMatcher matches individual entries against a CmdQuery, except for the
// Before, Dedup and Limit fields, which concern the result list.
type CmdMatcher struct {
	q    CmdQuery
	text func(string) bool
	own  map[int]struct{}
}

// Matcher compiles the query into a CmdMatcher.
func (q CmdQuery) Matcher() (*CmdMatcher, error) {
	text, err := q.Text.Compile()
	if err != nil {
		return nil, err
	}
	m := &CmdMatcher{q: q, text: text}
	if len(q.Own) > 0 {
		m.own = make(map[int]struct{}, len(q.Own))
		for _, seq := range q.Own {
			m.own[seq] = struct{}{}
		}
	}
	return m, nil
}

// Match reports whether an entry matches. The metadata is nil for entries
// that have none.
func (m *CmdMatcher) Match(seq int, text string, meta *CmdMeta) bool {
	return m.MatchSeq(seq) && m.MatchMeta(meta) && m.text(text)
}

// MatchSeq reports whether an entry's sequence number is within the query's
// view.
func (m *CmdMatcher) MatchSeq(seq int) bool {
	if m.q.Upto > 0 && seq >= m.q.Upto {
		_, own := m.own[seq]
		return own
	}
	return true
}

// MatchMeta reports whether an entry's metadata passes the query's metadata
// filters. The metadata is nil for entries that have none.
func (m *CmdMatcher) MatchMeta(meta *CmdMeta) bool {
	q := &m.q
	if q.Dir == "" && q.Host == "" && q.Session == "" && q.Exit == "" &&
		q.Since.IsZero() && q.Until.IsZero() {
		return true
	}
	if meta == nil {
		return false
	}
	if q.Dir != "" && meta.Dir != q.Dir {
		return false
	}
	if q.Host != "" && meta.Host != q.Host {
		return false
	}
	if q.Session != "" && meta.Session != q.Session {
		return false
	}
	switch q.Exit {
	case ExitOK:
		if meta.Exit != 0 {
			return false
		}
	case ExitFail:
		if meta.Exit == 0 {
			return false
		}
	}
	if !q.Since.IsZero() && meta.Time.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && !meta.Time.Before(q.Until) {
		return false
	}
	return true
}

// MatchText reports whether an entry's text matches the query.
func (m *CmdMatcher) MatchText(text string) bool { return m.text(text) }

// SearchSlice runs a query over entries held in memory, oldest first, with
// metadata keyed by sequence number. It is the reference implementation of
// the query semantics, used by stores without an index and by tests.
func SearchSlice(cmds []Cmd, metas map[int]CmdMeta, q CmdQuery) ([]CmdHit, error) {
	m, err := q.Matcher()
	if err != nil {
		return nil, err
	}
	var hits []CmdHit
	var seen map[string]struct{}
	if q.Dedup {
		seen = map[string]struct{}{}
	}
	for i := len(cmds) - 1; i >= 0; i-- {
		cmd := cmds[i]
		if q.Before > 0 && cmd.Seq >= q.Before {
			continue
		}
		var meta *CmdMeta
		if mt, ok := metas[cmd.Seq]; ok {
			meta = &mt
		}
		if !m.Match(cmd.Seq, cmd.Text, meta) {
			continue
		}
		if seen != nil {
			if _, dup := seen[cmd.Text]; dup {
				continue
			}
			seen[cmd.Text] = struct{}{}
		}
		hit := CmdHit{Cmd: cmd}
		if meta != nil {
			hit.Meta = *meta
			hit.Meta.ID, hit.Meta.Idx = "", 0
		}
		hits = append(hits, hit)
		if q.Limit > 0 && len(hits) >= q.Limit {
			break
		}
	}
	return hits, nil
}
