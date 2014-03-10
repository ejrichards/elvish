package edit

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/xiaq/elvish/util"
)

// Line editor builtins.
// These are not exposed to the user in anyway yet. Ideally, they should
// reside in a dedicated namespace and callable by users, e.g.
// le:kill-line-right.

type editorAction int

const (
	noAction editorAction = iota
	changeMode
	changeModeAndReprocess
	exitReadLine
)

type leReturn struct {
	action         editorAction
	newMode        bufferMode
	readLineReturn LineRead
}

type leBuiltin func(ed *Editor, k Key) *leReturn

var leBuiltins = map[string]leBuiltin{
	// Command and insert mode
	"start-insert":    startInsert,
	"start-command":   startCommand,
	"kill-line-left":  killLineLeft,
	"kill-line-right": killLineRight,
	"kill-rune-left":  killRuneLeft,
	"kill-rune-right": killRuneRight,
	"move-dot-left":   moveDotLeft,
	"move-dot-right":  moveDotRight,
	"move-dot-up":     moveDotUp,
	"move-dot-down":   moveDotDown,
	"insert-key":      insertKey,
	"return-line":     returnLine,
	"return-eof":      returnEORight,
	"default-command": defaultCommand,
	"default-insert":  defaultInsert,

	// Completion mode
	"start-completion":   startCompletion,
	"cancel-completion":  cancelCompletion,
	"select-cand-up":     selectCandUp,
	"select-cand-down":   selectCandDown,
	"select-cand-left":   selectCandLeft,
	"select-cand-right":  selectCandRight,
	"cycle-cand-right":   cycleCandRight,
	"default-completion": defaultCompletion,

	// Navigation mode
	"start-navigation":   startNavigation,
	"select-nav-up":      selectNavUp,
	"select-nav-down":    selectNavDown,
	"ascend-nav":         ascendNav,
	"descend-nav":        descendNav,
	"default-navigation": defaultNavigation,

	// History mode
	"start-history":       startHistory,
	"cancel-history":      cancelHistory,
	"select-history-prev": selectHistoryPrev,
	"select-history-next": selectHistoryNext,
	"default-history":     defaultHistory,
}

func startInsert(ed *Editor, k Key) *leReturn {
	return &leReturn{action: changeMode, newMode: modeInsert}
}

func defaultCommand(ed *Editor, k Key) *leReturn {
	ed.pushTip(fmt.Sprintf("Unbound: %s", k))
	return nil
}

func startCommand(ed *Editor, k Key) *leReturn {
	return &leReturn{action: changeMode, newMode: modeCommand}
}

func killLineLeft(ed *Editor, k Key) *leReturn {
	sol := util.FindLastSOL(ed.line[:ed.dot])
	ed.line = ed.line[:sol] + ed.line[ed.dot:]
	ed.dot = sol
	return nil
}

func killLineRight(ed *Editor, k Key) *leReturn {
	eol := util.FindFirstEOL(ed.line[ed.dot:]) + ed.dot
	ed.line = ed.line[:ed.dot] + ed.line[eol:]
	return nil
}

func killRuneLeft(ed *Editor, k Key) *leReturn {
	if ed.dot > 0 {
		_, w := utf8.DecodeLastRuneInString(ed.line[:ed.dot])
		ed.line = ed.line[:ed.dot-w] + ed.line[ed.dot:]
		ed.dot -= w
	} else {
		ed.beep()
	}
	return nil
}

func killRuneRight(ed *Editor, k Key) *leReturn {
	if ed.dot < len(ed.line) {
		_, w := utf8.DecodeRuneInString(ed.line[ed.dot:])
		ed.line = ed.line[:ed.dot] + ed.line[ed.dot+w:]
	} else {
		ed.beep()
	}
	return nil
}

func moveDotLeft(ed *Editor, k Key) *leReturn {
	_, w := utf8.DecodeLastRuneInString(ed.line[:ed.dot])
	ed.dot -= w
	return nil
}

func moveDotRight(ed *Editor, k Key) *leReturn {
	_, w := utf8.DecodeRuneInString(ed.line[ed.dot:])
	ed.dot += w
	return nil
}

func moveDotUp(ed *Editor, k Key) *leReturn {
	sol := util.FindLastSOL(ed.line[:ed.dot])
	if sol == 0 {
		ed.beep()
		return nil
	}
	prevEOL := sol - 1
	prevSOL := util.FindLastSOL(ed.line[:prevEOL])
	width := WcWidths(ed.line[sol:ed.dot])
	ed.dot = prevSOL + len(TrimWcWidth(ed.line[prevSOL:prevEOL], width))
	return nil
}

