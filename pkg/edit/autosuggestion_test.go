package edit

import (
	"testing"
	"time"

	"src.elv.sh/pkg/cli/term"
	"src.elv.sh/pkg/cli/tk"
	"src.elv.sh/pkg/edit/complete"
	"src.elv.sh/pkg/eval"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/testutil"
	"src.elv.sh/pkg/ui"
)

// Tests for the suggester itself. These use builtin commands only, so that
// the validation of history entries doesn't depend on the environment.

func TestSuggester_SuggestsMostRecentMatch(t *testing.T) {
	s := newTestSuggester(t, "echo a", "put x", "echo b")
	testSuggestion(t, s, "e", "echo b")
	testSuggestion(t, s, "p", "put x")
	testSuggestion(t, s, "x", "")
}

func TestSuggester_SkipsExactMatch(t *testing.T) {
	s := newTestSuggester(t, "echo a b", "echo a")
	testSuggestion(t, s, "echo a", "echo a b")
}

func TestSuggester_NoSuggestionForEmptyCode(t *testing.T) {
	s := newTestSuggester(t, "echo a")
	if got := s.Get(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSuggester_Disabled(t *testing.T) {
	enabled := true
	s := newSuggester(newTestHistStore("echo a"), eval.NewEvaler(), func() bool { return enabled })
	testSuggestion(t, s, "e", "echo a")
	enabled = false
	if got := s.Get("e"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSuggester_PrefersExactMatchOverCaseInsensitiveMatch(t *testing.T) {
	s := newTestSuggester(t, "echo a x", "echo B")
	// Exact match wins, even if older.
	testFreshSuggestion(t, s, "echo a", "echo a x")
	testFreshSuggestion(t, s, "echo B", "")
	testFreshSuggestion(t, s, "echo", "echo B")
	testFreshSuggestion(t, s, "echo A", "echo a x")
	testFreshSuggestion(t, s, "ECHO", "echo B")
	testFreshSuggestion(t, s, "ECHO A", "echo a x")
	// A case-insensitive match of the same length has nothing to show.
	testFreshSuggestion(t, s, "ECHO B", "")
}

func TestSuggester_KeepsSuggestionWhenTypingMatchingText(t *testing.T) {
	s := newTestSuggester(t, "echo a", "echo b")
	testSuggestion(t, s, "e", "echo b")
	// Typing more characters that match the suggestion should reuse the
	// cached result without a new lookup.
	testCachedSuggestion(t, s, "ec", "echo b")
	testCachedSuggestion(t, s, "echo ", "echo b")
	// Typing all of the suggestion makes it an exact match; a new lookup is
	// needed to find an older, longer command.
	testSuggestion(t, s, "echo b", "")
	// Typing something that doesn't match the suggestion also needs a new
	// lookup.
	testSuggestion(t, s, "echo a", "")
}

func TestSuggester_KeepsCaseInsensitiveSuggestionWhenTypingMatchingText(t *testing.T) {
	s := newTestSuggester(t, "echo a")
	testSuggestion(t, s, "E", "echo a")
	testCachedSuggestion(t, s, "EC", "echo a")
	testCachedSuggestion(t, s, "ECho", "echo a")
}

func TestSuggester_ReconsidersExactMatchWhenCaseStopsMatching(t *testing.T) {
	s := newTestSuggester(t, "echo A x", "echo a")
	testSuggestion(t, s, "echo", "echo a")
	// "echo A" only matches "echo a" case-insensitively, but matches "echo A
	// x" exactly, so a new lookup is needed and finds the latter.
	testSuggestion(t, s, "echo A", "echo A x")
}

func TestSuggester_KeepsNoMatchWhenTypingMore(t *testing.T) {
	s := newTestSuggester(t, "echo a")
	testSuggestion(t, s, "x", "")
	testCachedSuggestion(t, s, "xy", "")
	// Deleting all characters and typing again needs a new lookup.
	testSuggestion(t, s, "e", "echo a")
}

func TestSuggester_SuppressesAfterDeletion(t *testing.T) {
	s := newTestSuggester(t, "echo a", "echo b", "ex")
	testSuggestion(t, s, "ec", "echo b")
	// Deleting doesn't trigger a new lookup, even though "e" would match.
	testCachedSuggestion(t, s, "e", "")
	// Typing again does.
	testSuggestion(t, s, "ex", "")
	testSuggestion(t, s, "ec", "echo b")
}

func TestSuggester_RestoresSuggestionAfterDeletion(t *testing.T) {
	s := newTestSuggester(t, "echo a")
	testSuggestion(t, s, "ec", "echo a")
	// Typing something that doesn't match replaces the suggestion.
	testSuggestion(t, s, "ecx", "")
	// Deleting back to the code the suggestion was for restores it.
	testCachedSuggestion(t, s, "ec", "echo a")
	// Deleting further suppresses suggestions.
	testCachedSuggestion(t, s, "e", "")
}

func TestSuggester_InvalidatesCacheWhenHistoryChanges(t *testing.T) {
	hs := newTestHistStore("echo a")
	s := newSuggester(hs, eval.NewEvaler(), func() bool { return true })
	testSuggestion(t, s, "e", "echo a")

	hs.AddCmd(storedefs.Cmd{Text: "echo b", Seq: -1})
	testSuggestion(t, s, "e", "echo b")

	// Without a DB, fast-forwarding results in an empty store.
	hs.FastForward()
	testSuggestion(t, s, "e", "")
}

func TestSuggester_DiscardsLateResultForOldCode(t *testing.T) {
	s := newTestSuggester(t, "echo a", "put x")
	// Request two lookups back to back; the first result must not be
	// delivered as the result of the second code.
	s.Get("e")
	s.Get("p")
	waitLateUpdate(t, s)
	// There may be one or two late updates depending on timing; what matters
	// is that the result for "e" is never reported as the result for "p".
	if got := s.Get("p"); got != "" && got != "put x" {
		t.Errorf("got %q, want empty or \"put x\"", got)
	}
	testSuggestion(t, s, "p", "put x")
}

func TestSuggester_RejectsEntriesWithMissingCommands(t *testing.T) {
	testutil.Setenv(t, "PATH", "")
	s := newTestSuggester(t, "echo a", "nonexistent-command a")
	testSuggestion(t, s, "n", "")
	// Commands in all pipelines are checked.
	s = newTestSuggester(t, "echo a | nonexistent-command")
	testSuggestion(t, s, "e", "")
	// Variable commands can't be checked and are assumed to exist.
	s = newTestSuggester(t, "$f a")
	testSuggestion(t, s, "$", "$f a")
}

func TestSuggester_RejectsEntriesWithMissingPaths(t *testing.T) {
	testutil.InTempDir(t)
	testutil.ApplyDir(testutil.Dir{"f": "", "d": testutil.Dir{}})
	s := newTestSuggester(t,
		"echo ./f", "echo ./nope", "echo d/", "echo /nonexistent/x", "echo ~/nonexistent-dir-hopefully",
		"echo plain-arg", "echo -o/x", "echo $x/y")
	testFreshSuggestion(t, s, "echo ./f", "")
	testFreshSuggestion(t, s, "echo ./", "echo ./f")
	testFreshSuggestion(t, s, "echo d", "echo d/")
	testFreshSuggestion(t, s, "echo /", "")
	testFreshSuggestion(t, s, "echo ~", "")
	// Arguments that don't look like paths are not checked.
	testFreshSuggestion(t, s, "echo p", "echo plain-arg")
	// Flags and arguments that can't be evaluated purely are not checked.
	testFreshSuggestion(t, s, "echo -", "echo -o/x")
	testFreshSuggestion(t, s, "echo $", "echo $x/y")
}

func TestSuggester_RejectsCdToNonDirOrCwd(t *testing.T) {
	testutil.InTempDir(t)
	testutil.ApplyDir(testutil.Dir{"f": "", "d": testutil.Dir{}})
	s := newTestSuggester(t, "cd d", "cd f", "cd nope", "cd .")
	testFreshSuggestion(t, s, "cd d", "")
	testFreshSuggestion(t, s, "cd f", "")
	testFreshSuggestion(t, s, "cd n", "")
	testFreshSuggestion(t, s, "cd .", "")
	testFreshSuggestion(t, s, "cd", "cd d")
}

func TestSuggester_MultiLine(t *testing.T) {
	s := newTestSuggester(t, "echo a")
	// The last line starts a new command, and is matched against lines of
	// history entries.
	testSuggestion(t, s, "echo x\necho", "echo x\necho a")
	testSuggestion(t, s, "echo x\n  echo", "echo x\n  echo a")
	testSuggestion(t, s, "{\necho", "{\necho a")
	// The last line continues an expression.
	testSuggestion(t, s, "echo [\necho", "")
	testSuggestion(t, s, "echo x ^\necho", "")
	// Empty last line.
	testSuggestion(t, s, "echo x\n", "")
	// Multi-line entries are matched line by line.
	s = newTestSuggester(t, "echo x\necho a b\necho c")
	testSuggestion(t, s, "put y\necho a", "put y\necho a b")
	// Entries starting with the whole code are preferred.
	s = newTestSuggester(t, "put y\necho a b", "echo a c")
	testSuggestion(t, s, "put y\necho a", "put y\necho a b")
}

func TestSuggester_FallsBackToCompletion(t *testing.T) {
	testutil.InTempDir(t)
	testutil.ApplyDir(testutil.Dir{"foo.txt": "", "fop": "", "d": testutil.Dir{}})
	s := newTestSuggester(t, "echo a", "echo ./foo.txt")
	s.setCompleteCfg(func() complete.Config { return complete.Config{} })
	// Completion is used when the history has no match. Like Tab completion,
	// a trailing space is included for non-directories.
	testFreshSuggestion(t, s, "put fo", "put foo.txt ")
	testFreshSuggestion(t, s, "put foo.", "put foo.txt ")
	testFreshSuggestion(t, s, "put d", "put d/")
	// An exact history match is preferred over a completion.
	testFreshSuggestion(t, s, "echo ./fo", "echo ./foo.txt")
	// A case-insensitive history match is used when there is no completion.
	testFreshSuggestion(t, s, "ECHO", "echo ./foo.txt")
	// A completion is preferred over a case-insensitive history match.
	testFreshSuggestion(t, s, "ECHO ./fo", "ECHO ./foo.txt ")
	// No completion for an empty token or after a quote.
	testFreshSuggestion(t, s, "put ", "")
	testFreshSuggestion(t, s, "put 'fo'", "")
	// The completion is kept while the typed characters match it.
	testFreshSuggestion(t, s, "put f", "put foo.txt ")
	testCachedSuggestion(t, s, "put foo", "put foo.txt ")
	testSuggestion(t, s, "put fop", "put fop ")
	// A new token is completed again even if the history has no match.
	testFreshSuggestion(t, s, "put x", "")
	testSuggestion(t, s, "put x f", "put x foo.txt ")
}

func newTestHistStore(cmds ...string) *histStore {
	hs, _ := newHistStore(nil)
	for _, cmd := range cmds {
		hs.AddCmd(storedefs.Cmd{Text: cmd, Seq: -1})
	}
	return hs
}

func newTestSuggester(t *testing.T, cmds ...string) *suggester {
	t.Helper()
	return newSuggester(newTestHistStore(cmds...), eval.NewEvaler(), func() bool { return true })
}

// Tests that the suggester eventually gives the suggestion for the code,
// waiting for a late update if the lookup is asynchronous.
func testSuggestion(t *testing.T, s *suggester, code, want string) {
	t.Helper()
	got := s.Get(code)
	if !lookupDone(s) {
		waitLateUpdate(t, s)
		got = s.Get(code)
	}
	if got != want {
		t.Errorf("suggestion for %q is %q, want %q", code, got, want)
	}
}

// Like testSuggestion, but first clears the suggester's cache, so that the
// result doesn't depend on previous queries (in particular, deleting
// characters suppresses suggestions).
func testFreshSuggestion(t *testing.T, s *suggester, code, want string) {
	t.Helper()
	s.mu.Lock()
	s.valid, s.savedCode = false, ""
	s.mu.Unlock()
	testSuggestion(t, s, code, want)
}

// Tests that the suggester gives the suggestion for the code synchronously
// from its cache, without starting a new lookup.
func testCachedSuggestion(t *testing.T, s *suggester, code, want string) {
	t.Helper()
	got := s.Get(code)
	if !lookupDone(s) {
		t.Errorf("lookup started for %q, want cached result", code)
		waitLateUpdate(t, s)
		got = s.Get(code)
	}
	if got != want {
		t.Errorf("suggestion for %q is %q, want %q", code, got, want)
	}
}

func lookupDone(s *suggester) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func waitLateUpdate(t *testing.T, s *suggester) {
	t.Helper()
	select {
	case <-s.LateUpdates():
	case <-time.After(testutil.Scaled(time.Second)):
		t.Fatal("timed out waiting for late update")
	}
}

// Tests for the integration into the editor.

func TestAutosuggestion_Shown(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
}

func TestAutosuggestion_ShownWhenDotNotAtEnd(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TTYCtrl.Inject(term.K(ui.Left))
	f.TestTTY(t,
		"~> ech", Styles,
		"   vvv", term.DotHere,
		"o a", Styles,
		"vgg",
	)
	// Moving right just moves the dot since it is not at the end.
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	// But edit:autosuggestion:accept works with the dot anywhere.
	f.TTYCtrl.Inject(term.K(ui.Left))
	f.TestTTY(t,
		"~> ech", Styles,
		"   vvv", term.DotHere,
		"o a", Styles,
		"vgg",
	)
	evals(f.Evaler, "edit:autosuggestion:accept; edit:redraw")
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
	)
}

func TestAutosuggestion_EndAcceptsOneLine(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a\necho b")
	feedInput(f.TTYCtrl, "echo")
	f.TTYCtrl.Inject(term.K(ui.End))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere, "\n",
		"   echo b", Styles,
		"   gggggg",
	)
}

func TestAutosuggestion_SuppressedAfterDeletion(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K(ui.Backspace))
	f.TestTTY(t,
		"~> ech", Styles,
		"   !!!", term.DotHere,
	)
	feedInput(f.TTYCtrl, "o")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
}

func TestAutosuggestion_Disabled(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	evals(f.Evaler, "set edit:autosuggestion:enabled = $false")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
	)
}

