//go:build race

package tui

// raceOn: the race detector slows the code several times, timing tests skip under it.
const raceOn = true
