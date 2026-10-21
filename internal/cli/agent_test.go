package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Рабочий цикл агента, З2: docs/plans/z2-agent-cycle.md, разделы 3 и 5.

const critBody = "## Details\n- [ ] one\n- [ ] two\n"

func (b *box) commitCount() int {
	n, _ := strconv.Atoi(strings.TrimSpace(b.git("rev-list", "--count", "HEAD")))
	return n
}

// edit заменяет тело задачи.
func (b *box) edit(id, body string) {
	b.t.Helper()
	rev := strings.TrimPrefix(strings.SplitN(b.ok("", "show", id).out, "\n", 2)[0], "rev ")
	b.ok(body, "edit", id, "--base", rev)
}

// newTask создаёт задачу с тремя критериями; to - колонка.
func (b *box) newTask(title, to string) string {
	b.t.Helper()
	r := b.ok("", "new", title, "--to", to)
	id := strings.Fields(r.out)[1]
	b.edit(id, critBody)
	return id
}

func (b *box) claimOf(id string) string {
	b.t.Helper()
	data, _ := os.ReadFile(b.taskFile(id))
	m := regexp.MustCompile(`(?m)^claim: (.*)$`).FindStringSubmatch(string(data))
	if m == nil {
		return ""
	}
	return strings.Trim(m[1], `"'`)
}

func (b *box) rewrite(id string, f func(string) string) {
	b.t.Helper()
	path := b.taskFile(id)
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(f(string(data))), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) columnOrder(column string) []string {
	b.t.Helper()
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(b.ok("", "list", "--status", column).out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && strings.HasPrefix(f[1], "T-") {
			ids = append(ids, f[1])
		}
	}
	return ids
}

func (b *box) actorTrailer() string {
	b.t.Helper()
	return strings.TrimSpace(b.git("log", "-1", "--format=%(trailers:key=Actor,valueonly)"))
}

// Спека 2.4: личность по порядку --as, окружение агента, TIX_ACTOR; take без неё отказывает.
func TestCLI_Identity_Order(t *testing.T) {
	defer guard(t)
	cases := []struct {
		name  string
		extra map[string]string
		flag  string
		want  string
	}{
		{"флаг главнее", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_ACTOR": "ta:1"}, "codex:f1", "codex:f1"},
		{"сессия Claude", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "CODEX_THREAD_ID": "x1", "TIX_ACTOR": "ta:1"}, "", "claude:c1"},
		{"поток Codex", map[string]string{"CODEX_THREAD_ID": "x1", "TIX_ACTOR": "ta:1"}, "", "codex:x1"},
		{"TIX_ACTOR", map[string]string{"TIX_ACTOR": "opencode:o1"}, "", "opencode:o1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.extra = c.extra
			b.ok("", "new", "Work", "--to", "todo")
			args := []string{"take", "T-1"}
			if c.flag != "" {
				args = append(args, "--as", c.flag)
			}
			b.ok("", args...)
			if got := b.claimOf("T-1"); !strings.HasPrefix(got, c.want+" until ") {
				t.Errorf("claim %q, ждали владельца %s", got, c.want)
			}
			if got := b.actorTrailer(); got != c.want {
				t.Errorf("трейлер Actor %q, ждали %q", got, c.want)
			}
		})
	}

	b := newBox(t)
	b.ok("", "new", "Work", "--to", "todo")
	n := b.commitCount()
	for _, args := range [][]string{
		{"take", "T-1"},
		{"take", "T-1", "--as", "unknown"},
		{"take", "T-1", "--as", "a b"},
	} {
		r := b.tix("", args...)
		if r.code != 6 || !oneErrLine(r.err) || !strings.Contains(r.err, "--as") {
			t.Errorf("tix %v: код %d, stderr %q, ждали код 6 и одну строку с подсказкой --as", args, r.code, r.err)
		}
	}
	b.extra = map[string]string{"TIX_ACTOR": "bad|name"}
	if r := b.tix("", "take", "T-1"); r.code != 6 {
		t.Errorf("take с TIX_ACTOR=bad|name: код %d, ждали 6", r.code)
	}
	b.extra = nil
	if b.commitCount() != n || b.claimOf("T-1") != "" {
		t.Error("отказ take без личности изменил базу")
	}
	// Прочие команды без личности пишут unknown.
	b.ok("", "note", "T-1", "no identity")
	if got := b.actorTrailer(); got != "unknown" {
		t.Errorf("note без личности: Actor %q, ждали unknown", got)
	}
	b.ok("", "note", "T-1", "explicit unknown", "--as", "unknown")
	if got := b.actorTrailer(); got != "unknown" {
		t.Errorf("note --as unknown: Actor %q, ждали unknown", got)
	}
}

