package view

import (
	"hash/fnv"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// The board color scheme. Change the scheme here: card states, colors
// and the project palette. The board code only looks these values up.

// cardState - what a card tells the human: an agent works or nobody is on it.
type cardState int

const (
	stateIdle    cardState = iota // no live claim: the whole card is grey
	stateWorking                  // live claim
)

// cardLook - how a state is drawn. Head is the ID line, Title the title, Rest the meta and note lines.
type cardLook struct {
	Head, Title, Rest lipgloss.Style
}

var (
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6FCF7A"))

	scheme = [...]cardLook{
		stateIdle:    {Head: dimStyle, Title: dimStyle, Rest: dimStyle},
		stateWorking: {Head: lipgloss.NewStyle(), Title: greenStyle, Rest: dimStyle},
	}

	// projectPalette - muted colors of the project label; the name picks one by hash.
	projectPalette = [6]lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#82AAFF")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#C792EA")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#F78C6C")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#89DDFF")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9CAC")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#C3A57A")),
	}
)

// stateOf picks the state of a card: a live claim.
func stateOf(row Row) cardState {
	if !row.Live {
		return stateIdle
	}
	return stateWorking
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
