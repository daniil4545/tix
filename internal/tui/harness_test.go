package tui_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

var gitBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tix-tui-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if gitBin, err = exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git not found")
		os.Exit(2)
	}
	os.Setenv("HOME", dir)
	os.Setenv("TIX_HOME", filepath.Join(dir, "never-used"))
	os.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_DIR", "GIT_WORK_TREE", "VISUAL"} {
		os.Unsetenv(k)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// probe wraps the model and keeps the last frame: the screen text without parsing the terminal stream.
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
	*p.screen = ansiRe.ReplaceAllString(v.Content, "")
	p.mu.Unlock()
	return v
}

type env struct {
	t      *testing.T
	root   string
	store  *core.Store
	tm     *teatest.TestModel
	mu     sync.Mutex
	screen string
}

// newEnv creates a store in a temp folder; config - the config.toml content, empty - default columns.
func newEnv(t *testing.T, config string) *env {
	t.Helper()
	root := filepath.Join(t.TempDir(), "tasks")
	if config != "" {
		if err := os.MkdirAll(filepath.Join(root, ".tix"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".tix", "config.toml"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &env{t: t, root: root, store: e0(t, root, 0)}
}

func e0(t *testing.T, root string, lock time.Duration) *core.Store {
	t.Helper()
	s, err := core.Open(root, core.Options{GitBin: gitBin, LockTimeout: lock})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e *env) create(title, project, column string) string {
	e.t.Helper()
	r, err := e.store.Create(title, project, column, "seed")
	if err != nil {
		e.t.Fatalf("Create(%q): %v", title, err)
	}
	return r.ID
}

// body sets the whole task body.
func (e *env) body(id, body string) {
	e.t.Helper()
	_, rev, err := e.store.Get(id)
	if err == nil {
		_, err = e.store.Edit(id, rev, body, "seed")
	}
	if err != nil {
		e.t.Fatalf("body %s: %v", id, err)
	}
}

func (e *env) start(project string, w, h int) {
	e.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	e.t.Cleanup(cancel)
	m := tui.New(ctx, tui.Deps{Store: e.store, Root: e.root, Actor: "human", Project: project})
	e.tm = teatest.NewTestModel(e.t, probe{Model: m, mu: &e.mu, screen: &e.screen}, teatest.WithInitialTermSize(w, h))
	e.t.Cleanup(func() { _ = e.tm.Quit() })
}

func (e *env) text() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.screen
}

func (e *env) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("did not wait for: %s\nscreen:\n%s", what, e.text())
}

func (e *env) screenHas(subs ...string) {
	e.t.Helper()
	e.until(fmt.Sprint("on screen ", subs), func() bool {
		s := e.text()
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				return false
			}
		}
		return true
	})
}

func (e *env) key(keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
		case "esc":
			e.tm.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
		default:
			for _, r := range k {
				e.tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
		}
	}
}

func (e *env) get(id string) (status string, waiting string, body string, comments []string) {
	e.t.Helper()
	tk, _, err := e.store.Get(id)
	if err != nil {
		e.t.Fatalf("Get %s: %v", id, err)
	}
	for _, c := range tk.Comments {
		comments = append(comments, c.Actor+": "+c.Text)
	}
	return tk.Status, tk.Waiting, tk.Body, comments
}

func (e *env) gitOut(args ...string) string {
	e.t.Helper()
	out, err := exec.CommandContext(context.Background(), gitBin, append([]string{"-C", e.root}, args...)...).CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
