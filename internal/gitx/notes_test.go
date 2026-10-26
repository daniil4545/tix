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
		t.Fatalf("Notes = %+v; want notes of T-1 and T-2", got)
	}
	if n := got["T-1"]; n.Text != "second | with %s" || !n.At.Equal(at) {
		t.Errorf("Notes[T-1] = %+v; want the newest 'second | with %%s' at 10:10", n)
	}
	if n := got["T-2"]; n.Text != "other task" {
		t.Errorf("Notes[T-2] = %+v; want 'other task'", n)
	}
}

func TestNotes_NoCommits_EmptyNoError(t *testing.T) {
	defer guard(t)
	got, err := newRepo(t).Notes()
	if err != nil || len(got) != 0 {
		t.Fatalf("Notes in an empty repository: %v, %v; want empty without an error", got, err)
	}
}

func TestTaskNotes_OnlyOwnNoteSubjects_OldToNew(t *testing.T) {
	r := newRepo(t)
	commitAt(t, r, "2026-10-01T10:00:00+0000", "T-1: note first", "claude:abc")
	commitAt(t, r, "2026-10-01T10:01:00+0000", "T-12: note twelve", "claude:abc")
	commitAt(t, r, "2026-10-01T10:02:00+0000", "T-1: move to doing", "claude:abc")
	commitAt(t, r, "2026-10-01T10:03:00+0000", "T-1: note second", "claude:abc")
	commitAt(t, r, "2026-10-01T10:04:00+0000", "T-4: external edit\n\nT-1: note in body", "external")

	got, err := r.TaskNotes("T-1", time.Time{})
	if err != nil {
		t.Fatalf("TaskNotes: %v", err)
	}
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" || got[0].Actor != "claude:abc" {
		t.Errorf("TaskNotes(T-1) = %+v; want [first second] old to new with the actor", got)
	}

	since := time.Date(2026, 10, 1, 10, 2, 0, 0, time.UTC)
	got, err = r.TaskNotes("T-1", since)
	if err != nil || len(got) != 1 || got[0].Text != "second" {
		t.Errorf("TaskNotes(T-1, since 10:02) = %+v, %v; want only [second]", got, err)
	}
}
