package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/history"
	"github.com/daniil4545/tix/internal/task"
	"github.com/daniil4545/tix/internal/tui/view"
)

// cardState - the card: the history cursor and the expanded edit series.
type cardState struct {
	id      string
	entries []history.Entry
	rows    []shown
	mtime   time.Time
	cursor  int             // criterion rows, then history entries
	moved   bool            // the user moved the cursor: the first history load does not shift it
	scroll  int             // body scroll in lines: the wheel folds the frame offset in first
	follow  bool            // the view fits the window to the cursor; a wheel stops it until the cursor moves
	nCrit   int             // the number of criteria before the history; updated on open and in cardView
	open    map[string]bool // expanded "N edits" series
	loaded  bool            // the history has arrived at least once
}

// shown - a history row on the screen. group - the series index in entries, -1 for a single entry.
type shown struct {
	entry history.Entry
	group int
}

// historyMsg - the card history from git and the file comments.
type historyMsg struct {
	id      string
	entries []history.Entry
	mtime   time.Time // fallback modification date if there is no history
	err     error
}

// openCard opens the card of the selected task; the history loads in the background.
func (m *Model) openCard() tea.Cmd {
	t := m.selected()
	if t == nil {
		return nil
	}
	m.showing = true
	m.card = cardState{id: t.ID, open: map[string]bool{}, nCrit: len(selectableCriteria(t)), follow: true}
	return m.historyCmd(t)
}

// historyCmd reads the task history; the first card frame does not wait for it.
func (m *Model) historyCmd(t *task.Task) tea.Cmd {
	return m.guard(func() tea.Msg {
		var mtime time.Time
		if info, err := os.Stat(t.Path); err == nil {
			mtime = info.ModTime()
		}
		bin, err := gitx.Resolve(m.d.GitBin)
		if err != nil {
			return historyMsg{id: t.ID, mtime: mtime, err: err}
		}
		es, err := history.Load(gitx.Repo{Dir: m.d.Root, Bin: bin}, t)
		return historyMsg{id: t.ID, entries: es, mtime: mtime, err: err}
	})
}

// set takes the history and puts the cursor on the last entry on the first load.
func (c *cardState) set(msg historyMsg) {
	c.entries, c.mtime = msg.entries, msg.mtime
	c.rebuild()
	if !c.loaded && !c.moved {
		c.cursor = c.nCrit + max(len(c.rows)-1, 0)
		c.follow = true
	}
	c.loaded = true
	c.cursor = min(c.cursor, c.last())
}

func (m *Model) setHistoryMsg(msg historyMsg) {
	if !m.showing || m.card.id != msg.id {
		return
	}
	m.card.set(msg)
	if msg.err != nil {
		m.setStatus("history: "+firstLine(msg.err.Error()), true)
	}
}

// rebuild lays the entries out as screen rows: an expanded series is replaced by its edits.
func (c *cardState) rebuild() {
	c.rows = c.rows[:0]
	for i, e := range c.entries {
		switch {
		case len(e.Folded) == 0:
			c.rows = append(c.rows, shown{entry: e, group: -1})
		case c.open[groupKey(e)]:
			for _, f := range e.Folded {
				c.rows = append(c.rows, shown{entry: f, group: i})
			}
		default:
			c.rows = append(c.rows, shown{entry: e, group: i})
		}
	}
}

func groupKey(e history.Entry) string {
	return fmt.Sprintf("%d %s", e.Folded[0].At.UnixNano(), e.Actor)
}

// changed - the time of the last history entry (the feed goes from old to new), without history - the file mtime.
func (c *cardState) changed() time.Time {
	if len(c.entries) == 0 {
		return c.mtime
	}
	return c.entries[len(c.entries)-1].At
}

// last - the index of the last selectable row: criteria and history.
func (c *cardState) last() int { return max(c.nCrit+len(c.rows)-1, 0) }

// step moves the cursor over criteria and history; the view follows it again.
func (c *cardState) step(d int) {
	c.moved = true
	c.follow = true
	c.cursor = min(max(c.cursor+d, 0), c.last())
}

