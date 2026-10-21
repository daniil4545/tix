package e2e

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/tui"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// TestTUI_OneShot_FirstFrameInPipe: tix without arguments, stdout is not a terminal, TIX_TUI_ONESHOT=1:
// the binary opens the store, draws the board into the pipe and exits with code 0.
func TestTUI_OneShot_FirstFrameInPipe(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Frame task", "--to", "idea")
	r := b.runEnv(b.with("TIX_TUI_ONESHOT=1"), "")
	out := ansi.ReplaceAllString(r.out, "")
	if r.code != 0 || !strings.Contains(out, "idea") || !strings.Contains(out, "Frame task") {
		t.Fatalf("code %d, stderr %q, frame:\n%s", r.code, r.err, out)
	}
}

// TestTUI_NoTerminal_FallsBackToCLI: without ONESHOT and without a terminal tix works as before (cli.Run).
func TestTUI_NoTerminal_FallsBackToCLI(t *testing.T) {
	b := newBox(t)
	r := b.tix("")
	if strings.Contains(r.out, "\x1b[?1049h") {
		t.Errorf("alternate screen opened without a terminal: %q", r.out)
	}
	if r.code == 0 || !strings.Contains(r.err, "command required") {
		t.Errorf("code %d, stderr %q; want the cli.Run answer about a missing command", r.code, r.err)
	}
}

type probe struct {
	tea.Model
	mu     *sync.Mutex
	screen *string
}

func (p probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := p.Model.Update(msg)
	p.Model = m
	return p, cmd
}

func (p probe) View() tea.View {
	v := p.Model.View()
	p.mu.Lock()
	*p.screen = ansi.ReplaceAllString(v.Content, "")
	p.mu.Unlock()
	return v
}

// TestTUI_LiveUpdate_AgentWritesThroughTix: the board is open, the agent writes with a real tix process.
func TestTUI_LiveUpdate_AgentWritesThroughTix(t *testing.T) {
	b := newBox(t)
	t.Setenv("HOME", b.root)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	b.ok("", "new", "First", "--to", "idea")
	b.ok("", "new", "Second", "--to", "idea")
	store, err := core.Open(b.home, core.Options{GitBin: gitBin})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var screen string
	m := tui.New(ctx, tui.Deps{Store: store, Root: b.home, Actor: "human"})
	tm := teatest.NewTestModel(t, probe{Model: m, mu: &mu, screen: &screen}, teatest.WithInitialTermSize(140, 40))
	defer func() { _ = tm.Quit() }()
	text := func() string { mu.Lock(); defer mu.Unlock(); return screen }
	until := func(what string, cond func(s string) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond(text()) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting: %s\n%s", what, text())
	}
	has := func(sub string) func(string) bool { return func(s string) bool { return strings.Contains(s, sub) } }
	until("first frame", has("Second"))

	// Second is selected; the agent adds a task above it: the selection stays on Second by id.
	tm.Send(tea.KeyPressMsg{Code: 'j', Text: "j"})
	b.ok("", "new", "From agent", "--to", "idea")
	b.ok("", "move", "T-3", "idea", "--first")
	until("new task on the board", has("From agent"))
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	until("Second card", func(s string) bool { return strings.Contains(s, "Second") && !strings.Contains(s, "From agent") })

	// The agent deletes the open task: back to the board and a message.
	b.ok("", "rm", "T-2")
	until("T-2 removed", has("T-2 removed"))
	until("board without Second", func(s string) bool { return strings.Contains(s, "First") && !strings.Contains(s, "Second") })
}
