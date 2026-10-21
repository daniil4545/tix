package actor_test

import (
	"os"
	"path/filepath"
	"testing"

	"tracker/internal/actor"
)

func TestInboxDir_EnvOverridesCache(t *testing.T) {
	if got := actor.InboxDir(func(string) string { return "/x/y" }); got != "/x/y" {
		t.Errorf("InboxDir с переменной = %q", got)
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
		{"окружение главнее входа", map[string]string{"CLAUDE_CODE_SESSION_ID": "e1"}, "s1", false, "claude:e1"},
		{"вход Claude", nil, "s1", false, "claude:s1"},
		{"вход Codex", nil, "s1", true, "codex:s1"},
		{"нет личности", nil, "", false, ""},
		{"пробел в id", nil, "a b", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := actor.HookActor(env(c.env), c.session, c.codex); got != c.want {
				t.Errorf("HookActor = %q, ждали %q", got, c.want)
			}
		})
	}
}

func TestMarks_RoundTripAndUnreadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "inbox")
	if m, ok := actor.ReadMarks(dir, "claude-a"); ok || len(m) != 0 {
		t.Errorf("без файла: %v, %v", m, ok)
	}
	if err := actor.WriteMarks(dir, "claude-a", actor.Marks{"T-1": 3}); err != nil {
		t.Fatal(err)
	}
	if m, ok := actor.ReadMarks(dir, "claude-a"); !ok || m["T-1"] != 3 {
		t.Errorf("после записи: %v, %v", m, ok)
	}
	for _, bad := range []string{"{", "null", `{"T-1":"x"}`, ""} {
		if err := os.WriteFile(filepath.Join(dir, "claude-a"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if m, ok := actor.ReadMarks(dir, "claude-a"); ok || len(m) != 0 {
			t.Errorf("нечитаемый %q принят: %v", bad, m)
		}
	}
}
