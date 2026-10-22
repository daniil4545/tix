package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/cli"
)

// Zone I tests on top of the tester scenarios: mark format, Codex identity, order and the `more` tail,
// an untouched store.

func unitInboxBox(t *testing.T, env map[string]string) (b *box, dir string) {
	t.Helper()
	b = newBox(t)
	dir = filepath.Join(b.root, "marks")
	env["TIX_INBOX_DIR"] = dir
	b.extra = env
	return b, dir
}

func unitInboxRun(t *testing.T, b *box, stdin string) string {
	t.Helper()
	r := b.tix(stdin, "inbox", "--hook")
	if r.code != 0 || r.err != "" {
		t.Fatalf("inbox --hook: code %d, stderr %q; want 0 and silence", r.code, r.err)
	}
	return r.out
}

func unitHookStdin(event, session string, kv ...string) string {
	m := map[string]string{"session_id": session, "hook_event_name": event}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// Marks file: JSON {id: number} in a dir outside the store, no temp files, the store does not change.
func TestInboxUnit_MarksFileShape_BaseUntouched(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	b.newTask("Work", "todo")
	b.ok("", "take", "T-1")
	b.ok("", "comment", "T-1", "first", "--as", "human")
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	b.ok("", "comment", "T-1", "second", "--as", "human")
	commits, snap := b.commitCount(), b.snapshot()
	if out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1")); !strings.Contains(out, "second") {
		t.Fatalf("comment not delivered: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "claude-u1"))
	if err != nil {
		t.Fatal(err)
	}
	var marks map[string]int
	if json.Unmarshal(data, &marks) != nil || marks["T-1"] != 2 || len(marks) != 1 {
		t.Errorf("marks file %q, want {\"T-1\":2}", data)
	}
	// Besides the marks file there is only the lock .claude-u1.lock; no temp files remain.
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("%d files in the marks dir, want the marks and the lock", len(entries))
	}
	if b.commitCount() != commits || b.snapshot() != snap {
		t.Error("the hook changed the store: commit or files")
	}
}

// Codex identity without environment variables: turn_id in the input gives the prefix codex:.
func TestInboxUnit_CodexIdentityFromInput(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CODEX_THREAD_ID": "x9"})
	b.newTask("Work", "todo")
	b.ok("", "take", "T-1")
	b.extra = map[string]string{"TIX_INBOX_DIR": dir}
	in := unitHookStdin("UserPromptSubmit", "x9", "turn_id", "t1")
	unitInboxRun(t, b, in)
	b.ok("", "comment", "T-1", "for codex", "--as", "human")
	out := unitInboxRun(t, b, in)
	if !strings.Contains(out, "for codex") {
		t.Fatalf("Codex did not get the comment: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "codex-x9")); err != nil {
		t.Errorf("no file codex-x9: %v", err)
	}
}

// Stop in Codex and Claude: decision=block and reason, no hookSpecificOutput.
func TestInboxUnit_StopFormat(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	b.newTask("Work", "todo")
	b.ok("", "take", "T-1")
	unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	b.ok("", "comment", "T-1", "stop me", "--as", "human")
	var got map[string]any
	out := unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	if json.Unmarshal([]byte(out), &got) != nil || got["decision"] != "block" || got["hookSpecificOutput"] != nil ||
		!strings.Contains(fmt.Sprint(got["reason"]), "stop me") {
		t.Errorf("Stop reply %q", out)
	}
}

// Tasks go by number; what did not fit is listed in more, comma-separated.
func TestInboxUnit_OrderAndMoreListsTasks(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	for i := 0; i < 3; i++ {
		b.newTask("Work", "todo")
		b.ok("", "take", fmt.Sprintf("T-%d", i+1))
	}
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	long := strings.Repeat("x", 3000)
	for _, id := range []string{"T-3", "T-1", "T-2"} {
		b.ok("", "comment", id, long, "--as", "human")
	}
	var got struct {
		H struct {
			Text string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %q", out)
	}
	text := got.H.Text
	if i1, i2 := strings.Index(text, "T-1 human"), strings.Index(text, "T-2 human"); i1 < 0 || i2 < i1 {
		t.Errorf("task order broken: %.120q", text)
	}
	if len([]rune(text)) > 8000 || !strings.HasSuffix(text, "\n... more: tix show T-3") {
		t.Errorf("length %d, tail %q", len([]rune(text)), text[max(len(text)-60, 0):])
	}
	if out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1")); out != "" {
		t.Errorf("repeat not empty: %q", out)
	}
}

// Without --hook the command refuses as invalid input and does not wait for stdin.
func TestInboxUnit_WithoutHook_InputError(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	if r := b.tix("", "inbox"); r.code != 6 || r.out != "" {
		t.Errorf("tix inbox: code %d, stdout %q; want 6 and empty", r.code, r.out)
	}
}

// unitHold creates T-1 and claims it with session u1.
func unitHold(b *box) {
	b.t.Helper()
	b.newTask("Work", "todo")
	b.ok("", "take", "T-1")
}

func unitText(t *testing.T, out string) string {
	t.Helper()
	var got struct {
		H struct {
			Text string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not JSON: %q", out)
	}
	return got.H.Text
}

// Stop wakes only on a human word; an agent comment waits for PostToolUse and is not lost.
func TestInboxUnit_Stop_OnlyHumanBlocks(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	b.ok("", "comment", "T-1", "from agent", "--as", "claude:other")
	if out := unitInboxRun(t, b, unitHookStdin("Stop", "u1")); out != "" {
		t.Fatalf("Stop woken by an agent comment: %q", out)
	}
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "from agent") {
		t.Errorf("agent comment lost after Stop: %q", text)
	}
	b.ok("", "comment", "T-1", "agent two", "--as", "claude:other")
	b.ok("", "comment", "T-1", "human word", "--as", "human")
	var got map[string]string
	out := unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	if json.Unmarshal([]byte(out), &got) != nil || !strings.Contains(got["reason"], "human word") || !strings.Contains(got["reason"], "agent two") {
		t.Errorf("Stop with a human: %q, want both comments", out)
	}
}

// Two parallel hooks of one session deliver a comment exactly once.
func TestInboxUnit_ParallelHooks_DeliverOnce(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	b.ok("", "comment", "T-1", "once", "--as", "human")
	const n = 6
	outs := make([]res, n)
	var wg sync.WaitGroup
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i] = b.tix(unitHookStdin("PostToolUse", "u1"), "inbox", "--hook")
		}()
	}
	wg.Wait()
	delivered := 0
	for _, r := range outs {
		if r.code != 0 {
			t.Fatalf("code %d, stderr %q", r.code, r.err)
		}
		if r.out != "" {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("delivered %d times out of %d parallel hooks, want 1", delivered, n)
	}
}

