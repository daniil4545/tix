package view_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/tui/view"
)

// starParams returns the color parameters of the escape sequence right before the first "*".
func starParams(t *testing.T, line string) string {
	t.Helper()
	i := strings.Index(line, "*")
	if i < 0 {
		t.Fatalf("no star in %q", plain(line))
	}
	j := strings.LastIndex(line[:i], "\x1b[")
	if j < 0 {
		t.Fatalf("star without color in %q", line)
	}
	seq := line[j+len("\x1b[") : i]
	return strings.TrimSuffix(seq, "m")
}

func hasParam(params, want string) bool {
	return params == want || strings.HasPrefix(params, want+";") || strings.HasSuffix(params, ";"+want) ||
		strings.Contains(params, ";"+want+";")
}

// spec: T-44 scenario 3 (pulse line has no prefix)
func TestRenderBoard_AskKind_PulseLineWithoutPrefix(t *testing.T) {
	for _, ask := range []string{"review: look at it", "access: look at it", "decide: look at it", "look at it"} {
		t.Run(ask, func(t *testing.T) {
			b := view.Board{Total: 1, W: 120, H: 40, Cols: []view.Col{oneCol(view.Row{ID: "T-7", Title: "Task", Ask: ask, Live: true})}}
			raw := plain(view.RenderBoard(b))
			if !strings.Contains(raw, "? look at it") {
				t.Errorf("no pulse line %q in:\n%s", "? look at it", raw)
			}
			for _, kind := range []string{"review:", "access:", "decide:"} {
				if strings.Contains(raw, kind) {
					t.Errorf("pulse line keeps the prefix %q:\n%s", kind, raw)
				}
			}
		})
	}
}
