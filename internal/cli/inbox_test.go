package cli_test

// Доставка комментариев человека агенту: docs/plans/mvp-inbox.md, разделы 3 и 5.
// Пишутся по спеке, без чтения кода зоны I. Каталог отметок вне базы: TRACKER_INBOX_DIR.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	inboxHeader = "New comments on your tasks (from tk):"
	inboxLimit  = 8000
)

func (b *box) inboxDir() string { return filepath.Join(b.root, "inbox") }

// inboxMark - файл отметок сессии claude:c1: <агент>-<id сессии>.
func (b *box) inboxMark() string { return filepath.Join(b.inboxDir(), "claude-c1") }

// newInboxBox - стенд с сессией Claude "c1" и каталогом отметок во временной папке.
func newInboxBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TRACKER_INBOX_DIR": b.inboxDir()}
	return b
}

// hold создаёт T-1 и берёт её сессией c1.
func (b *box) hold() {
	b.t.Helper()
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "take", "T-1")
}

func (b *box) comment(id, text, as string) {
	b.t.Helper()
	b.ok("", "comment", id, text, "--as", as)
}

func hookIn(event string, kv ...string) string {
	m := map[string]string{"session_id": "c1", "hook_event_name": event}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	data, _ := json.Marshal(m)
	return string(data)
}

// inbox запускает `tk inbox --hook` с входом хука и возвращает доставленный текст ("" - пусто).
// Код выхода всегда 0; формат ответа сверяется с событием.
func (b *box) inbox(event string, kv ...string) string {
	b.t.Helper()
	return b.inboxRaw(hookIn(event, kv...), event)
}

func (b *box) inboxRaw(stdin, event string) string {
	b.t.Helper()
	r := b.tk(stdin, "inbox", "--hook")
	if r.code != 0 {
		b.t.Fatalf("inbox --hook %q: код %d, stdout %q, stderr %q, ждали 0", stdin, r.code, r.out, r.err)
	}
	return parseHookOut(b.t, r.out, event)
}

func parseHookOut(t *testing.T, out, event string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("вывод из пробелов %q, ждали пусто или JSON", out)
	}
	if event == "Stop" {
		var v struct{ Decision, Reason string }
		if err := json.Unmarshal([]byte(out), &v); err != nil || v.Decision != "block" || v.Reason == "" {
			t.Fatalf("Stop: вывод %q, ждали {decision:block, reason:<текст>}: %v", out, err)
		}
		return v.Reason
	}
	var v struct {
		H struct {
			Name string `json:"hookEventName"`
			Text string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.H.Name != event || v.H.Text == "" {
		t.Fatalf("%s: вывод %q, ждали hookSpecificOutput с hookEventName=%s и additionalContext: %v", event, out, event, err)
	}
	return v.H.Text
}

// wantLine ищет строку комментария `T-N автор HH:MM: текст`.
func wantLine(t *testing.T, text, id, author, body string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(id) + ` ` + regexp.QuoteMeta(author) + ` \d\d:\d\d: ` + regexp.QuoteMeta(body) + `$`)
	if !re.MatchString(text) {
		t.Errorf("в тексте нет строки `%s %s HH:MM: %s`:\n%s", id, author, body, text)
	}
}

// Сценарий 1: первый вызов пуст и ставит отметку; новый комментарий доставляется один раз.
func TestInbox_FirstCallEmptyThenHumanCommentDeliveredOnce(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.comment("T-1", "old remark before first call", "human")

	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("первый вызов: %q, ждали пусто (старое читают через tk show)", got)
	}
	if _, err := os.Stat(b.inboxMark()); err != nil {
		t.Fatalf("после первого вызова нет файла отметок %s: %v", b.inboxMark(), err)
	}
	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("старый комментарий доставлен после первого вызова: %q", got)
	}

	b.comment("T-1", "Answer: use sqlite", "human")
	got := b.inbox("PostToolUse")
	if !strings.HasPrefix(got, inboxHeader+"\n") {
		t.Errorf("текст %q, ждали заголовок %q первой строкой", got, inboxHeader)
	}
	wantLine(t, got, "T-1", "human", "Answer: use sqlite")
	if strings.Contains(got, "old remark") {
		t.Errorf("доставлен комментарий до отметки: %q", got)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("повтор: %q, ждали пусто", again)
	}
}

// Сценарий 1: два комментария подряд (в одну минуту) доставляются оба, по порядку.
func TestInbox_TwoCommentsSameMinute_BothDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "first remark", "human")
	b.comment("T-1", "second remark", "human")

	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "first remark")
	wantLine(t, got, "T-1", "human", "second remark")
	if strings.Index(got, "first remark") > strings.Index(got, "second remark") {
		t.Errorf("порядок нарушен:\n%s", got)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("повтор: %q, ждали пусто", again)
	}
}

