package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unitFocusURL = "warp://session/0123456789abcdef0123456789abcdef"

func unitFocusBox(t *testing.T, vars map[string]string) (*box, string) {
	t.Helper()
	b := newBox(t)
	dir := filepath.Join(t.TempDir(), "focus")
	vars["TIX_FOCUS_DIR"] = dir
	b.extra = vars
	return b, dir
}

// File 0600 with one line in the focus dir, not in the store and not in git.
func TestFocusUnit_AgentWrite_FileOutsideBase(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL})
	b.ok("", "new", "task")
	path := filepath.Join(dir, "claude-s1")
	data, err := os.ReadFile(path)
	info, serr := os.Stat(path)
	if err != nil || serr != nil || string(data) != unitFocusURL+"\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("focus file: %q, %v, %v; want one line and 0600", data, err, serr)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".tix", "focus")); err == nil {
		t.Error("focus created inside the store")
	}
	if st := b.git("status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(st) != "" {
		t.Errorf("git status not empty: %q", st)
	}
}

// A value of the wrong shape, a human and an unknown identity are not written.
func TestFocusUnit_NotWritten(t *testing.T) {
	cases := map[string]map[string]string{
		"foreign shape": {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": "warp://session/xyz"},
		"uppercase":     {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": strings.ToUpper(unitFocusURL)},
		"with tail":     {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL + "\nx"},
		"argument":      {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": "-a Calculator"},
		"human":         {"TIX_ACTOR": "human", "WARP_FOCUS_URL": unitFocusURL},
		"no identity":   {"WARP_FOCUS_URL": unitFocusURL},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			b, dir := unitFocusBox(t, vars)
			b.tix("", "new", "task")
			if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
				t.Errorf("written %v", entries)
			}
		})
	}
}

// The file name does not leave the focus dir.
func TestFocusUnit_ActorWithSlashes_StaysInDir(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"TIX_ACTOR": "x:../../evil", "WARP_FOCUS_URL": unitFocusURL})
	b.ok("", "new", "task")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || strings.ContainsAny(entries[0].Name(), "/\\") {
		t.Fatalf("focus dir = %v, %v; want one file without slashes", entries, err)
	}
}

// A file write error does not fail the command.
func TestFocusUnit_DirIsFile_CommandStillSucceeds(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL})
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := b.tix("", "new", "task"); r.code != 0 {
		t.Errorf("code %d, stderr %q", r.code, r.err)
	}
}
