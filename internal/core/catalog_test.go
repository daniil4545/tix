package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
)

func TestStore_SetProjectPath(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	r, err := s.SetProjectPath("Alpha", "/work/alpha", "tester")
	if err != nil || r.Commit == "" {
		t.Fatalf("first set: %+v, %v", r, err)
	}
	if s.Config().Paths["alpha"] != "/work/alpha" {
		t.Errorf("paths %v, want lower-case name", s.Config().Paths)
	}
	n := e.count()
	r, err = s.SetProjectPath("alpha", "/work/alpha", "tester")
	if err != nil || r.Commit != "" || e.count() != n {
		t.Errorf("repeat: %+v, %v, commits %d -> %d; want ok with no commit", r, err, n, e.count())
	}
	_, err = s.SetProjectPath("alpha", "/other", "tester")
	if !errors.Is(err, core.ErrRule) {
		t.Errorf("other path: %v, want ErrRule", err)
	}
	if _, err = s.SetProjectPath("beta", "", "tester"); err != nil {
		t.Errorf("no path: %v", err)
	}
	if _, err = s.SetProjectPath("beta", "/work/beta", "tester"); err != nil {
		t.Errorf("path for a pathless project: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(e.root, ".tix", "config.toml"))
	if !strings.Contains(string(data), "alpha") || !strings.Contains(string(data), "/work/beta") {
		t.Errorf("config.toml:\n%s", data)
	}
	if out := e.git("status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("uncommitted: %s", out)
	}
	s2 := e.open(2 * time.Second)
	if got := s2.Catalog(); len(got) != 2 || got[0].Name != "alpha" || got[1].Path != "/work/beta" {
		t.Errorf("catalog %+v", got)
	}
}

func TestStore_SetProjectPath_KeepsForeignSections(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	if _, err := s.Create("t", "p", "idea", "tester"); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(e.root, ".tix", "config.toml")
	if err := os.WriteFile(cfg, []byte("[projects]\nd = \"x\"\n\n[adapter]\nkind = \"y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s = e.open(2 * time.Second)
	if _, err := s.SetProjectPath("a", "/a", "tester"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg)
	for _, want := range []string{"[projects]", "[paths]", "[adapter]", `kind = "y"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%s lost:\n%s", want, data)
		}
	}
}
