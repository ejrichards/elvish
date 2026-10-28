package modes

import (
	"fmt"
	"strings"
	"time"

	"src.elv.sh/pkg/cli"
	"src.elv.sh/pkg/cli/tk"
	"src.elv.sh/pkg/store/storedefs"
	"src.elv.sh/pkg/ui"
	"src.elv.sh/pkg/wcwidth"
)

// Histlist is a mode for browsing history and selecting entries to insert. It
// is based on the ComboBox widget.
type Histlist interface {
	tk.ComboBox
}

// HistlistSpec specifies the configuration for the histlist mode.
type HistlistSpec struct {
	// Key bindings.
	Bindings tk.Bindings
	// Search is called with the filter text and whether to deduplicate, and
	// returns the matching entries, newest first. It is called once when the
	// mode starts and again whenever the filter changes; filtering is done
	// by the callee, which can limit how many entries it returns.
	Search func(filter string, dedup bool) ([]storedefs.CmdHit, error)
	// Whether to show the metadata of entries: how long they ran (colored by
	// exit status) and how long ago they were run.
	ShowMeta bool
	// Now is used to compute how long ago each command was run. Defaults to
	// time.Now if unset.
	Now func() time.Time
	// HighlightCmd is called to highlight the text of each entry. If nil,
	// entries are shown unstyled.
	HighlightCmd func(code string) ui.Text
	// Dedup is called to determine whether deduplication should be done.
	// Defaults to true if unset.
	Dedup func() bool
	// ScopeLabel is called for a label of the scope the search is limited
	// to, shown in the mode line. An empty label, or a nil function, shows
	// nothing.
	ScopeLabel func() string
	// Highlighter for the filter text.
	FilterHighlighter func(string) (ui.Text, []ui.Text)
	// RPrompt of the code area (first row of the widget).
	CodeAreaRPrompt func() ui.Text
}

// NewHistlist creates a new histlist mode.
func NewHistlist(app cli.App, spec HistlistSpec) (Histlist, error) {
	codeArea, err := FocusedCodeArea(app)
	if err != nil {
		return nil, err
	}
	if spec.Search == nil {
		return nil, errNoHistoryStore
	}
	if spec.Dedup == nil {
		spec.Dedup = func() bool { return true }
	}
	if spec.Now == nil {
		spec.Now = time.Now
	}
	if spec.HighlightCmd == nil {
		spec.HighlightCmd = func(code string) ui.Text { return ui.T(code) }
	}

	// The initial search is done before building the widget, so that a
	// store error fails the mode instead of showing an empty list.
	hits, err := spec.Search("", spec.Dedup())
	if err != nil {
		return nil, fmt.Errorf("db error: %v", err.Error())
	}
	items := func(hits []storedefs.CmdHit) histlistItems {
		return histlistItems{hits, spec.ShowMeta, spec.Now(), spec.HighlightCmd,
			make([]ui.Text, len(hits))}
	}
	first := true
	w := tk.NewComboBox(tk.ComboBoxSpec{
		CodeArea: tk.CodeAreaSpec{
			Prompt: func() ui.Text {
				content := " HISTORY "
				if spec.Dedup() {
					content += "(dedup on) "
				}
				if spec.ScopeLabel != nil {
					if label := spec.ScopeLabel(); label != "" {
						content += "(" + label + ") "
					}
				}
				return modeLine(content, true)
			},
			RPrompt:     spec.CodeAreaRPrompt,
			Highlighter: spec.FilterHighlighter,
		},
		ListBox: tk.ListBoxSpec{
			Bindings: spec.Bindings,
			OnAccept: func(it tk.Items, i int) {
				text := it.(histlistItems).cmd(i).Text
				codeArea.MutateState(func(s *tk.CodeAreaState) {
					buf := &s.Buffer
					if buf.Content == "" {
						buf.InsertAtDot(text)
					} else {
						buf.InsertAtDot("\n" + text)
					}
				})
				app.PopAddon()
			},
		},
		OnFilter: func(w tk.ComboBox, p string) {
			// The combo box filters once on creation with an empty filter;
			// that is the search already done above.
			if first {
				first = false
			} else {
				hits, err = spec.Search(p, spec.Dedup())
				if err != nil {
					app.Notify(ErrorText(err))
					hits = nil
				}
			}
			it := items(hits)
			w.ListBox().Reset(it, it.Len()-1)
		},
	})
	return w, nil
}

