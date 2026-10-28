package storedefs

import (
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Scoring of fuzzy matches, modeled after fzf's. Every matched character
// scores fuzzyScoreMatch, plus a bonus for where it sits: at the start of a
// word, in a run of consecutive matches, or at a lowercase-to-uppercase
// transition. Characters skipped between matches cost a gap penalty. The
// effect is that a contiguous match always outscores a scattered one, and a
// match at word boundaries ("gc" for "git commit") outscores one in the middle
// of words.
const (
	fuzzyScoreMatch = 16
	// After the start of the text or whitespace.
	fuzzyBonusBoundary = 8
	// After a character that is neither a letter nor a digit, like "-".
	fuzzyBonusNonWord = 6
	// An uppercase letter after a lowercase one.
	fuzzyBonusCamel = 7
	// Right after another matched character. Like in fzf, a run of
	// consecutive matches also carries the bonus of its first character, so
	// that a whole word matched from its start beats the same characters
	// scattered over the starts of several words.
	fuzzyBonusConsecutive = 4
	// The bonus of the first matched character is multiplied by this.
	fuzzyBonusFirstMul       = 2
	fuzzyPenaltyGapStart     = -3
	fuzzyPenaltyGapExtension = -1
)

// Matches a needle as a subsequence of a text and scores the match.
type fuzzyMatcher struct {
	needle []rune
	fold   bool
}

func newFuzzyMatcher(needle string, fold bool) fuzzyMatcher {
	if fold {
		needle = strings.ToLower(needle)
	}
	return fuzzyMatcher{[]rune(needle), fold}
}

func (m fuzzyMatcher) eq(r rune, n rune) bool {
	if m.fold {
		r = unicode.ToLower(r)
	}
	return r == n
}

// Match reports whether the needle occurs in s as a subsequence, and the
// score of the best such occurrence it finds. An empty needle matches
// everything with a score of 0.
//
// Like fzf's first algorithm, it scans forward to find where the earliest
// match ends, then backward from there to find the latest start that still
// fits the whole needle; the match within that window is greedy. This finds
// the tightest window around the first occurrence, which is the best match
// in the common case and never worse than the greedy forward match.
func (m fuzzyMatcher) Match(s string) (int, bool) {
	if len(m.needle) == 0 {
		return 0, true
	}
	// Forward scan: the end of the earliest match.
	end := -1
	j := 0
	for i, r := range s {
		if m.eq(r, m.needle[j]) {
			j++
			if j == len(m.needle) {
				end = i + utf8.RuneLen(r)
				break
			}
		}
	}
	if end < 0 {
		return 0, false
	}
	// Backward scan: the latest start that fits the needle before end.
	start := 0
	j = len(m.needle) - 1
	for k := end; k > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:k])
		k -= size
		if m.eq(r, m.needle[j]) {
			j--
			if j < 0 {
				start = k
				break
			}
		}
	}
	var prev rune
	if start > 0 {
		prev, _ = utf8.DecodeLastRuneInString(s[:start])
	}
	return m.score(s[start:end], prev), true
}

// Scores the greedy match of the needle in a window of the text. prev is the
// character before the window, or 0 if the window starts the text.
func (m fuzzyMatcher) score(window string, prev rune) int {
	score := 0
	j := 0
	prevMatched := false
	// Bonus of the first character of the current run of matches.
	runBonus := 0
	gap := 0
	for _, r := range window {
		if j < len(m.needle) && m.eq(r, m.needle[j]) {
			bonus := charBonus(prev, r)
			switch {
			case j == 0:
				runBonus = bonus
				bonus *= fuzzyBonusFirstMul
			case prevMatched:
				bonus = max(bonus, runBonus, fuzzyBonusConsecutive)
			default:
				runBonus = bonus
				score += fuzzyPenaltyGapStart + fuzzyPenaltyGapExtension*(gap-1)
			}
			score += fuzzyScoreMatch + bonus
			j++
			prevMatched = true
			gap = 0
		} else {
			prevMatched = false
			gap++
		}
		prev = r
	}
	return score
}

// The bonus for matching r when it follows prev, which is 0 at the start of
// the text.
func charBonus(prev, r rune) int {
	switch {
	case prev == 0 || unicode.IsSpace(prev):
		return fuzzyBonusBoundary
	case unicode.IsLower(prev) && unicode.IsUpper(r):
		return fuzzyBonusCamel
	case !unicode.IsLetter(prev) && !unicode.IsDigit(prev):
		return fuzzyBonusNonWord
	}
	return 0
}

// Frecency scores how recently and how often a command was run, for ordering
// fuzzy matches of equal quality. It follows atuin: a recency score that
// steps down with the age of the last run, from 100 within the hour to 5
// after a month, plus a frequency score that grows with the logarithm of the
// count, capped at 100. A zero last-run time counts as very old.
func Frecency(count int, lastRun, now time.Time) int {
	if count <= 0 {
		return 0
	}
	recency := 5
	if !lastRun.IsZero() {
		switch hours := now.Sub(lastRun).Hours(); {
		case hours < 1:
			recency = 100
		case hours < 7:
			recency = 90
		case hours < 25:
			recency = 70
		case hours < 73:
			recency = 50
		case hours < 169:
			recency = 30
		case hours < 721:
			recency = 15
		}
	}
	frequency := min(int(math.Log(float64(count))*20), 100)
	return recency + frequency
}

// FuzzyRanker collects the entries that match a query's secondary fuzzy
// query, and ranks them. Entries are added newest first; the first occurrence
// of a text becomes the hit, and later occurrences only count toward its
// frecency. It is created by CmdMatcher.Ranker.
type FuzzyRanker struct {
	fuzzy func(string) (int, bool)
	now   time.Time
	cands []fuzzyCand
	// Text to index in cands, or -1 for a text known not to match.
	idx map[string]int
}

type fuzzyCand struct {
	hit   CmdHit
	score int
	count int
}

// Add considers an entry that passed the query's other filters but not its
// primary text query. If the entry becomes a new candidate, it returns a
// pointer to the hit for the caller to fill in the metadata; the pointer is
// only valid until the next call. Otherwise it returns nil.
func (r *FuzzyRanker) Add(seq int, text string) *CmdHit {
	if i, ok := r.idx[text]; ok {
		if i >= 0 {
			r.cands[i].count++
		}
		return nil
	}
	score, ok := r.fuzzy(text)
	if !ok {
		r.idx[text] = -1
		return nil
	}
	text = strings.Clone(text)
	r.idx[text] = len(r.cands)
	r.cands = append(r.cands, fuzzyCand{
		hit: CmdHit{Cmd: Cmd{Text: text, Seq: seq}, Fuzzy: true}, score: score, count: 1})
	return &r.cands[len(r.cands)-1].hit
}

// Hits returns up to limit candidates, best first: by match score, then by
// frecency, then newest first. A limit of 0 means no limit.
func (r *FuzzyRanker) Hits(limit int) []CmdHit {
	type ranked struct {
		hit             CmdHit
		score, frecency int
	}
	rs := make([]ranked, len(r.cands))
	for i, c := range r.cands {
		rs[i] = ranked{c.hit, c.score, Frecency(c.count, c.hit.Meta.Time, r.now)}
	}
	slices.SortFunc(rs, func(a, b ranked) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if a.frecency != b.frecency {
			return b.frecency - a.frecency
		}
		return b.hit.Cmd.Seq - a.hit.Cmd.Seq
	})
	if limit > 0 && len(rs) > limit {
		rs = rs[:limit]
	}
	hits := make([]CmdHit, len(rs))
	for i, r := range rs {
		hits[i] = r.hit
	}
	return hits
}
