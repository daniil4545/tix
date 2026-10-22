package actor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/actor"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

// Spec 2.4: order of --as, agent environment, TIX_ACTOR.
func TestResolve_Order(t *testing.T) {
	defer guard(t)
	cases := []struct {
		name      string
		flag      string
		env       map[string]string
		want      string
		wantKnown bool
	}{
		{"flag wins over environment", "opencode:f1",
			map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "CODEX_THREAD_ID": "x1", "TIX_ACTOR": "ta"}, "opencode:f1", true},
		{"Claude session", "", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1"}, "claude:c1", true},
		{"Codex thread", "", map[string]string{"CODEX_THREAD_ID": "x1"}, "codex:x1", true},
		{"agent environment before TIX_ACTOR", "", map[string]string{"CODEX_THREAD_ID": "x1", "TIX_ACTOR": "opencode:o1"}, "codex:x1", true},
		{"only TIX_ACTOR", "", map[string]string{"TIX_ACTOR": "opencode:o1"}, "opencode:o1", true},
		{"nothing", "", map[string]string{}, "unknown", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			got, known, err := actor.Resolve(c.flag, func(k string) string { return c.env[k] })
			if err != nil || known != c.wantKnown || got != c.want {
				t.Errorf("Resolve(%q, %v) = %q, %v; want %q, %v", c.flag, c.env, got, known, c.want, c.wantKnown)
			}
		})
	}
}

// Spec 2.4: an agent started inside another sees both agent variables; the name is refused, not guessed.
func TestResolve_BothAgentVariables_RefusedWithHint(t *testing.T) {
	defer guard(t)
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "CODEX_THREAD_ID": "x1"}
	_, _, err := actor.Resolve("", func(k string) string { return env[k] })
	if !errors.Is(err, actor.ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	for _, hint := range []string{"--as codex:x1", "--as claude:c1"} {
		if !strings.Contains(err.Error(), hint) {
			t.Errorf("error %q has no hint %q", err, hint)
		}
	}
}

// Valid guards comment and commit markup: space, pipe, arrow, line break.
func TestValid(t *testing.T) {
	defer guard(t)
	for _, ok := range []string{"claude:8f3a1c2e-aaaa", "codex:019-abc", "opencode:s1", "human", "unknown"} {
		if err := actor.Valid(ok); err != nil {
			t.Errorf("Valid(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "a|b", "a-->b", "a\nb", "a\r\nb"} {
		if err := actor.Valid(bad); err == nil {
			t.Errorf("Valid(%q) = nil, want an error", bad)
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
			t.Errorf("Short(%q) = %q, want %q", in, got, want)
		}
	}
}

// A model value lands in the `export` line for the shell: anything outside the allowlist is rejected.
func TestValidModel(t *testing.T) {
	defer guard(t)
	for _, ok := range []string{"claude-opus-4-8", "claude-opus-4-8[1m]", "gpt-5.5", "us.anthropic.x:v1/a_b", strings.Repeat("a", 100)} {
		if !actor.ValidModel(ok) {
			t.Errorf("ValidModel(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "a;b", "a$(id)", "a`id`", "a b", "a\nb", "a\r", "a'b", `a"b`, "a&b", "a|b", "a>b", "a*b", "a\\b", "a\x00b", "модель", strings.Repeat("a", 101)} {
		if actor.ValidModel(bad) {
			t.Errorf("ValidModel(%q) = true, want false", bad)
		}
	}
}
