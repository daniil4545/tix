package project_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tracker/internal/project"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	return dir
}

func commit(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "c")
}

// Code: <проект>@<sha> берёт HEAD без запуска git (docs/api/git.md, 7).
func TestHead_ReadsFullShaWithoutGit(t *testing.T) {
	defer guard(t)
	dir := newProject(t)
	commit(t, dir)
	want := gitRun(t, dir, "rev-parse", "HEAD")
	if got, ok := project.Head(dir); !ok || got != want {
		t.Errorf("Head (loose ref) = %q, %v; ждали %q", got, ok, want)
	}

	gitRun(t, dir, "pack-refs", "--all")
	if got, ok := project.Head(dir); !ok || got != want {
		t.Errorf("Head (packed-refs) = %q, %v; ждали %q", got, ok, want)
	}

	gitRun(t, dir, "checkout", "-q", "--detach")
	if got, ok := project.Head(dir); !ok || got != want {
		t.Errorf("Head (detached) = %q, %v; ждали %q", got, ok, want)
	}

	commit(t, dir)
	if got, ok := project.Head(dir); !ok || got == want || got != gitRun(t, dir, "rev-parse", "HEAD") {
		t.Errorf("Head после второго коммита = %q, %v; ждали новый HEAD", got, ok)
	}
}

func TestHead_NoCommitsNoRepoReftable_False(t *testing.T) {
	defer guard(t)
	if got, ok := project.Head(newProject(t)); ok {
		t.Errorf("Head без коммитов = %q, true; ждали false", got)
	}
	if got, ok := project.Head(t.TempDir()); ok {
		t.Errorf("Head вне git = %q, true; ждали false", got)
	}
	// reftable: HEAD - заглушка, ссылки лежат вне .git/refs; читать их без git нельзя.
	dir := t.TempDir()
	for _, d := range []string{filepath.Join(dir, ".git", "reftable")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/.invalid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := project.Head(dir); ok {
		t.Errorf("Head в reftable = %q, true; ждали false", got)
	}
}
