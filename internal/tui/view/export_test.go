package view

import "charm.land/lipgloss/v2"

// Test access to the scheme table: tests compare with its values, not with hex codes.
type CardState = cardState

const (
	StateIdle    = stateIdle
	StateWorking = stateWorking
	StateWaiting = stateWaiting
)

func StateOf(r Row) CardState { return stateOf(r) }

// sgr returns the escape prefix a style puts before text.
func sgr(st lipgloss.Style) string {
	out := st.Render("x")
	for i := range out {
		if out[i] == 'x' {
			return out[:i]
		}
	}
	return ""
}

func TitleSGR(s CardState) string    { return sgr(scheme[s].Title) }
func RestSGR(s CardState) string     { return sgr(scheme[s].Rest) }
func SignOf(s CardState) string      { return scheme[s].Sign }
func KindStarOf(s CardState) bool    { return scheme[s].KindColor }
func KindSGR(kind string) string     { return sgr(kindStyle(kind)) }
func OverdueSGR() string             { return sgr(overdueStyle) }
func ProjectPaletteSGR(i int) string { return sgr(projectPalette[i]) }
func ProjectPaletteSize() int        { return len(projectPalette) }
