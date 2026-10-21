package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Migration: a store of the former version (.tracker, entries in .gitignore) gets .tix after the first Open,
// tasks and the journal stay in place, .gitignore is updated, the next write commits the edits.
func TestOpen_LegacyTrackerDir_MigratedToTix(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(0)
	r := mustCreate(t, s, "Legacy")
	// State of the former version: the service folder and .gitignore with the old name.
	if err := os.Rename(filepath.Join(e.root, ".tix"), filepath.Join(e.root, ".tracker")); err != nil {
		t.Fatal(err)
	}
	ign := filepath.Join(e.root, ".gitignore")
	data, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ign, []byte(strings.ReplaceAll(string(data), ".tix/", ".tracker/")), 0o600); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", "legacy layout")
	before := e.count()

	s = e.open(0)
	if _, err := os.Stat(filepath.Join(e.root, ".tix", "config.toml")); err != nil {
		t.Errorf("no .tix/config.toml after Open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".tracker")); !os.IsNotExist(err) {
		t.Errorf(".tracker remained after migration: %v", err)
	}
	got, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), ".tracker") || !strings.Contains(string(got), ".tix/lock\n") {
		t.Errorf(".gitignore after migration:\n%s", got)
	}
	if _, _, err := s.Get(r.ID); err != nil {
		t.Errorf("task %s after migration: %v", r.ID, err)
	}
	if e.count() != before {
		t.Errorf("Open created a commit: was %d, now %d", before, e.count())
	}
	if _, err := s.Comment(r.ID, "after", "tester"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if out := e.git("status", "--porcelain"); out != "" {
		t.Errorf("migration edits not committed:\n%s", out)
	}
	if e.count() <= before {
		t.Errorf("a write after migration did not create a commit")
	}
}

// If .tix exists and .tracker remained, Open touches nothing: data is not overwritten.
func TestOpen_BothDirs_LegacyUntouched(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(0)
	mustCreate(t, s, "Both")
	legacy := filepath.Join(e.root, ".tracker")
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	e.open(0)
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf(".tracker removed while .tix exists: %v", err)
	}
}

// legacyBase prepares a store of the former version: the service folder .tracker with a journal, .gitignore
// with text ign (nil - no file), everything committed.
func legacyBase(t *testing.T, e *env, ign *string) {
	t.Helper()
	mustCreate(t, e.open(0), "Legacy")
	if err := os.Rename(filepath.Join(e.root, ".tix"), filepath.Join(e.root, ".tracker")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, ".tracker", "log"), []byte("old log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.root, ".gitignore")
	if ign == nil {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(path, []byte(*ign), 0o600); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", "legacy layout")
}

// Any variant of the former-version .gitignore: after migration the .tix service files do not
// get into git, the journal stays, a repeated Open does not change .gitignore.
func TestOpen_LegacyGitignoreVariants_TixFilesIgnored(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		ign  *string
	}{
		{"partial", str(".tracker/lock\n*.swp\n")},
		{"rooted", str("/.tracker/\n")},
		{"bare dir", str(".tracker\n")},
		{"no entry", str("*.swp\n")},
		{"no newline at end", str("*.swp")},
		{"no file", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			e := newEnv(t)
			legacyBase(t, e, c.ign)
			s := e.open(0)
			ign := filepath.Join(e.root, ".gitignore")
			first, _ := os.ReadFile(ign)
			e.open(0)
			if again, _ := os.ReadFile(ign); string(again) != string(first) {
				t.Errorf("repeated Open changed .gitignore:\n%s\nnow:\n%s", first, again)
			}
			if got, err := os.ReadFile(filepath.Join(e.root, ".tix", "log")); err != nil || !strings.HasPrefix(string(got), "old log\n") {
				t.Errorf("journal after migration: %q, %v", got, err)
			}
			r := mustCreate(t, s, "After")
			if _, err := s.Comment(r.ID, "one", "tester"); err != nil {
				t.Fatalf("Comment: %v", err)
			}
			if out := e.git("ls-files", ".tix/lock", ".tix/log", ".tix/tmp"); out != "" {
				t.Errorf(".tix service files in git:\n%s\n.gitignore:\n%s", out, first)
			}
			if out := e.git("status", "--porcelain"); out != "" {
				t.Errorf("dirty after the write:\n%s", out)
			}
		})
	}
}

// Both folders (an old binary recreated .tracker): .tracker is not touched, and .gitignore
// gets the missing .tix lines.
func TestOpen_BothDirs_GitignoreGetsTixLines(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	mustCreate(t, e.open(0), "Both")
	if err := os.MkdirAll(filepath.Join(e.root, ".tracker"), 0o750); err != nil {
		t.Fatal(err)
	}
	ign := filepath.Join(e.root, ".gitignore")
	if err := os.WriteFile(ign, []byte(".tracker/lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.open(0)
	got, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), ".tracker/lock\n") || !strings.Contains(string(got), ".tix/lock\n") {
		t.Errorf(".gitignore with two folders:\n%s", got)
	}
}