func moveDotDown(ed *Editor, k Key) *leReturn {
	eol := util.FindFirstEOL(ed.line[ed.dot:]) + ed.dot
	if eol == len(ed.line) {
		ed.beep()
		return nil
	}
	nextSOL := eol + 1
	nextEOL := util.FindFirstEOL(ed.line[nextSOL:]) + nextSOL
	sol := util.FindLastSOL(ed.line[:ed.dot])
	width := WcWidths(ed.line[sol:ed.dot])
	ed.dot = nextSOL + len(TrimWcWidth(ed.line[nextSOL:nextEOL], width))
	return nil
}

func insertKey(ed *Editor, k Key) *leReturn {
	ed.line = ed.line[:ed.dot] + string(k.rune) + ed.line[ed.dot:]
	ed.dot += utf8.RuneLen(k.rune)
	return nil
}

func returnLine(ed *Editor, k Key) *leReturn {
	return &leReturn{action: exitReadLine, readLineReturn: LineRead{Line: ed.line}}
}

func returnEORight(ed *Editor, k Key) *leReturn {
	if len(ed.line) == 0 {
		return &leReturn{action: exitReadLine, readLineReturn: LineRead{EOF: true}}
	}
	return nil
}

func selectCandUp(ed *Editor, k Key) *leReturn {
	ed.completion.prev(false)
	return nil
}

func selectCandDown(ed *Editor, k Key) *leReturn {
	ed.completion.next(false)
	return nil
}

func selectCandLeft(ed *Editor, k Key) *leReturn {
	if c := ed.completion.current - ed.completionLines; c >= 0 {
		ed.completion.current = c
	}
	return nil
}

func selectCandRight(ed *Editor, k Key) *leReturn {
	if c := ed.completion.current + ed.completionLines; c < len(ed.completion.candidates) {
		ed.completion.current = c
	}
	return nil
}

func cycleCandRight(ed *Editor, k Key) *leReturn {
	ed.completion.next(true)
	return nil
}

func cancelCompletion(ed *Editor, k Key) *leReturn {
	ed.completion = nil
	ed.mode = modeInsert
	return nil
}

func defaultInsert(ed *Editor, k Key) *leReturn {
	if k.Mod == 0 && k.rune > 0 && unicode.IsGraphic(k.rune) {
		return insertKey(ed, k)
	}
	ed.pushTip(fmt.Sprintf("Unbound: %s", k))
	return nil
}

func defaultCompletion(ed *Editor, k Key) *leReturn {
	ed.acceptCompletion()
	return &leReturn{action: changeModeAndReprocess, newMode: modeInsert}
}

func startNavigation(ed *Editor, k Key) *leReturn {
	ed.mode = modeNavigation
	ed.navigation = newNavigation()
	return &leReturn{}
}

func selectNavUp(ed *Editor, k Key) *leReturn {
	ed.navigation.prev()
	return &leReturn{}
}

func selectNavDown(ed *Editor, k Key) *leReturn {
	ed.navigation.next()
	return &leReturn{}
}

func ascendNav(ed *Editor, k Key) *leReturn {
	ed.navigation.ascend()
	return &leReturn{}
}

func descendNav(ed *Editor, k Key) *leReturn {
	ed.navigation.descend()
	return &leReturn{}
}

func defaultNavigation(ed *Editor, k Key) *leReturn {
	ed.mode = modeInsert
	ed.navigation = nil
	return &leReturn{}
}

func startHistory(ed *Editor, k Key) *leReturn {
	ed.history.saved = ed.line
	ed.history.prefix = ed.line[:ed.dot]
	ed.history.current = len(ed.history.items)
	if ed.history.prev() {
		ed.mode = modeHistory
	} else {
		ed.pushTip("no matching history item")
	}
	return nil
}

func cancelHistory(ed *Editor, k Key) *leReturn {
	ed.mode = modeInsert
	return nil
}

func selectHistoryPrev(ed *Editor, k Key) *leReturn {
	ed.history.prev()
	return nil
}

func selectHistoryNext(ed *Editor, k Key) *leReturn {
	ed.history.next()
	return nil
}

func defaultHistory(ed *Editor, k Key) *leReturn {
	ed.acceptHistory()
	return &leReturn{action: changeModeAndReprocess, newMode: modeInsert}
}