// Раздел 3: чужой захват, перехват, снятие, повтор, id без T-, коды 2 4 5 6.
func TestCLI_Take_ForeignClaimStealReleaseCodes(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Work", "--to", "todo")
	b.ok("", "new", "Idea")
	reply := regexp.MustCompile(`^ok T-1 todo > doing  @\S+ until \d\d:\d\d\n$`)
	if r := b.ok("", "take", "T-1", "--as", "claude:a"); !reply.MatchString(r.out) {
		t.Errorf("take: %q, ждали `ok T-1 todo > doing  @claude... until HH:MM`", r.out)
	}
	n := b.commitCount()
	if r := b.ok("", "take", "T-1", "--as", "claude:a"); !strings.HasPrefix(r.out, "ok T-1") || b.commitCount() != n {
		t.Errorf("повторный take: %q, коммитов %d, было %d", r.out, b.commitCount(), n)
	}

	r := b.tix("", "take", "T-1", "--as", "claude:b")
	if r.code != 2 || !oneErrLine(r.err) || !strings.Contains(r.err, "claude:a") || !strings.Contains(r.err, "tix take T-1 --steal") {
		t.Errorf("take чужого: код %d, stderr %q, ждали код 2 и `claimed by claude:a ... tix take T-1 --steal`", r.code, r.err)
	}
	if b.commitCount() != n {
		t.Error("отказ take добавил коммит")
	}
	if r := b.tix("", "take", "T-1", "--release", "--as", "claude:b"); r.code != 2 {
		t.Errorf("release чужого живого claim: код %d, ждали 2", r.code)
	}
	b.ok("", "take", "T-1", "--steal", "--as", "claude:b")
	if subj := strings.TrimSpace(b.git("log", "-1", "--format=%s")); !strings.HasPrefix(subj, "T-1: steal ") {
		t.Errorf("перехват в истории: тема %q, ждали `T-1: steal <владелец>`", subj)
	}
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:b until ") {
		t.Errorf("после --steal claim %q", got)
	}
	b.ok("", "take", "T-1", "--release", "--as", "claude:b")
	if got := b.claimOf("T-1"); got != "" {
		t.Errorf("после --release claim %q", got)
	}

	for _, c := range []struct {
		args []string
		code int
		why  string
	}{
		{[]string{"take", "T-99", "--as", "claude:a"}, 4, "нет задачи"},
		{[]string{"take", "T-2", "--as", "claude:a"}, 5, "из inbox"},
		{[]string{"take", "foo", "--as", "claude:a"}, 6, "плохой id"},
		{[]string{"take", "--as", "claude:a"}, 6, "нет id"},
	} {
		r := b.tix("", c.args...)
		if r.code != c.code || r.out != "" || !oneErrLine(r.err) {
			t.Errorf("%s: tix %v: код %d, stdout %q, stderr %q, ждали код %d и одну строку err", c.why, c.args, r.code, r.out, r.err, c.code)
		}
	}
	if r := b.tix("", "take", "T-2", "--as", "claude:a"); !strings.Contains(r.err, "tix move T-2") {
		t.Errorf("take из inbox: %q, ждали подсказку `tix move T-2 ...`", r.err)
	}
	// Номер без T- разворачивается в T-N.
	b.ok("", "take", "1", "--as", "claude:c")
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:c until ") {
		t.Errorf("`take 1` не взял T-1: claim %q", got)
	}
	if a, c := b.ok("", "show", "1").out, b.ok("", "show", "T-1").out; a != c {
		t.Errorf("`show 1` и `show T-1` различаются:\n%s\n---\n%s", a, c)
	}
}