// Спека 3.5: PostToolUse и UserPromptSubmit - additionalContext с тем же hookEventName.
func TestInbox_PostToolUseAndUserPromptSubmit_AdditionalContext(t *testing.T) {
	for _, event := range []string{"PostToolUse", "UserPromptSubmit"} {
		t.Run(event, func(t *testing.T) {
			defer guard(t)
			b := newInboxBox(t)
			b.hold()
			b.inbox(event)
			b.comment("T-1", "do it", "human")
			wantLine(t, b.inbox(event), "T-1", "human", "do it")
		})
	}
}

// Сценарий 2: свой комментарий агента не доставляется.
func TestInbox_OwnComment_NotDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "my own remark", "claude:c1")
	if got := b.inbox("PostToolUse"); got != "" {
		t.Errorf("свой комментарий доставлен: %q", got)
	}
}

// Сценарий 2: комментарий в задаче без захвата этой личности и в задаче с чужим захватом - не доставляется;
// комментарий в своей задаче при этом доставляется (контроль: пусто не из-за поломки).
func TestInbox_UnclaimedAndForeignClaim_NotDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.ok("", "new", "Free", "--to", "todo")
	b.ok("", "new", "Foreign", "--to", "todo")
	b.ok("", "take", "T-3", "--as", "claude:other")
	b.inbox("PostToolUse")

	b.comment("T-1", "for mine", "human")
	b.comment("T-2", "for free", "human")
	b.comment("T-3", "for foreign", "human")
	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "for mine")
	for _, bad := range []string{"for free", "for foreign", "T-2", "T-3"} {
		if strings.Contains(got, bad) {
			t.Errorf("доставлено лишнее %q:\n%s", bad, got)
		}
	}
}

// Сценарий 3: Stop с новым комментарием - decision block и текст в reason; без нового - пусто; повтор пуст.
func TestInbox_Stop_BlocksWithTextOnlyWhenNew(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	if got := b.inbox("Stop"); got != "" {
		t.Fatalf("Stop без комментариев: %q, ждали пусто", got)
	}
	b.comment("T-1", "one more thing", "human")
	got := b.inbox("Stop")
	if !strings.HasPrefix(got, inboxHeader+"\n") {
		t.Errorf("reason %q, ждали заголовок %q", got, inboxHeader)
	}
	wantLine(t, got, "T-1", "human", "one more thing")
	if again := b.inbox("Stop"); again != "" {
		t.Errorf("повторный Stop: %q, ждали пусто (Stop не должен зациклиться)", again)
	}
}

// Сценарий 4: много комментариев - текст до 8000 знаков, хвост `more`, повторный Stop пуст.
func TestInbox_ManyComments_TruncatedWithMoreTail(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("Stop")
	for i := 1; i <= 6; i++ {
		b.comment("T-1", fmt.Sprintf("n%d %s", i, strings.Repeat("a", 2000)), "human")
	}
	got := b.inbox("Stop")
	if n := utf8.RuneCountInString(got); n > inboxLimit {
		t.Errorf("текст %d знаков, предел %d", n, inboxLimit)
	}
	if !strings.Contains(got, "n1 ") {
		t.Errorf("первый комментарий не доставлен")
	}
	if last := got[strings.LastIndex(strings.TrimRight(got, "\n"), "\n")+1:]; !strings.HasPrefix(strings.TrimRight(last, "\n"), "... more: tk show T-1") {
		t.Errorf("последняя строка %q, ждали `... more: tk show T-1`", last)
	}
	if again := b.inbox("Stop"); again != "" {
		t.Errorf("повторный Stop: %q, ждали пусто", again)
	}
}

// Сценарий 4: один комментарий длиннее 8000 знаков обрезается до предела.
func TestInbox_SingleLongComment_Truncated(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", strings.Repeat("x", 12000), "human")
	got := b.inbox("PostToolUse")
	if got == "" {
		t.Fatal("длинный комментарий не доставлен совсем")
	}
	if n := utf8.RuneCountInString(got); n > inboxLimit {
		t.Errorf("текст %d знаков, предел %d", n, inboxLimit)
	}
	if again := b.inbox("PostToolUse"); again != "" {
		t.Errorf("повтор: %q, ждали пусто", again)
	}
}

