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

// Тесты зоны I поверх сценариев tester: формат отметок, личность Codex, порядок и хвост more,
// нетронутая база.

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
		t.Fatalf("inbox --hook: код %d, stderr %q; ждали 0 и тишину", r.code, r.err)
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

// Файл отметок: JSON {id: число} в каталоге вне базы, без временных файлов, база не меняется.
func TestInboxUnit_MarksFileShape_BaseUntouched(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "take", "T-1")
	b.ok("", "comment", "T-1", "first", "--as", "human")
	unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))
	b.ok("", "comment", "T-1", "second", "--as", "human")
	commits, snap := b.commitCount(), b.snapshot()
	if out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1")); !strings.Contains(out, "second") {
		t.Fatalf("комментарий не доставлен: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "claude-u1"))
	if err != nil {
		t.Fatal(err)
	}
	var marks map[string]int
	if json.Unmarshal(data, &marks) != nil || marks["T-1"] != 2 || len(marks) != 1 {
		t.Errorf("файл отметок %q, ждали {\"T-1\":2}", data)
	}
	// Кроме файла отметок есть только замок .claude-u1.lock; временных файлов не остаётся.
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("в каталоге отметок %d файлов, ждали отметки и замок", len(entries))
	}
	if b.commitCount() != commits || b.snapshot() != snap {
		t.Error("хук изменил базу: коммит или файлы")
	}
}

// Личность Codex без переменных окружения: turn_id во входе даёт префикс codex:.
func TestInboxUnit_CodexIdentityFromInput(t *testing.T) {
	b, dir := unitInboxBox(t, map[string]string{"CODEX_THREAD_ID": "x9"})
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "take", "T-1")
	b.extra = map[string]string{"TIX_INBOX_DIR": dir}
	in := unitHookStdin("UserPromptSubmit", "x9", "turn_id", "t1")
	unitInboxRun(t, b, in)
	b.ok("", "comment", "T-1", "for codex", "--as", "human")
	out := unitInboxRun(t, b, in)
	if !strings.Contains(out, "for codex") {
		t.Fatalf("Codex не получил комментарий: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "codex-x9")); err != nil {
		t.Errorf("нет файла codex-x9: %v", err)
	}
}

// Stop в Codex и Claude: decision=block и reason, без hookSpecificOutput.
func TestInboxUnit_StopFormat(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "take", "T-1")
	unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	b.ok("", "comment", "T-1", "stop me", "--as", "human")
	var got map[string]any
	out := unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	if json.Unmarshal([]byte(out), &got) != nil || got["decision"] != "block" || got["hookSpecificOutput"] != nil ||
		!strings.Contains(fmt.Sprint(got["reason"]), "stop me") {
		t.Errorf("ответ Stop %q", out)
	}
}

// Задачи идут по номеру; что не влезло, перечислено в more через запятую.
func TestInboxUnit_OrderAndMoreListsTasks(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	for i := 0; i < 3; i++ {
		b.ok("", "new", "Work", "--to", "todo")
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
		t.Fatalf("не JSON: %q", out)
	}
	text := got.H.Text
	if i1, i2 := strings.Index(text, "T-1 human"), strings.Index(text, "T-2 human"); i1 < 0 || i2 < i1 {
		t.Errorf("порядок задач нарушен: %.120q", text)
	}
	if len([]rune(text)) > 8000 || !strings.HasSuffix(text, "\n... more: tix show T-3") {
		t.Errorf("длина %d, хвост %q", len([]rune(text)), text[max(len(text)-60, 0):])
	}
	if out := unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1")); out != "" {
		t.Errorf("повтор не пуст: %q", out)
	}
}

// Без --hook команда отказывает как неверный ввод и не ждёт stdin.
func TestInboxUnit_WithoutHook_InputError(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	if r := b.tix("", "inbox"); r.code != 6 || r.out != "" {
		t.Errorf("tix inbox: код %d, stdout %q; ждали 6 и пусто", r.code, r.out)
	}
}

// unitHold создаёт T-1 и берёт её сессией u1.
func unitHold(b *box) {
	b.t.Helper()
	b.ok("", "new", "Work", "--to", "todo")
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
		t.Fatalf("вывод не JSON: %q", out)
	}
	return got.H.Text
}

