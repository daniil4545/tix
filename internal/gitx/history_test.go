package gitx_test

import (
	"regexp"
	"testing"
	"time"

	"tracker/internal/gitx"
)

var hashRe = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

// commitAt делает пустой коммит с заданным временем автора; трейлер Actor ставит Commit.
func commitAt(t *testing.T, r gitx.Repo, at, subject, actor string, trailers ...gitx.Trailer) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_DATE", at)
	t.Setenv("GIT_COMMITTER_DATE", at)
	h, err := r.Commit(nil, subject, actor, trailers...)
	if err != nil {
		t.Fatalf("Commit(%q): %v", subject, err)
	}
	return h
}

func TestLog_NoCommits_EmptyNoError(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	got, err := r.Log("T-1")
	if err != nil || len(got) != 0 {
		t.Fatalf("Log в пустом репозитории: %v, %v; ждали пусто без ошибки", got, err)
	}
}

func TestLog_OnlyThisTask_NewestFirst_AllFields(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	commitAt(t, r, "2026-10-01T10:00:00+0000", "T-1: new", "claude:abc")
	commitAt(t, r, "2026-10-01T10:05:00+0000", "T-12: new", "claude:abc")
	commitAt(t, r, "2026-10-01T10:10:00+0000", "T-1: note a b | c %s", "codex:def",
		gitx.Trailer{Key: "Code", Value: "x1"})
	commitAt(t, r, "2026-10-01T10:15:00+0000", "T-10: take", "claude:abc")
	last := commitAt(t, r, "2026-10-01T10:20:00+0000", "T-1: move todo > doing", "")

	got, err := r.Log("T-1")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Log(T-1): %d коммитов %+v; ждали 3 (T-12 и T-10 чужие)", len(got), got)
	}
	want := []struct{ subject, actor, at string }{
		{"T-1: move todo > doing", "", "2026-10-01T10:20:00Z"},
		{"T-1: note a b | c %s", "codex:def", "2026-10-01T10:10:00Z"},
		{"T-1: new", "claude:abc", "2026-10-01T10:00:00Z"},
	}
	for i, w := range want {
		c := got[i]
		at, _ := time.Parse(time.RFC3339, w.at)
		if c.Subject != w.subject || c.Actor != w.actor || !c.At.Equal(at) || !hashRe.MatchString(c.Hash) {
			t.Errorf("Log[%d] = %+v; ждали subject %q actor %q at %s и хеш", i, c, w.subject, w.actor, w.at)
		}
	}
	if got[0].Hash != last {
		t.Errorf("Log[0].Hash = %q, хеш последнего коммита %q", got[0].Hash, last)
	}
}

func TestLog_ExternalEditCommitWithSeveralIDs_FoundByEach(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	h := commitAt(t, r, "2026-10-01T11:00:00+0000",
		"T-2: external edit\n\nT-13: external edit", "external")
	for _, id := range []string{"T-2", "T-13"} {
		got, err := r.Log(id)
		if err != nil || len(got) != 1 || got[0].Hash != h || got[0].Actor != "external" {
			t.Errorf("Log(%s) = %+v, %v; ждали один внешний коммит %s", id, got, err, h)
		}
	}
	if got, _ := r.Log("T-3"); len(got) != 0 {
		t.Errorf("Log(T-3) = %+v; в коммите такого id нет", got)
	}
}