// Сценарий 4a: вход с agent_id (вызов из субагента) - пусто, и ответ остаётся для главного агента.
func TestInbox_SubagentCall_EmptyAndKeepsCommentForMainAgent(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	b.comment("T-1", "for the main agent", "human")
	if got := b.inbox("PostToolUse", "agent_id", "sub1"); got != "" {
		t.Fatalf("вызов с agent_id: %q, ждали пусто", got)
	}
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "for the main agent")
}

// Сценарий 4a: истёкший захват своей личности не мешает доставке (ответ на ask приходит после 3 ч).
func TestInbox_ExpiredOwnClaim_StillDelivered(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.rewrite("T-1", func(s string) string {
		return regexp.MustCompile(`(?m)^claim: (\S+) until .*$`).ReplaceAllString(s, "claim: $1 until 2000-01-01T00:00")
	})
	if c := b.claimOf("T-1"); !strings.HasSuffix(c, "until 2000-01-01T00:00") {
		t.Fatalf("claim не истёк в подготовке: %q", c)
	}
	b.inbox("PostToolUse")
	b.comment("T-1", "late answer", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "late answer")
}

// Сценарий 4a: нечитаемый файл отметок = нет отметки: пусто, ничего старого не доставлено, дальше работает.
func TestInbox_CorruptMarkFile_EmptyThenWorks(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	if err := os.WriteFile(b.inboxMark(), []byte("not json{"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.comment("T-1", "written while mark broken", "human")
	if got := b.inbox("PostToolUse"); got != "" {
		t.Fatalf("с битым файлом отметок: %q, ждали пусто", got)
	}
	b.comment("T-1", "after repair", "human")
	got := b.inbox("PostToolUse")
	wantLine(t, got, "T-1", "human", "after repair")
	if strings.Contains(got, "written while mark broken") {
		t.Errorf("доставлен комментарий, учтённый при сбросе отметки:\n%s", got)
	}
}

// Спека 3.3: число в файле больше текущего (правка в обход ядра) - сброс на текущее, новое после этого доставляется.
func TestInbox_MarkAboveCurrent_ResetsAndDeliversNew(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.inbox("PostToolUse")
	data, err := os.ReadFile(b.inboxMark())
	if err != nil {
		t.Fatal(err)
	}
	var marks map[string]int
	if err := json.Unmarshal(data, &marks); err != nil || len(marks) != 1 {
		t.Fatalf("файл отметок %q: ждали JSON-объект с одной задачей: %v", data, err)
	}
	for k := range marks {
		marks[k] = 99
	}
	data, _ = json.Marshal(marks)
	if err := os.WriteFile(b.inboxMark(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	b.comment("T-1", "first after reset", "human")
	b.inbox("PostToolUse")
	b.comment("T-1", "second after reset", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "second after reset")
}

// Сценарий 5 и спека 3.5/3.6: нет stdin, мусор, иное событие, нет базы, нет личности - пусто, код 0.
func TestInbox_BadInputOrEnvironment_EmptyExitZero(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
		setup func(b *box)
		env   map[string]string // nil - окружение сессии c1
	}{
		{"no stdin", "", nil, nil},
		{"garbage", "this is not json", nil, nil},
		{"json array", `[1,2]`, nil, nil},
		{"other event", hookIn("SessionStart"), nil, nil},
		{"no event", `{"session_id":"c1"}`, nil, nil},
		{"no identity", `{"hook_event_name":"PostToolUse"}`, nil, map[string]string{}},
		{"no base", hookIn("PostToolUse"), func(b *box) { b.home = filepath.Join(b.root, "absent", "tasks") }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newInboxBox(t)
			if c.setup == nil {
				b.hold()
				b.inbox("PostToolUse")
				b.comment("T-1", "pending", "human")
			} else {
				c.setup(b)
			}
			if c.env != nil {
				b.extra = map[string]string{"TRACKER_INBOX_DIR": b.inboxDir()}
			}
			r := b.tk(c.stdin, "inbox", "--hook")
			if r.code != 0 || r.out != "" {
				t.Fatalf("stdin %q: код %d, stdout %q, stderr %q, ждали код 0 и пустой stdout", c.stdin, r.code, r.out, r.err)
			}
		})
	}
}

// Спека 3.1: окружение хука не даёт личность - берётся session_id входа с префиксом claude:.
func TestInbox_NoEnvIdentity_UsesSessionIDFromInput(t *testing.T) {
	defer guard(t)
	b := newInboxBox(t)
	b.hold()
	b.extra = map[string]string{"TRACKER_INBOX_DIR": b.inboxDir()}
	b.inbox("PostToolUse")
	b.comment("T-1", "found by session id", "human")
	wantLine(t, b.inbox("PostToolUse"), "T-1", "human", "found by session id")
}
