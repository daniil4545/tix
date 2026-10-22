package view

import (
	"hash/fnv"
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
		lipgloss.NewStyle().Foreground(lipgloss.Color("#8B9DC3")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#A98FBF")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#C08F9A")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#B5A27A")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#B58F6E")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#86A9B3")),
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

// projectStyle - the palette style of a project: FNV-1a of the lower-cased name.
func projectStyle(name string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return projectPalette[h.Sum32()%uint32(len(projectPalette))]
}
