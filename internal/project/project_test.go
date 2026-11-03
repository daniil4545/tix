package project_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/project"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

func mk(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetect(t *testing.T) {
	defer guard(t)
	base := t.TempDir()
	root := filepath.Join(base, "tasks")
	proj := filepath.Join(base, "dev", "myproj")
	mk(t, filepath.Join(root, ".git"), filepath.Join(root, "sub"), filepath.Join(proj, ".git"),
		filepath.Join(proj, "pkg", "deep"), filepath.Join(base, "plain", "dir"),
		filepath.Join(base, "dev", "main"))
	// a real worktree: .git is a file with a pointer, commondir next to it
	gitEnv := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	for _, args := range [][]string{
		{"-C", filepath.Join(base, "dev", "main"), "init", "-q"},
		{"-C", filepath.Join(base, "dev", "main"), "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "i"},
		{"-C", filepath.Join(base, "dev", "main"), "worktree", "add", "-q", filepath.Join(base, "dev", "wt")},
	} {
		c := exec.CommandContext(context.Background(), "git", args...)
		c.Env = gitEnv
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	mk(t, filepath.Join(base, "dev", "wt", "src"))
	mapped := &config.Config{Projects: map[string]string{"myproj": "трекер"}}
	cases := []struct {
		name string
		cwd  string
		cfg  *config.Config
		want string
		ok   bool
	}{
		{"repository root", proj, config.Default(), "myproj", true},
		{"deep folder", filepath.Join(proj, "pkg", "deep"), config.Default(), "myproj", true},
		{"name map", filepath.Join(proj, "pkg"), mapped, "трекер", true},
		{"worktree gives the main folder", filepath.Join(base, "dev", "wt", "src"), config.Default(), "main", true},
		{"root itself", root, config.Default(), "", false},
		{"inside root, though .git is there", filepath.Join(root, "sub"), config.Default(), "", false},
		{"outside git", filepath.Join(base, "plain", "dir"), config.Default(), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			got, ok := project.Detect(c.cwd, root, c.cfg)
			if got != c.want || ok != c.ok {
				t.Errorf("Detect(%s) = %q, %v; want %q, %v", c.cwd, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestDetect_Catalog(t *testing.T) {
	defer guard(t)
	base := t.TempDir()
	root := filepath.Join(base, "tasks")
	outer := filepath.Join(base, "work", "outer")
	inner := filepath.Join(outer, "inner")
	link := filepath.Join(base, "link")
	mk(t, root, filepath.Join(inner, "pkg"), filepath.Join(outer, ".worktrees", "wt", "src"),
		filepath.Join(base, "work", "other"), filepath.Join(outer, ".git"))
	if err := os.Symlink(outer, link); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Paths = map[string]string{"outer": outer, "inner": inner, "linked": filepath.Join(base, "work", "other"), "nopath": ""}
	cases := []struct {
		name, cwd, want string
		ok              bool
	}{
		{"subfolder", filepath.Join(outer, ".worktrees", "wt", "src"), "outer", true},
		{"longest path wins", filepath.Join(inner, "pkg"), "inner", true},
		{"the path itself", outer, "outer", true},
		{"through a symlink", filepath.Join(link, "x"), "outer", true},
		{"git fallback with no match", filepath.Join(base, "work"), "", false},
	}
	mk(t, filepath.Join(link, "x"))
	for _, c := range cases {
		got, ok := project.Detect(c.cwd, root, cfg)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Detect(%s) = %q, %v; want %q, %v", c.name, c.cwd, got, ok, c.want, c.ok)
		}
	}
	// a registered path given through a symlink matches the real cwd
	cfg.Paths = map[string]string{"viaLink": link}
	if got, _ := project.Detect(filepath.Join(outer, "x"), root, cfg); got != "viaLink" {
		t.Errorf("symlinked registration: %q", got)
	}
}
