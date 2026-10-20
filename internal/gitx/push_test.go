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
		t.Fatalf("Remotes в чистом репозитории = %v, %v; ждали пусто", got, err)
	}
	raw(t, r, "remote", "add", "origin", newBare(t))
	got, err = r.Remotes()
	if err != nil || !reflect.DeepEqual(got, []string{"origin"}) {
		t.Errorf("Remotes = %v, %v; ждали [origin]", got, err)
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
		t.Fatalf("после Push ветка %s в bare = %q, ждали HEAD %s", br, got, local)
	}

	// Чужой коммит в remote: наш Push не fast-forward и не должен перезаписать (без --force).
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
		t.Error("Push поверх чужого коммита прошёл: нужен отказ без --force")
	}
	if got := bareHead(t, bare, br); got != theirs {
		t.Errorf("Push затёр remote: ветка %s = %q, ждали %q", br, got, theirs)
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
				t.Errorf("Push в %s прошёл", url)
			}
		case <-time.After(45 * time.Second):
			t.Fatalf("Push в %s завис: запрос пароля или нет таймаута", url)
		}
		raw(t, r, "remote", "remove", "bad")
	}
}