// The mark cannot be written: empty, code 0; once writing works again, the comment is not lost.
func TestInboxUnit_MarksNotWritable_EmptyThenDelivered(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	b.ok("", "comment", "T-1", "kept", "--as", "human")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if out := unitInboxRun(t, b, unitHookStdin("Stop", "u1")); out != "" {
		t.Fatalf("the mark was not written, yet text was printed: %q", out)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "kept") {
		t.Errorf("comment lost: %q", text)
	}
	// The marks dir is a file: empty, code 0.
	b2, dir2 := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b2)
	if err := os.WriteFile(dir2, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := unitInboxRun(t, b2, unitHookStdin("PostToolUse", "u1")); out != "" {
		t.Errorf("marks dir is a file: %q", out)
	}
}

// A mark out of range (-1) resets to the current count, no panic and no repeat of the old.
func TestInboxUnit_NegativeMark_ResetsToCurrent(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	b.ok("", "comment", "T-1", "old", "--as", "human")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(dir, "claude-u1")
	if err := os.WriteFile(mark, []byte(`{"T-1":-1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1")); out != "" {
		t.Errorf("mark -1 delivered an old one: %q", out)
	}
	if data, _ := os.ReadFile(mark); string(data) != `{"T-1":1}` {
		t.Errorf("mark after reset %q", data)
	}
	b.ok("", "comment", "T-1", "new", "--as", "human")
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "new") {
		t.Errorf("new one not delivered: %q", text)
	}
}

// Input above the 64 KB ceiling: empty, code 0, a line in .tix/log.
func TestInboxUnit_InputOverLimit_LoggedEmpty(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	in := unitHookStdin("PostToolUse", "u1", "tool_response", strings.Repeat("x", 70<<10))
	if out := unitInboxRun(t, b, in); out != "" {
		t.Errorf("input above the ceiling: %q", out)
	}
	logData, _ := os.ReadFile(filepath.Join(b.home, ".tix", "log"))
	if !strings.Contains(string(logData), "hook input not read") {
		t.Errorf("no line about the input in .tix/log: %q", logData)
	}
}

// unitRunPipe runs inbox --hook with stdin from r and waits for the return no longer than limit.
func unitRunPipe(t *testing.T, b *box, r io.Reader, limit time.Duration) (out string, code int) {
	t.Helper()
	vars := map[string]string{"TIX_HOME": b.home, "TIX_GIT": gitBin, "TIX_LOCK_TIMEOUT": "2s", "HOME": os.Getenv("HOME")}
	for k, v := range b.extra {
		vars[k] = v
	}
	var o, e bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(context.Background(), []string{"inbox", "--hook"}, cli.Env{
			Stdin: r, Stdout: &o, Stderr: &e, Cwd: b.cwd, Getenv: func(k string) string { return vars[k] },
		})
	}()
	select {
	case code = <-done:
		return o.String(), code
	case <-time.After(limit):
		t.Fatalf("the hook did not return within %v with stdin without EOF", limit)
		return "", 0
	}
}

// Stdin without EOF: a whole JSON is processed at once, a truncated one - no later than the timeout.
func TestInboxUnit_StdinWithoutEOF_ReturnsInTime(t *testing.T) {
	defer cli.SetHookReadTimeout(200 * time.Millisecond)()
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	b.ok("", "comment", "T-1", "no eof", "--as", "human")

	pr, pw := io.Pipe()
	defer pw.Close()
	go func() { _, _ = pw.Write([]byte(unitHookStdin("PostToolUse", "u1"))) }()
	out, code := unitRunPipe(t, b, pr, 5*time.Second)
	if code != 0 || !strings.Contains(unitText(t, out), "no eof") {
		t.Errorf("whole JSON without EOF: code %d, %q", code, out)
	}

	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	go func() { _, _ = pw2.Write([]byte(`{"session_id":"u1","hook_event`)) }()
	if out, code := unitRunPipe(t, b, pr2, 5*time.Second); code != 0 || out != "" {
		t.Errorf("truncated JSON: code %d, %q", code, out)
	}
}

// Quotes, backslash, newline, tab and a control char in the text give valid JSON,
// and parsing returns the text undistorted.
func TestInboxUnit_JSONEscaping_RoundTrip(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	body := "say \"hi\" \\ back\nsecond\tline \x01 end"
	b.ok("", "comment", "T-1", body, "--as", "human")
	for _, ev := range []string{"PostToolUse", "UserPromptSubmit", "Stop"} {
		if ev != "PostToolUse" {
			b.ok("", "comment", "T-1", body, "--as", "human")
		}
		out := unitInboxRun(t, b, unitHookStdin(ev, "u1"))
		var text string
		if ev == "Stop" {
			var got map[string]string
			if json.Unmarshal([]byte(out), &got) != nil {
				t.Fatalf("%s: not JSON: %q", ev, out)
			}
			text = got["reason"]
		} else {
			text = unitText(t, out)
		}
		if !strings.Contains(text, body) {
			t.Errorf("%s: text distorted: %q, want it to contain %q", ev, text, body)
		}
	}
}
