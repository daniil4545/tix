package gitx_test

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.CommandContext(context.Background(), gitBin, "init", "--bare", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("init --bare: %v\n%s", err, out)
	}
	return dir
}

func branch(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), gitBin, "-C", dir, "symbolic-ref", "--short", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("symbolic-ref: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func bareHead(t *testing.T, bare, br string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), gitBin, "-C", bare, "rev-parse", "refs/heads/"+br).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func TestRemotes_ListsNames(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	got, err := r.Remotes()
	if err != nil || len(got) != 0 {
		t.Fatalf("Remotes in a clean repository = %v, %v; want empty", got, err)
	}
	raw(t, r, "remote", "add", "origin", newBare(t))
	got, err = r.Remotes()
	if err != nil || !reflect.DeepEqual(got, []string{"origin"}) {
		t.Errorf("Remotes = %v, %v; want [origin]", got, err)
	}
}

func TestPush_SendsHEAD_NoForceOnDiverged(t *testing.T) {
	defer guard(t)
	bare := newBare(t)
	r := newRepo(t)
	raw(t, r, "remote", "add", "origin", bare)
	put(t, r, "a.md", "one\n")
	if _, err := r.Commit([]string{"a.md"}, "first", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), "origin"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	br := branch(t, r.Dir)
	local := strings.TrimSpace(raw(t, r, "rev-parse", "HEAD"))
	if got := bareHead(t, bare, br); got != local {
		t.Fatalf("after Push branch %s in bare = %q, want HEAD %s", br, got, local)
	}

	// A foreign commit in the remote: our Push is not a fast-forward and must not overwrite it (no --force).
	other := newRepo(t)
	raw(t, other, "remote", "add", "origin", bare)
	put(t, other, "b.md", "other\n")
	if _, err := other.Commit([]string{"b.md"}, "other", "tester"); err != nil {
		t.Fatal(err)
	}
	raw(t, other, "push", "--force", "origin", "HEAD")
	theirs := bareHead(t, bare, br)
	put(t, r, "a.md", "two\n")
	if _, err := r.Commit([]string{"a.md"}, "second", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(context.Background(), "origin"); err == nil {
		t.Error("Push over a foreign commit passed: a refusal without --force is needed")
	}
	if got := bareHead(t, bare, br); got != theirs {
		t.Errorf("Push overwrote the remote: branch %s = %q, want %q", br, got, theirs)
	}
}

func TestPush_UnreachableRemote_ErrorNotHang(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	put(t, r, "a.md", "one\n")
	if _, err := r.Commit([]string{"a.md"}, "first", "tester"); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{r.Dir + "/no/such/remote.git", "http://user:secret@127.0.0.1:1/r.git"} {
		raw(t, r, "remote", "add", "bad", url)
		done := make(chan error, 1)
		go func() { done <- r.Push(context.Background(), "bad") }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("Push to %s passed", url)
			}
		case <-time.After(45 * time.Second):
			t.Fatalf("Push to %s hung: a password prompt or no timeout", url)
		}
		raw(t, r, "remote", "remove", "bad")
	}
}
