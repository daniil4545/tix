package actor_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/actor"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("паника: %v", r)
	}
}

// Спека 2.4: порядок --as, окружение агента, TIX_ACTOR.
func TestResolve_Order(t *testing.T) {
	defer guard(t)
	cases := []struct {
		name      string
		flag      string
		env       map[string]string
		want      string
		wantKnown bool
	}{
		{"флаг главнее окружения", "opencode:f1",
			map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "CODEX_THREAD_ID": "x1", "TIX_ACTOR": "ta"}, "opencode:f1", true},
		{"сессия Claude", "", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1"}, "claude:c1", true},
		{"поток Codex", "", map[string]string{"CODEX_THREAD_ID": "x1"}, "codex:x1", true},
		{"Claude раньше Codex", "", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "CODEX_THREAD_ID": "x1"}, "claude:c1", true},
		{"окружение агента раньше TIX_ACTOR", "", map[string]string{"CODEX_THREAD_ID": "x1", "TIX_ACTOR": "opencode:o1"}, "codex:x1", true},
		{"только TIX_ACTOR", "", map[string]string{"TIX_ACTOR": "opencode:o1"}, "opencode:o1", true},
		{"ничего", "", map[string]string{}, "unknown", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			got, known := actor.Resolve(c.flag, func(k string) string { return c.env[k] })
			if known != c.wantKnown || got != c.want {
				t.Errorf("Resolve(%q, %v) = %q, %v; ждали %q, %v", c.flag, c.env, got, known, c.want, c.wantKnown)
			}
		})
	}
}

// Valid охраняет разметку комментария и коммита: пробел, черта, стрелка, перевод строки.
func TestValid(t *testing.T) {
	defer guard(t)
	for _, ok := range []string{"claude:8f3a1c2e-aaaa", "codex:019-abc", "opencode:s1", "human", "unknown"} {
		if err := actor.Valid(ok); err != nil {
			t.Errorf("Valid(%q) = %v, ждали nil", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "a|b", "a-->b", "a\nb", "a\r\nb"} {
		if err := actor.Valid(bad); err == nil {
			t.Errorf("Valid(%q) = nil, ждали ошибку", bad)
		}
	}
}

func TestShort_KindAndEightCharsOfID(t *testing.T) {
	defer guard(t)
	cases := map[string]string{
		"claude:8f3a1c2e-1b2c-4d5e-9f00-aabbccddeeff": "claude:8f3a1c2e",
		"codex:019abcde-ffff":                         "codex:019abcde",
		"claude:ab12":                                 "claude:ab12",
		"human":                                       "human",
	}
	for in, want := range cases {
		if got := actor.Short(in); got != want {
			t.Errorf("Short(%q) = %q, ждали %q", in, got, want)
		}
	}
}

// Значение модели попадает в строку `export` для shell: всё, что выходит за белый список, отказ.
func TestValidModel(t *testing.T) {
	defer guard(t)
	for _, ok := range []string{"claude-opus-4-8", "claude-opus-4-8[1m]", "gpt-5.5", "us.anthropic.x:v1/a_b", strings.Repeat("a", 100)} {
		if !actor.ValidModel(ok) {
			t.Errorf("ValidModel(%q) = false, ждали true", ok)
		}
	}
	for _, bad := range []string{"", "a;b", "a$(id)", "a`id`", "a b", "a\nb", "a\r", "a'b", `a"b`, "a&b", "a|b", "a>b", "a*b", "a\\b", "a\x00b", "модель", strings.Repeat("a", 101)} {
		if actor.ValidModel(bad) {
			t.Errorf("ValidModel(%q) = true, ждали false", bad)
		}
	}
}
