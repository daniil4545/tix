package gitx_test

import (
	"testing"
	"time"
)

func TestNotes_NewestPerTask_OnlyNoteSubjects(t *testing.T) {
	defer guard(t)
	r := newRepo(t)
	commitAt(t, r, "2026-10-01T10:00:00+0000", "T-1: note first", "claude:abc")
	commitAt(t, r, "2026-10-01T10:05:00+0000", "T-2: note other task", "claude:abc")
	commitAt(t, r, "2026-10-01T10:10:00+0000", "T-1: note second | with %s", "claude:abc")
	commitAt(t, r, "2026-10-01T10:15:00+0000", "T-1: move todo > doing", "")
	commitAt(t, r, "2026-10-01T10:16:00+0000", "T-3: noted but not a note", "")
	commitAt(t, r, "2026-10-01T10:17:00+0000", "T-4: external edit\n\nT-5: note hidden in body", "external")

	got, err := r.Notes()
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	at, _ := time.Parse(time.RFC3339, "2026-10-01T10:10:00Z")
	if len(got) != 2 {
		t.Fatalf("Notes = %+v; ждали заметки T-1 и T-2", got)
	}
	if n := got["T-1"]; n.Text != "second | with %s" || !n.At.Equal(at) {
		t.Errorf("Notes[T-1] = %+v; ждали новейшую 'second | with %%s' в 10:10", n)
	}
	if n := got["T-2"]; n.Text != "other task" {
		t.Errorf("Notes[T-2] = %+v; ждали 'other task'", n)
	}
}

func TestNotes_NoCommits_EmptyNoError(t *testing.T) {
	defer guard(t)
	got, err := newRepo(t).Notes()
	if err != nil || len(got) != 0 {
		t.Fatalf("Notes в пустом репозитории: %v, %v; ждали пусто без ошибки", got, err)
	}
}