func TestAutosuggestion_AcceptWithRight(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
	)
}

func TestAutosuggestion_AcceptWithEnd(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K(ui.End))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
	)
}

func TestAutosuggestion_AcceptWordWithAltF(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a b")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a b", Styles,
		"gggg",
	)
	f.TTYCtrl.Inject(term.K('f', ui.Alt))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
		" b", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K('f', ui.Alt))
	f.TestTTY(t,
		"~> echo a b", Styles,
		"   vvvv    ", term.DotHere,
	)
}

func TestAutosuggestion_AcceptFunctions(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a b")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a b", Styles,
		"gggg",
	)
	evals(f.Evaler, "edit:autosuggestion:accept-word; edit:redraw")
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
		" b", Styles,
		"gg",
	)
	evals(f.Evaler, "edit:autosuggestion:accept; edit:redraw")
	f.TestTTY(t,
		"~> echo a b", Styles,
		"   vvvv    ", term.DotHere,
	)
}

func TestAutosuggestion_CaseInsensitiveMatchCorrectsCaseWhenAccepted(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "ECHO")
	f.TestTTY(t,
		"~> ECHO", Styles,
		"   !!!!", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
	)
}

func TestAutosuggestion_FromCompletion(t *testing.T) {
	f := setupAutosuggestionTest(t)
	testutil.ApplyDir(testutil.Dir{"foo.txt": ""})
	feedInput(f.TTYCtrl, "echo fo")
	f.TestTTY(t,
		"~> echo fo", Styles,
		"   vvvv", term.DotHere,
		"o.txt ", Styles,
		"gggggg",
	)
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> echo foo.txt ", Styles,
		"   vvvv", term.DotHere,
	)
}