// scrollBy scrolls the body from base by d lines by hand: the view stops following the cursor
// until it moves.
func (c *cardState) scrollBy(base, d int) {
	c.follow = false
	c.scroll = base + d
}

// toggle expands the edit series under the cursor or collapses an expanded one. A criterion row
// is not a series: cardEnter toggles it.
func (c *cardState) toggle() {
	row := c.cursor - c.nCrit
	if row < 0 || row >= len(c.rows) || c.rows[row].group < 0 {
		return
	}
	g := c.rows[row].group
	key := groupKey(c.entries[g])
	c.open[key] = !c.open[key]
	c.rebuild()
	for i, r := range c.rows {
		if r.group == g {
			c.follow = true
			c.cursor = c.nCrit + i
			return
		}
	}
}

// syncCardScroll stores the body offset of the frame on the screen: the fit while the view follows
// the cursor, else the clamped scroll.
func (m *Model) syncCardScroll() {
	m.card.scroll = view.CardScroll(m.cardView())
}

// cardView builds the card frame from the current task file.
func (m *Model) cardView() view.Card {
	if t := m.byID(m.card.id); t != nil {
		m.card.nCrit = len(selectableCriteria(t))
	}
	m.card.cursor = min(m.card.cursor, m.card.last())
	c := view.Card{ID: m.card.id, Cursor: m.card.cursor, W: m.w, H: m.h,
		Scroll: m.card.scroll, Follow: m.card.follow}
	c.Status, c.IsErr = m.statusText()
	c.History = make([]history.Entry, len(m.card.rows))
	for i, r := range m.card.rows {
		c.History[i] = r.entry
	}
	t := m.byID(m.card.id)
	if t == nil {
		return c
	}
	c.Title, c.Column, c.Ask = t.Title, t.Status, strings.TrimSpace(t.Ask)
	c.Fields = fields(t, time.Now(), m.card.changed())
	c.Abstract, c.Summary = section(t.Body, "Abstract"), section(t.Body, "Summary")
	c.Details = section(t.Body, "Details")
	if crit := t.Criteria(); len(crit) > 0 {
		c.Checks = checks(crit)
		c.CritAt = selectableCriteria(t)
	}
	return c
}

// fields - the card fields line: project, external link, claim, wait, due, tags, modification date.
func fields(t *task.Task, now time.Time, changed time.Time) string {
	parts := []string{t.Project}
	if t.External != "" {
		parts = append(parts, t.External)
	}
	if c, ok := t.ClaimInfo(); ok && c.Until.After(now) {
		parts = append(parts, "@"+actor.Short(c.Actor)+" until "+c.Until.Format("15:04"))
	}
	if strings.TrimSpace(t.Waiting) != "" {
		parts = append(parts, "waiting for: "+t.Waiting)
	}
	if t.Due != "" {
		parts = append(parts, "due "+dueText(t.Due))
	}
	if len(t.Tags) > 0 {
		parts = append(parts, "#"+strings.Join(t.Tags, " #"))
	}
	if !changed.IsZero() {
		parts = append(parts, "changed "+changed.Format("02.01 15:04"))
	}
	return strings.Join(parts, "   ")
}

// section returns the text of section "## name" of the task body without empty edges.
func section(body, name string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimRight(ln, " \t\r")
		switch {
		case strings.HasPrefix(ln, "## "):
			if in {
				return strings.TrimSpace(strings.Join(out, "\n"))
			}
			in = strings.EqualFold(strings.TrimPrefix(ln, "## "), name)
		case in:
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// criterionRows - indexes of criterion rows in the Details section text. The row count must match
// the task criteria count, otherwise criterion choice is off: the section and the core read different things.
func criterionRows(details string, want int) []int {
	at := task.CriterionRows(details)
	if len(at) != want {
		return nil
	}
	return at
}

// selectableCriteria - the task criterion rows in the Details text that the cursor selects.
func selectableCriteria(t *task.Task) []int {
	return criterionRows(section(t.Body, "Details"), len(t.Criteria()))
}
