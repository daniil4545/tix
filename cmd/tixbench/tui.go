package main

import (
	"fmt"
	"strings"
)

// tuiOneshotEnv: tix without arguments draws the first board frame to stdout and exits (a measurement without a terminal).
const tuiOneshotEnv = "TIX_TUI_ONESHOT=1"

// tuiBenches - the board open measurement: the goal is up to 100 ms on 1000 tasks.
func tuiBenches() []bench {
	return []bench{{
		name: "tui open", targetMS: 100, env: []string{tuiOneshotEnv},
		check: func(_ int, out string) error {
			if !strings.Contains(out, "todo") || !strings.Contains(out, "T-") {
				return fmt.Errorf("first frame has no board: %.80q", out)
			}
			return nil
		},
	}}
}
