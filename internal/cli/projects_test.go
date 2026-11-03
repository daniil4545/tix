package cli_test

import (
	"os"
	"strings"
	"testing"
)

func TestProjectsAdd(t *testing.T) {
	b := newBox(t)
	dir := t.TempDir()
	b.ok("", "projects", "add", "tarn", dir)
	if r := b.ok("", "projects", "add", "tarn", dir); !strings.HasPrefix(r.out, "ok") {
		t.Errorf("repeat: %q", r.out)
	}
	cfg, _ := os.ReadFile(b.home + "/.tix/config.toml")
	if !strings.Contains(string(cfg), dir) {
		t.Errorf("config: %s", cfg)
	}
	for _, path := range []string{"rel/dir", dir + "/missing"} {
		if r := b.tix("", "projects", "add", "x", path); r.code != 6 {
			t.Errorf("path %q: code %d, %q", path, r.code, r.err)
		}
	}
}

func TestProjectsList(t *testing.T) {
	b := newBox(t)
	b.ok("", "projects", "add", "call-transcription")
	b.newTaskIn(b.cwd, "Work", "idea", "--project", "ghost")
	out := b.ok("", "projects").out
	if !strings.Contains(out, "call-transcription  -  0") {
		t.Errorf("no row for call-transcription:\n%s", out)
	}
	if !strings.Contains(out, "ghost") || !strings.Contains(out, "not in catalog") {
		t.Errorf("no ghost row:\n%s", out)
	}
	if strings.Contains(strings.SplitN(out, "\n", 2)[0], "not in catalog") {
		t.Errorf("known project marked:\n%s", out)
	}
}
