package edit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"src.elv.sh/pkg/cli"
	"src.elv.sh/pkg/cli/tk"
	"src.elv.sh/pkg/edit/complete"
	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/parse"
	"src.elv.sh/pkg/parse/np"
	"src.elv.sh/pkg/store/storedefs"
)

func initAutosuggestion(appSpec *cli.AppSpec, ed *Editor, ev *eval.Evaler, hs *histStore, nb eval.NsBuilder) {
	enabled := newBoolVar(true)
	sg := newSuggester(hs, ev, func() bool { return enabled.GetRaw().(bool) })
	ed.suggester = sg
	appSpec.Suggester = sg

	// The closures below dereference ed.app lazily, since ed.app is not set
	// yet when this function is called.
	nb.AddNs("autosuggestion",
		eval.BuildNsNamed("edit:autosuggestion").
			AddVar("enabled", enabled).
			AddGoFns(map[string]any{
				"accept":      func() { acceptSuggestion(ed.app, acceptWhole) },
				"accept-word": func() { acceptSuggestion(ed.app, acceptWord) },
			}))
}

// Accepts the suggestion shown in the focused code area, up to the position
// returned by upTo (see tk.CodeArea.AcceptSuggestion).
func acceptSuggestion(app cli.App, upTo pureMover) bool {
	codeArea, ok := focusedCodeArea(app)
	if !ok {
		return false
	}
	return codeArea.AcceptSuggestion(upTo)
}

// A pureMover that moves to the end of the buffer, used for accepting the
// whole suggestion.
func acceptWhole(full string, _ int) int { return len(full) }

// A pureMover that moves to the end of the current line, used for accepting
// the suggestion up to the end of its first line.
func acceptLine(full string, dot int) int {
	if i := strings.IndexByte(full[dot:], '\n'); i != -1 {
		return dot + i
	}
	return len(full)
}

// Returns a pureMover that moves to the end of the next word (skipping any
// whitespace first), used for accepting one word of the suggestion. This is
// unlike the move-dot-right-*word movers, which move to the *start* of the
// next word: accepting the suggestion up to there would only accept the
// whitespace before the word.
func acceptGeneralWord(categorize categorizer) pureMover {
	return func(full string, dot int) int {
		pos := skipWsRight(categorize, full, dot)
		return skipSameCatRight(categorize, full, pos)
	}
}

var (
	acceptWord      = acceptGeneralWord(categorizeWord)
	acceptSmallWord = acceptGeneralWord(tk.CategorizeSmallWord)
	acceptAlnumWord = acceptGeneralWord(categorizeAlnum)
)

const suggesterLatesBufferSize = 128

// A cli.Suggester modelled after the autosuggestion feature of the Fish
// shell. For the current code, it suggests, in order of preference:
//
//  1. The most recent command in the history that starts with the code
//     (compared case-sensitively), or, if the code has multiple lines and the
//     last line starts a new command, the most recent command containing a
//     line that starts with the last line.
//
//  2. The first completion candidate for the code, if any.
//
//  3. The same as 1, but compared case-insensitively.
//
// History entries are only suggested if they still look runnable: the
// commands must exist, the target of "cd" must be a directory other than the
// working directory, and arguments that look like paths must exist.
//
// The lookup is done asynchronously; the result is delivered via LateUpdates.
// The suggester caches the last result: as long as the user keeps typing
// characters that match the suggestion, no new lookup is needed. Likewise, if
// the history had no match for a prefix, it has no match for any longer
// prefix (although completions may still have).
type suggester struct {
	hs      *histStore
	ev      *eval.Evaler
	enabled func() bool
	lates   chan struct{}

	mu sync.Mutex
	// Returns the completion config, if completion is available.
	completeCfg func() complete.Config
	// Generation of the history store when the cache was populated.
	gen uint64
	// Whether the cache is populated.
	valid bool
	// The code the cache is for.
	code string
	// The suggested full code for code. Empty if there is none, or if the
	// lookup has not finished yet.
	full string
	// Whether full matches code case-sensitively.
	exact bool
	// Whether the lookup for code has finished.
	done bool
	// Whether the history had no match for code (only valid if done).
	histNone bool
	// The last non-empty suggestion that was replaced, so that it can be
	// restored when the user deletes back to the code it was for.
	savedCode, savedFull string
	savedExact           bool
	// The snapshot of the history store used by the last lookup.
	snapshot *histSnapshot
}

func newSuggester(hs *histStore, ev *eval.Evaler, enabled func() bool) *suggester {
	return &suggester{
		hs: hs, ev: ev, enabled: enabled,
		lates: make(chan struct{}, suggesterLatesBufferSize)}
}

func (s *suggester) setCompleteCfg(cfg func() complete.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeCfg = cfg
}

func (s *suggester) LateUpdates() <-chan struct{} { return s.lates }