// Раздел 3: истёкший claim свободен и в ctx не виден.
func TestCLI_ExpiredClaim_TakenWithoutSteal(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Work", "--to", "doing")
	b.rewrite("T-1", func(s string) string {
		return strings.Replace(s, "\n---\n", "\nclaim: claude:ghost until 2000-01-01T00:00\n---\n", 1)
	})
	if r := b.ok("", "ctx"); strings.Contains(r.out, "ghost") {
		t.Errorf("истёкший claim показан в ctx:\n%s", r.out)
	}
	b.ok("", "take", "T-1", "--as", "claude:a")
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:a until ") {
		t.Errorf("claim после take истёкшего: %q", got)
	}
	if subj := strings.TrimSpace(b.git("log", "-1", "--format=%s")); strings.Contains(subj, "steal") {
		t.Errorf("тема %q: истёкший claim не перехват", subj)
	}
}

const cliReviewConfig = "[[column]]\nname = \"idea\"\nrole = \"inbox\"\n\n[[column]]\nname = \"todo\"\nrole = \"ready\"\n\n" +
	"[[column]]\nname = \"review\"\n\n[[column]]\nname = \"doing\"\nrole = \"active\"\n\n" +
	"[[column]]\nname = \"blocked\"\nrole = \"waiting\"\n\n[[column]]\nname = \"done\"\nrole = \"closed\"\n"