func TestAutosuggestion_RejectsEntriesWithMissingPaths(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo ./f", "echo ./nope")
	testutil.ApplyDir(testutil.Dir{"f": ""})
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" ./f", Styles,
		"gggg",
	)
}

func TestAutosuggestion_NotAcceptedAfterHistwalkImplicitAccept(t *testing.T) {
	// History walk shows "echo a" as pending code (so no suggestion is
	// shown). Pressing Right accepts the pending code and forwards the key to
	// the code area, which must not accept the suggestion for the new buffer
	// content ("echo a b"), since it was never shown.
	f := setupAutosuggestionTest(t, "echo a b", "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K(ui.Up))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv__", term.DotHere, "\n",
		" HISTORY #2 ", Styles,
		"************",
	)
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> echo a", Styles,
		"   vvvv  ", term.DotHere,
		" b", Styles,
		"gg",
	)
}

func TestAutosuggestion_HiddenInListingMode(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.TTYCtrl.Inject(term.K('R', ui.Ctrl))
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", "\n",
		" HISTORY (dedup on)  ", Styles,
		"******************** ", term.DotHere,
		"                 Ctrl-D dedup\n", Styles,
		"                 ++++++      ",
		"   1 echo a                                       ", Styles,
		"++++++++++++++++++++++++++++++++++++++++++++++++++",
	)
}