func (s *suggester) Get(code string) string {
	if code == "" || !s.enabled() {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if gen := s.hs.Generation(); gen != s.gen {
		// The history store has changed; invalidate the cache.
		s.gen = gen
		s.valid = false
	}

	skipHistory := false
	if s.valid {
		if code == s.code {
			return s.full
		}
		if strings.HasPrefix(s.code, code) {
			// The user has deleted characters. Like in Fish, this doesn't
			// trigger a new lookup, so that suggestions don't pop up while
			// deleting. However, a suggestion that was replaced by typing is
			// restored when the user deletes back to the code it was for.
			if code == s.savedCode {
				s.code, s.full, s.exact = s.savedCode, s.savedFull, s.savedExact
				s.done, s.histNone = true, false
				return s.full
			}
			s.code, s.full, s.done, s.histNone = code, "", true, false
			return ""
		}
		if s.done && strings.HasPrefix(code, s.code) {
			// The user has typed more characters.
			if s.full != "" && len(s.full) > len(code) &&
				(s.exact && strings.HasPrefix(s.full, code) ||
					!s.exact && foldHasPrefix(s.full, code)) {
				// The characters match the suggestion, so it remains the best
				// suggestion. (If the old suggestion was an exact match but
				// the new code only matches case-insensitively, a new lookup
				// is needed, since there may be an exact match now.)
				s.code = code
				return s.full
			}
			// Either the user has typed something not matching the
			// suggestion, or has typed all of the suggestion. In the latter
			// case there may still be an older, longer command to suggest,
			// so a new lookup is needed in both cases.
			//
			// However, if the history had no match for the old code, it has
			// no match for the new code either. This only holds if the new
			// code doesn't add a line, since the last line is also matched
			// separately.
			skipHistory = s.histNone && !strings.Contains(code[len(s.code):], "\n")
		}
	}

	if s.valid && s.full != "" {
		s.savedCode, s.savedFull, s.savedExact = s.code, s.full, s.exact
	}

	if skipHistory && s.completeCfg == nil {
		// Nothing can be suggested; no need for a lookup.
		s.code, s.full, s.exact, s.done, s.histNone, s.valid = code, "", false, true, true, true
		return ""
	}

	s.code, s.full, s.exact, s.done, s.histNone, s.valid = code, "", false, false, false, true
	gen := s.gen
	go s.lookup(code, gen, skipHistory)
	return ""
}

// Whether the lookup for the given code and generation is stale.
func (s *suggester) stale(code string, gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.valid || s.code != code || s.gen != gen
}

func (s *suggester) lookup(code string, gen uint64, skipHistory bool) {
	full, exact, histNone := "", false, true
	if !skipHistory {
		full, exact = s.fromHistory(code, gen)
		histNone = full == ""
	}
	if !exact {
		if s.stale(code, gen) {
			return
		}
		// An exact completion is preferred over an inexact history match.
		if comp := s.fromCompletion(code); comp != "" {
			full, exact = comp, true
		}
	}

	s.mu.Lock()
	if !s.valid || s.code != code || s.gen != gen {
		// The code or the history store has changed since the lookup was
		// started; discard the result.
		s.mu.Unlock()
		return
	}
	s.full, s.exact, s.done, s.histNone = full, exact, true, histNone
	// The channel send below might block, so unlock first.
	s.mu.Unlock()
	s.lates <- struct{}{}
}

// Finds the most recent runnable command in the history that starts with the
// code, preferring exact matches to case-insensitive ones. Returns the
// suggested full code and whether it is an exact match, or "" if there is
// none.
func (s *suggester) fromHistory(code string, gen uint64) (string, bool) {
	cmds := s.getSnapshot()

	// Pass 1: whole-buffer prefix.
	reserve := ""
	for i := len(cmds) - 1; i >= 0; i-- {
		text := cmds[i].Text
		if len(text) <= len(code) {
			continue
		}
		if strings.HasPrefix(text, code) {
			if s.runnable(text) {
				return text, true
			}
		} else if reserve == "" && foldHasPrefix(text, code) {
			if s.runnable(text) {
				reserve = text
			}
		}
		if i%256 == 0 && s.stale(code, gen) {
			return "", false
		}
	}

	// Pass 2: last-line prefix, for multi-line code whose last line starts a
	// new command. The suggestion is only that line.
	line, ok := lastLineStartingCommand(code)
	if ok {
		lineTrim := strings.TrimLeft(line, " \t")
		if lineTrim != "" {
			prefix := code[:len(code)-len(lineTrim)]
			for i := len(cmds) - 1; i >= 0; i-- {
				for _, l := range strings.Split(cmds[i].Text, "\n") {
					l = strings.TrimLeft(l, " \t")
					if len(l) <= len(lineTrim) {
						continue
					}
					if strings.HasPrefix(l, lineTrim) {
						if s.runnable(l) {
							return prefix + l, true
						}
					} else if reserve == "" && foldHasPrefix(l, lineTrim) {
						if s.runnable(l) {
							reserve = prefix + l
						}
					}
				}
				if i%256 == 0 && s.stale(code, gen) {
					return "", false
				}
			}
		}
	}

	return reserve, false
}

func (s *suggester) getSnapshot() []storedefs.Cmd {
	s.mu.Lock()
	old := s.snapshot
	s.mu.Unlock()

	snapshot, err := s.hs.Snapshot(old)
	if err != nil || snapshot == nil {
		return nil
	}
	if snapshot != old {
		s.mu.Lock()
		s.snapshot = snapshot
		s.mu.Unlock()
	}
	return snapshot.cmds
}

// Finds the first completion candidate for the code, and returns the full
// code with the candidate inserted, or "" if there is none.
func (s *suggester) fromCompletion(code string) string {
	s.mu.Lock()
	cfgFn := s.completeCfg
	s.mu.Unlock()
	if cfgFn == nil {
		return ""
	}
	// Don't complete when the code ends with whitespace (the current token is
	// empty, so completions would be shown after every space), or a quote.
	if strings.HasSuffix(code, " ") || strings.HasSuffix(code, "\t") ||
		strings.HasSuffix(code, "\n") || strings.HasSuffix(code, "'") ||
		strings.HasSuffix(code, `"`) {
		return ""
	}
	result, err := complete.Complete(
		complete.CodeBuffer{Content: code, Dot: len(code)}, s.ev, cfgFn())
	if err != nil || result == nil || len(result.Items) == 0 {
		return ""
	}
	from, to := result.Replace.From, result.Replace.To
	if from < 0 || from > to || to != len(code) {
		return ""
	}
	seed := code[from:to]
	if seed == "" {
		return ""
	}
	// Like Tab completion, this includes any trailing space.
	item := result.Items[0].ToInsert
	if len(item) <= len(seed) || !strings.HasPrefix(item, seed) {
		return ""
	}
	return code[:from] + item
}

// Whether a history entry still looks runnable: all commands exist, the
// target of "cd" is a directory other than the working directory, and
// arguments that look like paths exist.
func (s *suggester) runnable(code string) bool {
	tree, _ := parse.Parse(parse.Source{Name: "[autosuggestion]", Code: code}, parse.Config{})
	for _, pipeline := range tree.Root.Pipelines {
		for _, form := range pipeline.Forms {
			if !s.formRunnable(form) {
				return false
			}
		}
	}
	return true
}

func (s *suggester) formRunnable(form *parse.Form) bool {
	if form.Head == nil {
		return true
	}
	head, ok := s.ev.PurelyEvalCompound(form.Head)
	if !ok {
		// Can't tell; assume it's fine.
		return true
	}
	if !hasCommand(s.ev, head) {
		return false
	}
	if head == "cd" && len(form.Args) == 1 {
		dir, ok := s.ev.PurelyEvalCompound(form.Args[0])
		if !ok {
			return true
		}
		return isDirOtherThanCwd(dir)
	}
	for _, arg := range form.Args {
		src := parse.SourceText(arg)
		if !looksLikePath(src) {
			continue
		}
		path, ok := s.ev.PurelyEvalCompound(arg)
		if !ok {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			return false
		}
	}
	return true
}

// Whether an argument (as source text) looks like a path that should still
// exist for the command to make sense. This is a heuristic: arguments
// containing a slash, or starting with "~" or ".".
func looksLikePath(src string) bool {
	if src == "" || src[0] == '-' {
		return false
	}
	return strings.Contains(src, "/") || src[0] == '~' || src[0] == '.'
}

func isDirOtherThanCwd(dir string) bool {
	stat, err := os.Stat(dir)
	if err != nil || !stat.IsDir() {
		return false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return true
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return true
	}
	return filepath.Clean(abs) != filepath.Clean(cwd)
}

// If code has multiple lines and its last line starts a new command (rather
// than continuing one from the previous line, or being part of a list or
// another compound expression), returns the last line and true.
func lastLineStartingCommand(code string) (string, bool) {
	i := strings.LastIndexByte(code, '\n')
	if i == -1 {
		return "", false
	}
	lineStart := i + 1
	tree, _ := parse.Parse(parse.Source{Name: "[autosuggestion]", Code: code}, parse.Config{})
	path := np.FindLeft(tree.Root, len(code))
	for _, n := range path {
		if form, ok := n.(*parse.Form); ok {
			return code[lineStart:], form.Range().From >= lineStart
		}
	}
	return "", false
}

// Whether s has prefix as a prefix, compared case-insensitively. The prefix
// must correspond to a prefix of s of the same byte length.
func foldHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	if len(s) > len(prefix) && !utf8.RuneStart(s[len(prefix)]) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}