func ensure(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

// ruleBox - база с колонкой review без роли: T-1 в idea без критериев, T-2 в idea с критериями,
// T-3 в doing без критериев.
func ruleBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "s1"}
	b.ok("", "new", "Bare")
	b.newTask("Crit", "idea")
	b.ok("", "new", "Doing", "--to", "doing")
	cfg := filepath.Join(b.home, ".tix", "config.toml")
	if err := os.WriteFile(cfg, []byte(cliReviewConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	b.ok("", "note", "T-1", "flush config") // правка в обход ядра коммитится первой операцией
	return b
}

// rule: команда отказывает кодом 5 одной строкой и не пишет коммит; возвращает stderr.
func (b *box) rule(args ...string) string {
	b.t.Helper()
	n := b.commitCount()
	r := b.tix("", args...)
	ensure(b.t, r.code == 5 && r.out == "" && oneErrLine(r.err), "tix %v: код %d, stdout %q, stderr %q, ждали код 5 и одну строку err", args, r.code, r.out, r.err)
	ensure(b.t, b.commitCount() == n, "tix %v: отказ добавил коммит", args)
	return r.err
}

// noCommit: команда проходит и не пишет коммит.
func (b *box) noCommit(args ...string) {
	b.t.Helper()
	n := b.commitCount()
	b.ok("", args...)
	ensure(b.t, b.commitCount() == n, "tix %v: повтор сделал коммит", args)
}

// Раздел 3: move отказы (Р51), waiting (Р52), повтор.
func TestCLI_Move_RefusalsWaitingRepeat(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	ensure(t, strings.Contains(b.rule("move", "T-1", "todo"), "criterion"), "move в ready без критерия")
	ensure(t, strings.Contains(b.rule("move", "T-2", "review"), "review has no role"), "move в колонку без роли")
	ensure(t, strings.Contains(b.rule("move", "T-3", "blocked"), "waiting needs a reason"), "move в waiting без причины")
	b.rule("move", "T-3", "todo", "-m", "text")

	r := b.ok("", "move", "T-2", "todo")
	ensure(t, r.out == "ok T-2 idea > todo\n", "move: %q", r.out)
	b.noCommit("move", "T-2", "todo")
	r = b.ok("", "move", "T-3", "blocked", "-m", "need key")
	ensure(t, r.out == "ok T-3 doing > blocked\n", "move в waiting: %q", r.out)
	ensure(t, strings.Contains(string(mustRead(t, b.taskFile("T-3"))), "need key"), "-m не записан в поле waiting")
	b.ok("", "take", "T-3")
	var js struct{ Waiting, Status string }
	_ = json.Unmarshal([]byte(b.ok("", "show", "T-3", "--json").out), &js)
	ensure(t, js.Waiting == "" && js.Status == "doing", "take из waiting: status %q, waiting %q, ждали doing и пусто", js.Status, js.Waiting)
	ensure(t, b.tix("", "move", "T-99", "todo").code == 4, "move несуществующей: ждали код 4")
}

// Раздел 3: set status= отказ; поля; повтор; значение после первого знака =.
func TestCLI_Set_RulesRepeatFirstEquals(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	ensure(t, strings.Contains(b.rule("set", "T-3", "status=done"), "tix move"), "set status=: ждали подсказку `tix move`")
	b.rule("set", "T-3", "title=")
	r := b.ok("", "set", "T-2", "summary=a=b")
	ensure(t, r.out == "ok T-2 summary\n", "set: %q", r.out)
	b.noCommit("set", "T-2", "summary=a=b")
	ensure(t, strings.Contains(string(mustRead(t, b.taskFile("T-2"))), "a=b"), "значение после первого `=` потеряно")
}

// Раздел 3: check, done: перечень неотмеченных, --force требует -m, повторы.
func TestCLI_CheckDone_CriteriaForceRepeat(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	b.ok("", "move", "T-2", "todo")
	b.ok("", "take", "T-2")
	r := b.ok("", "check", "T-2", "1")
	ensure(t, r.out == "ok T-2 1/2\n", "check: %q", r.out)
	e := b.rule("done", "T-2")
	ensure(t, strings.Contains(e, "1/2 criteria checked") && strings.Contains(e, "tix check T-2 2"), "done с неотмеченными: %q", e)
	b.rule("done", "T-2", "--force")
	b.ok("", "check", "2", "2") // номер без T-
	r = b.ok("", "done", "T-2")
	ensure(t, r.out == "ok T-2 doing > done\n", "done: %q", r.out)
	b.noCommit("done", "T-2")

	ensure(t, strings.Contains(b.rule("done", "T-3"), "no criteria"), "done без критериев и -m")
	b.ok("", "done", "T-3", "-m", "all shipped")
	ensure(t, strings.Contains(b.ok("", "show", "T-3").out, "Result: all shipped"), "итог `Result: all shipped` не записан комментарием")
	b.noCommit("done", "T-3", "-m", "all shipped")
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Раздел 3, порядок: --first, --after (в т.ч. в той же колонке, для TUI), чужая колонка.
func TestCLI_Move_FirstAfterSameColumn(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	for _, title := range []string{"A", "B", "C"} {
		b.ok("", "new", title, "--to", "doing")
	}
	b.ok("", "new", "Foreign") // T-4 в idea
	join := func(ids []string) string { return strings.Join(ids, ",") }
	if got := join(b.columnOrder("doing")); got != "T-1,T-2,T-3" {
		t.Fatalf("исходный порядок %s", got)
	}

	n := b.commitCount()
	b.ok("", "move", "T-3", "doing")
	if b.commitCount() != n || join(b.columnOrder("doing")) != "T-1,T-2,T-3" {
		t.Errorf("move в ту же колонку без места: коммитов %d (было %d), порядок %v", b.commitCount(), n, b.columnOrder("doing"))
	}
	if r := b.ok("", "move", "T-3", "doing", "--first"); !strings.HasPrefix(r.out, "ok T-3") {
		t.Errorf("--first: %q", r.out)
	}
	if b.commitCount() != n+1 || join(b.columnOrder("doing")) != "T-3,T-1,T-2" {
		t.Errorf("--first в той же колонке: коммитов %d (ждали %d), порядок %v", b.commitCount(), n+1, b.columnOrder("doing"))
	}
	if r := b.ok("", "move", "3", "doing", "--after", "2"); !strings.HasPrefix(r.out, "ok T-3") {
		t.Errorf("--after: %q", r.out)
	}
	if b.commitCount() != n+2 || join(b.columnOrder("doing")) != "T-1,T-2,T-3" {
		t.Errorf("--after 2 (номер без T-): коммитов %d (ждали %d), порядок %v", b.commitCount(), n+2, b.columnOrder("doing"))
	}
	r := b.tix("", "move", "T-1", "doing", "--after", "T-4")
	if r.code != 5 || !oneErrLine(r.err) {
		t.Errorf("--after задачи из другой колонки: код %d, stderr %q, ждали код 5", r.code, r.err)
	}
}

func projectDir(t *testing.T, b *box, name string) string {
	t.Helper()
	dir := filepath.Join(b.root, "dev", name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// seedProject: проект myproj с 7 задачами в ready и 1 в inbox, задача другого проекта.
func seedProject(t *testing.T) (*box, string) {
	t.Helper()
	b := newBox(t)
	proj := projectDir(t, b, "myproj")
	for i := 1; i <= 7; i++ {
		b.runIn(proj, "", "new", fmt.Sprintf("Ready %d", i), "--to", "todo")
	}
	b.ok("", "new", "Other project", "--project", "other", "--to", "todo")
	b.runIn(proj, "", "new", "Idea task") // inbox в ctx не входит
	return b, proj
}

// Раздел 3: пустой ~/tasks - одна строка, чтение ничего не создаёт.
func TestCLI_Ctx_EmptyBase_OneLine(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	r := b.runIn(projectDir(t, b, "myproj"), "", "ctx")
	ensure(t, r.code == 0 && r.out == msgNoTasks+"\n" && r.err == "", "ctx в пустой базе: код %d, stdout %q, stderr %q, ждали одну строку %q", r.code, r.out, r.err, msgNoTasks)
	_, err := os.Stat(filepath.Join(b.home, ".git"))
	ensure(t, err != nil, "ctx создал репозиторий: чтение не должно писать")
}

// Спека 2.2: 5 строк на колонку, "2 more", следующая задача; только проект cwd.
func TestCLI_Ctx_SevenReady_FiveRowsTwoMore(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	head := b.git("rev-parse", "HEAD")
	r := b.runIn(proj, "", "ctx")
	if r.code != 0 {
		t.Fatalf("ctx: код %d, stderr %q", r.code, r.err)
	}
	first := strings.SplitN(r.out, "\n", 2)[0]
	ensure(t, strings.HasPrefix(first, "project myproj") && strings.Contains(first, "rules:") && strings.Contains(first, "RULES.md"),
		"первая строка %q, ждали `project myproj   rules: ...RULES.md`", first)
	rows := regexp.MustCompile(`(?m)^todo\s+T-\d+ Ready \d`).FindAllString(r.out, -1)
	ensure(t, len(rows) == 5 && strings.Contains(r.out, "2 more"), "ждали 5 строк ready и `2 more`, строк %d:\n%s", len(rows), r.out)
	ensure(t, !strings.Contains(r.out, "Other project") && !strings.Contains(r.out, "Idea task"), "в ctx проекта чужое:\n%s", r.out)
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	ensure(t, lines[len(lines)-1] == "next T-1", "последняя строка %q, ждали `next T-1`", lines[len(lines)-1])
	ensure(t, b.git("rev-parse", "HEAD") == head, "ctx сделал коммит")
}

// Вне проекта - "project all"; --project перекрывает; --json.
func TestCLI_Ctx_OutsideProjectFlagJSON(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	ensure(t, strings.HasPrefix(b.ok("", "ctx").out, "project all"), "ctx вне проекта: ждали `project all`")
	out := b.ok("", "ctx", "--project", "other").out
	ensure(t, strings.Contains(out, "Other project") && !strings.Contains(out, "Ready 1"), "ctx --project other:\n%s", out)
	var js struct {
		Project, Next string
		Sections      []struct {
			Column string
			Rows   []struct{ ID string }
			More   int
		}
	}
	err := json.Unmarshal([]byte(b.runIn(proj, "", "ctx", "--json").out), &js)
	ensure(t, err == nil && js.Project == "myproj" && js.Next == "T-1" && len(js.Sections) > 0, "ctx --json: %v, %+v", err, js)
}

// Битый файл: warn, код 0, остальное показано.
func TestCLI_Ctx_BrokenFile_WarnExitZero(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	broken := filepath.Join(b.home, "T-50-broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: T-50\ntitle: a: b\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.runIn(proj, "", "ctx")
	ensure(t, r.code == 0 && strings.Contains(r.out, "Ready 1") && regexp.MustCompile(`^warn .*T-50-broken\.md`).MatchString(r.err),
		"ctx с битым файлом: код %d, stderr %q", r.code, r.err)
}

// Спека 2.2: ctx без git, 2000 знаков, строка до 160, свой claim "@you".
func TestCLI_Ctx_NoGit_LimitsAndOwnClaim(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	long := strings.Repeat("Длинное название задачи ", 10)
	for i := 0; i < 30; i++ {
		b.ok("", "new", fmt.Sprintf("%d %s", i, long), "--to", []string{"todo", "doing", "blocked"}[i%3])
	}
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "me1"}
	b.ok("", "take", "T-2") // doing: своя
	b.extra["TIX_GIT"] = "/nonexistent/git"
	r := b.tix("", "ctx")
	if r.code != 0 {
		t.Fatalf("ctx без git: код %d, stderr %q", r.code, r.err)
	}
	if n := utf8.RuneCountInString(r.out); n > 2000 {
		t.Errorf("ctx %d рун, предел 2000", n)
	}
	for _, line := range strings.Split(r.out, "\n") {
		if n := utf8.RuneCountInString(line); n > 160 {
			t.Errorf("строка %d рун, предел 160: %q", n, line)
		}
	}
	if !strings.Contains(r.out, "@you") {
		t.Errorf("свой claim не помечен @you:\n%s", r.out)
	}
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "other"}
	if out := b.ok("", "ctx").out; strings.Contains(out, "@you") || !strings.Contains(out, "me1") {
		t.Errorf("чужой claim: ждали имя владельца без @you:\n%s", out)
	}
}

// Любая запись создаёт RULES.md, если нет; чужую правку не затирает; чтение не создаёт.
func TestCLI_RulesFile_CreatedByWriteNeverOverwritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "list")
	b.ok("", "ctx")
	rules := filepath.Join(b.home, "RULES.md")
	if _, err := os.Stat(rules); err == nil {
		t.Error("чтение создало RULES.md")
	}
	b.ok("", "new", "First")
	data, err := os.ReadFile(rules)
	if err != nil || len(data) == 0 {
		t.Fatalf("первая запись не создала непустой RULES.md: %v", err)
	}
	custom := "# my rules\n"
	if err := os.WriteFile(rules, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	b.ok("", "new", "Second")
	if got, _ := os.ReadFile(rules); string(got) != custom {
		t.Errorf("RULES.md пользователя перезаписан:\n%s", got)
	}
}

func TestCLI_Backup_NoRemote_OneLineHint(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Work")
	r := b.tix("", "backup")
	if r.code == 0 || r.out != "" || !oneErrLine(r.err) || !strings.Contains(r.err, "git remote add origin") {
		t.Errorf("backup без origin: код %d, stdout %q, stderr %q, ждали ошибку с подсказкой `git remote add origin`", r.code, r.out, r.err)
	}
}
