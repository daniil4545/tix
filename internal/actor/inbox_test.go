package actor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daniil4545/tix/internal/actor"
)

func TestInboxDir_EnvOverridesCache(t *testing.T) {
	if got := actor.InboxDir(func(string) string { return "/x/y" }); got != "/x/y" {
		t.Errorf("InboxDir with variable = %q", got)
	}
}

func TestHookActor(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		name    string
		env     map[string]string
		session string
		codex   bool
		want    string
	}{
		{"input wins over inherited environment", map[string]string{"CLAUDE_CODE_SESSION_ID": "e1"}, "s1", true, "codex:s1"},
		{"Codex variable equal to input, no turn_id", map[string]string{"CLAUDE_CODE_SESSION_ID": "e1", "CODEX_THREAD_ID": "s1"}, "s1", false, "codex:s1"},
		{"Claude variable equal to input, with turn_id", map[string]string{"CLAUDE_CODE_SESSION_ID": "s1", "CODEX_THREAD_ID": "x1"}, "s1", true, "claude:s1"},
		{"environment without input", map[string]string{"CLAUDE_CODE_SESSION_ID": "e1"}, "", false, "claude:e1"},
		{"both variables without input", map[string]string{"CLAUDE_CODE_SESSION_ID": "e1", "CODEX_THREAD_ID": "x1"}, "", false, ""},
		{"Claude input", nil, "s1", false, "claude:s1"},
		{"Codex input", nil, "s1", true, "codex:s1"},
		{"no identity", nil, "", false, ""},
		{"space in id", nil, "a b", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := actor.HookActor(env(c.env), c.session, c.codex); got != c.want {
				t.Errorf("HookActor = %q, want %q", got, c.want)
			}
		})
	}
}

func TestMarks_RoundTripAndUnreadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "inbox")
	if m, ok := actor.ReadMarks(dir, "claude-a"); ok || len(m) != 0 {
		t.Errorf("no file: %v, %v", m, ok)
	}
	if err := actor.WriteMarks(dir, "claude-a", actor.Marks{"T-1": 3}); err != nil {
		t.Fatal(err)
	}
	if m, ok := actor.ReadMarks(dir, "claude-a"); !ok || m["T-1"] != 3 {
		t.Errorf("after write: %v, %v", m, ok)
	}
	for _, bad := range []string{"{", "null", `{"T-1":"x"}`, ""} {
		if err := os.WriteFile(filepath.Join(dir, "claude-a"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if m, ok := actor.ReadMarks(dir, "claude-a"); ok || len(m) != 0 {
			t.Errorf("unreadable %q accepted: %v", bad, m)
		}
	}
}