// Stop будит только слово человека; комментарий агента ждёт PostToolUse и не теряется.
func TestInboxUnit_Stop_OnlyHumanBlocks(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	b.ok("", "comment", "T-1", "from agent", "--as", "claude:other")
	if out := unitInboxRun(t, b, unitHookStdin("Stop", "u1")); out != "" {
		t.Fatalf("Stop разбужен комментарием агента: %q", out)
	}
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "from agent") {
		t.Errorf("комментарий агента потерян после Stop: %q", text)
	}
	b.ok("", "comment", "T-1", "agent two", "--as", "claude:other")
	b.ok("", "comment", "T-1", "human word", "--as", "human")
	var got map[string]string
	out := unitInboxRun(t, b, unitHookStdin("Stop", "u1"))
	if json.Unmarshal([]byte(out), &got) != nil || !strings.Contains(got["reason"], "human word") || !strings.Contains(got["reason"], "agent two") {
		t.Errorf("Stop с человеком: %q, ждали оба комментария", out)
	}
}

// Два параллельных хука одной сессии доставляют комментарий ровно один раз.
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
			t.Fatalf("код %d, stderr %q", r.code, r.err)
		}
		if r.out != "" {
			delivered++
		}
	}
	if delivered != 1 {
		t.Errorf("доставлено %d раз из %d параллельных хуков, ждали 1", delivered, n)
	}
}

// Отметку записать нельзя: пусто, код 0; когда запись вернулась, комментарий не потерян.
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
		t.Fatalf("отметку не записали, а текст напечатан: %q", out)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "kept") {
		t.Errorf("комментарий потерян: %q", text)
	}
	// Каталог отметок - файл: пусто, код 0.
	b2, dir2 := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b2)
	if err := os.WriteFile(dir2, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := unitInboxRun(t, b2, unitHookStdin("PostToolUse", "u1")); out != "" {
		t.Errorf("каталог отметок - файл: %q", out)
	}
}

// Отметка вне диапазона (-1) сбрасывается на текущее число, без паники и без повтора старого.
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
		t.Errorf("отметка -1 доставила старое: %q", out)
	}
	if data, _ := os.ReadFile(mark); string(data) != `{"T-1":1}` {
		t.Errorf("отметка после сброса %q", data)
	}
	b.ok("", "comment", "T-1", "new", "--as", "human")
	if text := unitText(t, unitInboxRun(t, b, unitHookStdin("PostToolUse", "u1"))); !strings.Contains(text, "new") {
		t.Errorf("новое не доставлено: %q", text)
	}
}

// Вход больше потолка 64 КБ: пусто, код 0, строка в .tix/log.
func TestInboxUnit_InputOverLimit_LoggedEmpty(t *testing.T) {
	b, _ := unitInboxBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "u1"})
	unitHold(b)
	in := unitHookStdin("PostToolUse", "u1", "tool_response", strings.Repeat("x", 70<<10))
	if out := unitInboxRun(t, b, in); out != "" {
		t.Errorf("вход больше потолка: %q", out)
	}
	logData, _ := os.ReadFile(filepath.Join(b.home, ".tix", "log"))
	if !strings.Contains(string(logData), "hook input not read") {
		t.Errorf("в .tix/log нет строки о входе: %q", logData)
	}
}

// unitRunPipe запускает inbox --hook со stdin из r и ждёт возврата не дольше limit.
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
		t.Fatalf("хук не вернулся за %v при stdin без EOF", limit)
		return "", 0
	}
}

// Stdin без EOF: целый JSON обрабатывается сразу, оборванный - не позже срока.
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
		t.Errorf("целый JSON без EOF: код %d, %q", code, out)
	}

	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	go func() { _, _ = pw2.Write([]byte(`{"session_id":"u1","hook_event`)) }()
	if out, code := unitRunPipe(t, b, pr2, 5*time.Second); code != 0 || out != "" {
		t.Errorf("оборванный JSON: код %d, %q", code, out)
	}
}

// Кавычки, обратная косая, перевод строки, табуляция и управляющий символ в тексте дают валидный JSON,
// а разбор возвращает текст без искажений.
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
				t.Fatalf("%s: не JSON: %q", ev, out)
			}
			text = got["reason"]
		} else {
			text = unitText(t, out)
		}
		if !strings.Contains(text, body) {
			t.Errorf("%s: текст исказился: %q, ждали вхождение %q", ev, text, body)
		}
	}
}