func TestAutosuggestion_HiddenAfterSubmit(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	feedInput(f.TTYCtrl, "\n")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", "\n", term.DotHere,
	)
	if code := <-f.codeCh; code != "echo" {
		t.Errorf("got code %q, want %q", code, "echo")
	}
}

func TestAutosuggestion_SeesNewHistoryAfterFastForward(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	feedInput(f.TTYCtrl, "echo")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" a", Styles,
		"gg",
	)
	f.Store.AddCmd("echo b")
	evals(f.Evaler, "edit:history:fast-forward; edit:redraw")
	f.TestTTY(t,
		"~> echo", Styles,
		"   vvvv", term.DotHere,
		" b", Styles,
		"gg",
	)
}

func TestAutosuggestion_DotInMiddleRightJustMoves(t *testing.T) {
	f := setupAutosuggestionTest(t, "echo a")
	f.SetCodeBuffer(tk.CodeBuffer{Content: "echo", Dot: 2})
	evals(f.Evaler, "edit:redraw")
	f.TTYCtrl.Inject(term.K(ui.Right))
	f.TestTTY(t,
		"~> ech", Styles,
		"   vvv", term.DotHere,
		"o a", Styles,
		"vgg",
	)
}

func setupAutosuggestionTest(t *testing.T, cmds ...string) *fixture {
	t.Helper()
	return setup(t, storeOp(func(s storedefs.Store) {
		for _, cmd := range cmds {
			s.AddCmd(cmd)
		}
	}))
}
