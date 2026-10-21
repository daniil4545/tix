package main

import (
	"fmt"
	"strings"
)

// tuiOneshotEnv: tix без аргументов рисует первый кадр доски в stdout и выходит (замер без терминала).
const tuiOneshotEnv = "TIX_TUI_ONESHOT=1"

// tuiBenches - замер открытия доски: цель раздела 7 плана З4, до 100 мс на 1000 задач.
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
