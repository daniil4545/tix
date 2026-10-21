package cli_test

// Focus of the agent session: file write. Written from the spec, without reading the code of zone H.
// The environment is passed through box.extra.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const warpURL = "warp://session/0123456789abcdef0123456789abcdef"

func (b *box) focusFiles() []string {
	b.t.Helper()
	files, err := filepath.Glob(filepath.Join(b.focusDir(), "*"))
	if err != nil {
		b.t.Fatal(err)
	}
	return files
}

// focusDir - the focus dir outside the store (TIX_FOCUS_DIR overrides os.UserCacheDir()/tix/focus).
func (b *box) focusDir() string { return filepath.Join(b.root, "focus") }

// agentEnv - the environment of Claude session "c1" with tab address url (empty - no variable).
func (b *box) agentEnv(url string) map[string]string {
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_FOCUS_DIR": b.focusDir()}
	if url != "" {
		env["WARP_FOCUS_URL"] = url
	}
	return env
}

// Any agent write command writes the address to the focus dir, file <agent>-<session id>, 0600, one line.
func TestFocus_AgentWriteCommands_StoreURL(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"new", []string{"new", "Second", "--to", "todo"}},
		{"note", []string{"note", "T-1", "progress"}},
		{"comment", []string{"comment", "T-1", "remark"}},
		{"take", []string{"take", "T-1"}},
		{"set", []string{"set", "T-1", "due=2026-11-01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.ok("", "new", "First", "--to", "todo")
			b.extra = b.agentEnv(warpURL)
			b.ok("", c.args...)
			files := b.focusFiles()
			if len(files) != 1 {
				t.Fatalf("focus files %d (%v), want one", len(files), files)
			}
			if name := filepath.Base(files[0]); name != "claude-c1" {
				t.Errorf("file name %q, want claude-c1 (<agent>-<session id>)", name)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimRight(string(data), "\n"); got != warpURL || strings.Contains(got, "\n") {
				t.Errorf("content %q, want one line %q", data, warpURL)
			}
			info, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("permissions %v, want 0600", info.Mode().Perm())
			}
		})
	}
}

// A value not of the form ^warp://session/[0-9a-f]{32}$ is not written; the command still passes.
func TestFocus_ValueOfOtherShape_NotWritten(t *testing.T) {
	hex32 := "0123456789abcdef0123456789abcdef"
	cases := []struct{ name, url string }{
		{"not warp", "https://example.com/session/" + hex32},
		{"non-hex letters", "warp://session/zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"uppercase", "warp://session/0123456789ABCDEF0123456789ABCDEF"},
		{"31 chars", "warp://session/" + hex32[:31]},
		{"33 chars", "warp://session/" + hex32 + "a"},
		{"tail after the address", warpURL + "/extra"},
		{"trailing newline", warpURL + "\n"},
		{"second line", warpURL + "\nwarp://session/" + hex32},
		{"leading space", " " + warpURL},
		{"scheme of another terminal", "ghostty://session/" + hex32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.extra = b.agentEnv(c.url)
			b.ok("", "new", "Work", "--to", "todo")
			if files := b.focusFiles(); len(files) != 0 {
				t.Errorf("value %q written to %v", c.url, files)
			}
		})
	}
}

// A human (identity human) writes no file; without WARP_FOCUS_URL there is no file.
func TestFocus_HumanOrNoURL_NotWritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.extra = b.agentEnv(warpURL)
	b.ok("", "new", "By human", "--to", "todo", "--as", "human")
	if files := b.focusFiles(); len(files) != 0 {
		t.Errorf("a human write left a focus file: %v", files)
	}

	b2 := newBox(t)
	b2.extra = b2.agentEnv("")
	b2.ok("", "new", "No url", "--to", "todo")
	if files := b2.focusFiles(); len(files) != 0 {
		t.Errorf("a file appeared without WARP_FOCUS_URL: %v", files)
	}
}

// A file write error does not fail the command: the store write passed as exactly one commit, code 0.
func TestFocus_FileWriteFails_CommandStillSucceeds(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "First", "--to", "todo")
	// a regular file sits in place of the focus dir: mkdir and write are impossible
	if err := os.WriteFile(b.focusDir(), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := b.commitCount()
	b.extra = b.agentEnv(warpURL)
	r := b.tix("", "note", "T-1", "still works")
	if r.code != 0 {
		t.Fatalf("note on focus write failure: code %d, stderr %q, want 0", r.code, r.err)
	}
	if after := b.commitCount(); after != before+1 {
		t.Errorf("commits %d -> %d, want exactly one (the note write)", before, after)
	}
}

// The tab address is bound to the machine: the focus file lies outside the store and does not go to git
// (neither as tracked nor as an external edit of the next operation).
func TestFocus_FileNeverCommittedToGit(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.extra = b.agentEnv(warpURL)
	b.ok("", "new", "First", "--to", "todo")
	if len(b.focusFiles()) != 1 {
		t.Fatalf("focus file not written: %v", b.focusFiles())
	}
	b.extra = nil
	b.ok("", "new", "Second", "--to", "todo") // the next operation picks up foreign tree edits
	if tracked := b.git("ls-files"); strings.Contains(tracked, "focus") {
		t.Errorf("focus file got into git: %q", tracked)
	}
	if st := b.git("status", "--porcelain"); strings.Contains(st, "focus") {
		t.Errorf("focus file hangs in git status: %q", st)
	}
}
