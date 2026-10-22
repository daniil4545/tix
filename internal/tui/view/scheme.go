package view

import (
	"hash/fnv"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// The board color scheme. Change the scheme here: card states, signs, colors, the overdue style
// and the project palette. The board code only looks these values up.

// cardState - what a card tells the human: an agent works, the agent waits for you, or nobody is on it.
type cardState int

const (
	stateIdle    cardState = iota // no live claim: the whole card is grey
	stateWorking                  // live claim, no ask
	stateWaiting                  // live claim and an ask
)

// cardLook - how a state is drawn. Head is the ID line, Title the title, Rest the meta, note and summary
// lines. Sign is printed at the end of the ID line ("" - none). KindColor: the sign and the ask text
// take the color of the ask kind.
type cardLook struct {
	Head, Title, Rest lipgloss.Style
	Sign              string
	KindColor         bool
}

var (
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6FCF7A"))

	scheme = [...]cardLook{
		stateIdle:    {Head: dimStyle, Title: dimStyle, Rest: dimStyle},
		stateWorking: {Head: lipgloss.NewStyle(), Title: greenStyle, Rest: dimStyle},
		stateWaiting: {Head: lipgloss.NewStyle(), Title: greenStyle, Rest: dimStyle, Sign: "*", KindColor: true},
	}

	// askKindColors - the sign color by ask kind: blue and red are the terminal ANSI colors.
	askKindColors = map[string]string{"decide": "#E9CF6A", "review": "4", "access": "1"}

	// overdueStyle - the due part of the meta line when the date is in the past.
	overdueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8606A"))

	// projectPalette - muted colors of the project label; the name picks one by hash.
	projectPalette = [6]lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#82AAFF")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#C792EA")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#F78C6C")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#89DDFF")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9CAC")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#C3A57A")),
	}

	// selKindBase - the selected line behind a colored sign.
	selKindBase = lipgloss.NewStyle().Background(lipgloss.Color("#7FD1C3"))
)

// stateOf picks the state of a card: a live claim, then an ask.
func stateOf(row Row) cardState {
	switch {
	case !row.Live:
		return stateIdle
	case strings.TrimSpace(row.Ask) != "":
		return stateWaiting
	}
	return stateWorking
}

// kindStyle - the style of an ask kind; an unknown kind is decide.
func kindStyle(kind string) lipgloss.Style {
	color, ok := askKindColors[kind]
	if !ok {
		color = askKindColors["decide"]
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color))
}

// selKindStyle - kindStyle on the selected line.
func selKindStyle(kind string) lipgloss.Style {
	return selKindBase.Foreground(kindStyle(kind).GetForeground())
}

// ProjectColors gives each project a palette slot: the FNV-1a slot of the lower-cased name, the next
// free slot on a collision. Names are placed in sorted order, so the same set gives the same colors;
// colors are distinct while there are no more projects than palette entries.
func ProjectColors(names []string) map[string]int {
	uniq := map[string]bool{}
	for _, n := range names {
		uniq[strings.ToLower(n)] = true
	}
	taken := [len(projectPalette)]bool{}
	free := len(projectPalette)
	out := make(map[string]int, len(uniq))
	for _, n := range slices.Sorted(maps.Keys(uniq)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(n))
		i := int(h.Sum32() % uint32(len(projectPalette)))
		for free > 0 && taken[i] {
			i = (i + 1) % len(projectPalette)
		}
		if free > 0 {
			taken[i] = true
			free--
		}
		out[n] = i
	}
	return out
}

// projectStyle - the palette style of a slot; a slot outside the palette wraps around.
func projectStyle(slot int) lipgloss.Style {
	return projectPalette[(slot%len(projectPalette)+len(projectPalette))%len(projectPalette)]
}
