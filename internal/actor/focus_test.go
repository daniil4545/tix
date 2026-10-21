package actor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/actor"
)

func TestValidFocusURL(t *testing.T) {
	good := "warp://session/0123456789abcdef0123456789abcdef"
	if !actor.ValidFocusURL(good) {
		t.Errorf("%q rejected", good)
	}
	for _, bad := range []string{"", good + "\n", good + "x", " " + good, "warp://session/ABCDEF0123456789abcdef0123456789",
		"warp://session/0123", "-a Calculator", "https://example.com"} {
		if actor.ValidFocusURL(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFocusFile(t *testing.T) {
	cases := map[string]string{
		"claude:8f3a": "claude-8f3a", "human": "", "unknown": "", "": "",
		"x:../../evil": "x-.._.._evil", "..": "",
	}
	for in, want := range cases {
		if got := actor.FocusFile(in); got != want {
			t.Errorf("FocusFile(%q) = %q, want %q", in, got, want)
		}
	}
	if got := actor.FocusFile("a/b"); strings.Contains(got, "/") {
		t.Errorf("slash in name: %q", got)
	}
}

func TestFocusDir_EnvOverridesCache(t *testing.T) {
	if got := actor.FocusDir(func(string) string { return "/x/y" }); got != "/x/y" {
		t.Errorf("FocusDir with TIX_FOCUS_DIR = %q", got)
	}
	got := actor.FocusDir(func(string) string { return "" })
	if got != "" && filepath.Base(got) != "focus" {
		t.Errorf("FocusDir default = %q, want .../tix/focus", got)
	}
}
