package cli_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/cli"
)

// The model in history.

// tripwire fails if anyone reads stdin.
type tripwire struct{ t *testing.T }

func (w tripwire) Read([]byte) (int, error) {
	w.t.Helper()
	w.t.Error("stdin was read, but there is no --hook flag")
	return 0, os.ErrClosed
}

func (b *box) envFile() string { return filepath.Join(b.root, "claude-env") }

func (b *box) envFileText() string {
	data, err := os.ReadFile(b.envFile())
	if err != nil {
		return ""
	}
	return string(data)
}

func TestCtxHook_Stdin_WritesModelOnlyWhenSafe(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		want  string
	}{
		{"valid", `{"model":"claude-opus-4-8[1m]","source":"startup"}`, "export TIX_MODEL='claude-opus-4-8[1m]'\n"},
		{"semicolon", `{"model":"a;rm -rf ~"}`, ""},
		{"command substitution", `{"model":"a$(id)"}`, ""},
		{"backtick", "{\"model\":\"a`id`\"}", ""},
		{"newline", `{"model":"a\nexport X=1"}`, ""},
		{"space", `{"model":"a b"}`, ""},
		{"quote", `{"model":"a'b"}`, ""},
		{"too long", `{"model":"` + strings.Repeat("a", 101) + `"}`, ""},
		{"empty model", `{"model":""}`, ""},
		{"no model", `{"session_id":"s","source":"clear"}`, ""},
		{"model not string", `{"model":["a"]}`, ""},
		{"not json", `model=abc`, ""},
		{"empty stdin", ``, ""},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			b.extra = map[string]string{"CLAUDE_ENV_FILE": b.envFile()}
			r := b.tix(c.stdin, "ctx", "--hook")
			if r.code != 0 {
				t.Fatalf("ctx --hook %q: code %d, stderr %q", c.stdin, r.code, r.err)
			}
			if got := b.envFileText(); got != c.want {
				t.Errorf("CLAUDE_ENV_FILE after %q = %q, want %q", c.stdin, got, c.want)
			}
			if !strings.Contains(r.out, msgNoTasks) {
				t.Errorf("ctx --hook did not print the plain ctx: %q", r.out)
			}
		})
	}
}

func TestCtxHook_Repeated_AppendsToExistingFile(t *testing.T) {
	b := newBox(t)
	b.extra = map[string]string{"CLAUDE_ENV_FILE": b.envFile()}
	if err := os.WriteFile(b.envFile(), []byte("export A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.ok(`{"model":"m1"}`, "ctx", "--hook")
	b.ok(`{"model":"m2"}`, "ctx", "--hook")
	if got, want := b.envFileText(), "export A=1\nexport TIX_MODEL='m1'\nexport TIX_MODEL='m2'\n"; got != want {
		t.Errorf("CLAUDE_ENV_FILE = %q, want %q", got, want)
	}
}

func TestCtxHook_NoEnvFile_PrintsCtxWithoutWriting(t *testing.T) {
	b := newBox(t)
	r := b.ok(`{"model":"m1"}`, "ctx", "--hook")
	if !strings.Contains(r.out, msgNoTasks) {
		t.Errorf("ctx output = %q", r.out)
	}
	if got := b.envFileText(); got != "" {
		t.Errorf("file created without CLAUDE_ENV_FILE: %q", got)
	}
}

func TestCtx_WithoutHook_DoesNotReadStdin(t *testing.T) {
	b := newBox(t)
	var o, e bytes.Buffer
	vars := map[string]string{
		"TIX_HOME": b.home, "TIX_GIT": gitBin, "CLAUDE_ENV_FILE": b.envFile(),
	}
	code := cli.Run(context.Background(), []string{"ctx"}, cli.Env{
		Stdin: tripwire{t}, Stdout: &o, Stderr: &e, Cwd: b.cwd,
		Getenv: func(k string) string { return vars[k] },
	})
	if code != 0 {
		t.Fatalf("ctx without --hook: code %d, stderr %q", code, e.String())
	}
	if got := b.envFileText(); got != "" {
		t.Errorf("ctx without --hook wrote the env file: %q", got)
	}
}

func TestCommit_ModelTrailer(t *testing.T) {
	tests := []struct {
		name, model string
		want        bool
	}{
		{"set", "claude-opus-4-8[1m]", true},
		{"unset", "", false},
		{"unsafe value dropped, write succeeds", "a;b", false},
		{"multiline value dropped, write succeeds", "a\nActor: x", false},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("TIX_MODEL", c.model)
			b := newBox(t)
			b.newTask("task", "todo")
			msg := b.git("log", "-1", "--format=%B")
			has := strings.Contains(msg, "\nModel: "+c.model+"\n") || strings.HasPrefix(msg, "Model: "+c.model)
			if c.want && !has {
				t.Errorf("commit has no Model trailer: %s\n%s", c.model, msg)
			}
			if !c.want && strings.Contains(msg, "Model:") {
				t.Errorf("Model trailer must not be written for %q:\n%s", c.model, msg)
			}
		})
	}
}

func TestCtxHook_StdinNeverClosed_PrintsCtxWithoutModel(t *testing.T) {
	defer cli.SetHookReadTimeout(100 * time.Millisecond)()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	b := newBox(t)
	r := runWith(t, b, pr, "ctx", "--hook")
	if r.code != 0 || !strings.Contains(r.out, msgNoTasks) {
		t.Fatalf("ctx --hook with unclosed stdin: code %d, out %q, err %q", r.code, r.out, r.err)
	}
	if got := b.envFileText(); got != "" {
		t.Errorf("model written without input: %q", got)
	}
}

func TestCtxHook_StdinNeverClosed_CompleteObjectStillRead(t *testing.T) {
	defer cli.SetHookReadTimeout(5 * time.Second)()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	go func() { _, _ = pw.Write([]byte(`{"model":"m1"}`)) }()
	b := newBox(t)
	r := runWith(t, b, pr, "ctx", "--hook")
	if r.code != 0 {
		t.Fatalf("code %d, err %q", r.code, r.err)
	}
	if got, want := b.envFileText(), "export TIX_MODEL='m1'\n"; got != want {
		t.Errorf("CLAUDE_ENV_FILE = %q, want %q", got, want)
	}
}

func TestCtxHook_EnvFileWithoutFinalNewline_KeepsForeignLine(t *testing.T) {
	b := newBox(t)
	b.extra = map[string]string{"CLAUDE_ENV_FILE": b.envFile()}
	if err := os.WriteFile(b.envFile(), []byte("export A=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.ok(`{"model":"m1"}`, "ctx", "--hook")
	if got, want := b.envFileText(), "export A=1\nexport TIX_MODEL='m1'\n"; got != want {
		t.Errorf("CLAUDE_ENV_FILE = %q, want %q", got, want)
	}
}

// runWith runs tix with arbitrary stdin and CLAUDE_ENV_FILE.
func runWith(t *testing.T, b *box, stdin io.Reader, args ...string) res {
	t.Helper()
	var o, e bytes.Buffer
	vars := map[string]string{"TIX_HOME": b.home, "TIX_GIT": gitBin, "CLAUDE_ENV_FILE": b.envFile()}
	code := cli.Run(context.Background(), args, cli.Env{
		Stdin: stdin, Stdout: &o, Stderr: &e, Cwd: b.cwd,
		Getenv: func(k string) string { return vars[k] },
	})
	return res{o.String(), e.String(), code}
}