// Items of the histlist. Hits are newest first, as returned by the search;
// the list shows them oldest first, with the newest at the bottom. Fuzzy
// hits, which the search returns after the others ranked best first, thus
// end up above the exact matches with the best nearest to them.
type histlistItems struct {
	hits     []storedefs.CmdHit
	showMeta bool
	// Reference point for showing how long ago each command was run.
	now time.Time
	// Highlights the text of an entry.
	highlight func(code string) ui.Text
	// Highlighted text of each hit, indexed like hits and filled in as
	// entries are shown. Highlighting parses the command, and the visible
	// entries are shown again on every redraw, so it is only done once per
	// entry. A slice, so that copies of the struct share it.
	highlighted []ui.Text
}

func (it histlistItems) hit(i int) storedefs.CmdHit { return it.hits[len(it.hits)-1-i] }

func (it histlistItems) cmd(i int) storedefs.Cmd { return it.hit(i).Cmd }

// Returns the highlighted text of the i-th item, highlighting it on first use.
func (it histlistItems) text(i int) ui.Text {
	j := len(it.hits) - 1 - i
	if it.highlighted[j] == nil {
		t := it.highlight(it.hits[j].Cmd.Text)
		if t == nil {
			t = ui.Text{}
		}
		it.highlighted[j] = t
	}
	return it.highlighted[j]
}

// Widths of the duration and "ago" columns. They fit all but the most extreme
// values ("59m59s" and "364d ago"); longer values push the rest of the line.
const (
	durationWidth = 6
	agoWidth      = 8
)

func (it histlistItems) Show(i int) ui.Text {
	hit := it.hit(i)
	entry := hit.Cmd
	// TODO: The alignment of the index works up to 10000 entries.
	// The sequence number of a fuzzy hit is dimmed, which marks where the
	// exact matches end.
	seq := ui.T(fmt.Sprintf("%4d ", entry.Seq))
	if hit.Fuzzy {
		seq = ui.T(fmt.Sprintf("%4d ", entry.Seq), ui.FgBrightBlack)
	}
	if !it.showMeta {
		return ui.Concat(seq, it.text(i))
	}
	m := hit.Meta
	var tb ui.TextBuilder
	tb.WriteText(seq)
	// A zero duration means the command hasn't finished (or the entry has no
	// metadata at all), so leave the column blank.
	if m.Duration == 0 {
		tb.WriteText(ui.T(strings.Repeat(" ", durationWidth)))
	} else {
		d := formatDuration(m.Duration)
		tb.WriteText(ui.T(pad(durationWidth, d)))
		if m.Exit == 0 {
			tb.WriteText(ui.T(d, ui.FgGreen))
		} else {
			tb.WriteText(ui.T(d, ui.FgRed))
		}
	}
	tb.WriteText(ui.T(" "))
	if m.Time.IsZero() {
		tb.WriteText(ui.T(strings.Repeat(" ", agoWidth)))
	} else {
		ago := formatAgo(it.now.Sub(m.Time))
		tb.WriteText(ui.T(pad(agoWidth, ago)))
		tb.WriteText(ui.T(ago, ui.FgBlue))
	}
	tb.WriteText(ui.T(" "))
	tb.WriteText(it.text(i))
	return tb.Text()
}

// Returns the spaces needed to right-align s in a column of the given width.
func pad(width int, s string) string {
	if n := width - wcwidth.Of(s); n > 0 {
		return strings.Repeat(" ", n)
	}
	return ""
}

// Formats a duration in seconds compactly, like "500ns", "12µs", "22ms",
// "1.5s", "20s", "1m30s" or "2h5m".
func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// Formats how long ago something happened, like "20s ago", "5m ago", "3h ago",
// "2d ago" or "1y ago".
func formatAgo(d time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 365*day:
		return fmt.Sprintf("%dd ago", int(d/day))
	default:
		return fmt.Sprintf("%dy ago", int(d/(365*day)))
	}
}

func (it histlistItems) Len() int { return len(it.hits) }
