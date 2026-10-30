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

// The agent cycle, slice S2.

const critBody = "## Details\n- [ ] one\n- [ ] two\n"

func (b *box) commitCount() int {
	n, _ := strconv.Atoi(strings.TrimSpace(b.git("rev-list", "--count", "HEAD")))
	return n
}

// edit replaces the task body.
func (b *box) edit(id, body string) {
	b.t.Helper()
	rev := strings.TrimPrefix(strings.SplitN(b.ok("", "show", id).out, "\n", 2)[0], "rev ")
	b.ok(body, "edit", id, "--base", rev)
}

// newTask creates a task with three criteria; to - the column.
func (b *box) newTask(title, to string) string { return b.newTaskIn(b.cwd, title, to) }

// newTaskIn is newTask run in the folder cwd; extra - more flags of new (--project).
// A column with an entry check (todo, blocked, done) gets the task through the inbox:
// created, edited, moved.
func (b *box) newTaskIn(cwd, title, to string, extra ...string) string {
	b.t.Helper()
	checked := to == "todo" || to == "blocked" || to == "done"
	into := to
	if checked {
		into = "idea"
	}
	r := b.runIn(cwd, "", append([]string{"new", title, "--to", into}, extra...)...)
	if r.code != 0 {
		b.t.Fatalf("tix new %q: code %d, stderr %q", title, r.code, r.err)
	}
	id := strings.Fields(r.out)[1]
	b.edit(id, critBody)
	if checked {
		args := []string{"move", id, to}
		if to != "todo" {
			args = append(args, "-m", "seed", "--force")
		}
		b.ok("", args...)
	}
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

// Identity by order: --as, the agent environment, TIX_ACTOR; take without one refuses.
func TestCLI_Identity_Order(t *testing.T) {
	defer guard(t)
	cases := []struct {
		name  string
		extra map[string]string
		flag  string
		want  string
	}{
		{"flag wins", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_ACTOR": "ta:1"}, "codex:f1", "codex:f1"},
		{"Claude session", map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_ACTOR": "ta:1"}, "", "claude:c1"},
		{"Codex thread", map[string]string{"CODEX_THREAD_ID": "x1", "TIX_ACTOR": "ta:1"}, "", "codex:x1"},
		{"TIX_ACTOR", map[string]string{"TIX_ACTOR": "opencode:o1"}, "", "opencode:o1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.extra = c.extra
			b.newTask("Work", "todo")
			args := []string{"take", "T-1"}
			if c.flag != "" {
				args = append(args, "--as", c.flag)
			}
			b.ok("", args...)
			if got := b.claimOf("T-1"); !strings.HasPrefix(got, c.want+" until ") {
				t.Errorf("claim %q, want owner %s", got, c.want)
			}
			if got := b.actorTrailer(); got != c.want {
				t.Errorf("Actor trailer %q, want %q", got, c.want)
			}
		})
	}

	b := newBox(t)
	b.newTask("Work", "todo")
	n := b.commitCount()
	for _, args := range [][]string{
		{"take", "T-1"},
		{"take", "T-1", "--as", "unknown"},
		{"take", "T-1", "--as", "a b"},
	} {
		r := b.tix("", args...)
		if r.code != 6 || !oneErrLine(r.err) || !strings.Contains(r.err, "--as") {
			t.Errorf("tix %v: code %d, stderr %q, want code 6 and one line with the --as hint", args, r.code, r.err)
		}
	}
	b.extra = map[string]string{"TIX_ACTOR": "bad|name"}
	if r := b.tix("", "take", "T-1"); r.code != 6 {
		t.Errorf("take with TIX_ACTOR=bad|name: code %d, want 6", r.code)
	}
	b.extra = nil
	if b.commitCount() != n || b.claimOf("T-1") != "" {
		t.Error("a take refusal without identity changed the store")
	}
	// Other commands without identity write unknown.
	b.ok("", "note", "T-1", "no identity")
	if got := b.actorTrailer(); got != "unknown" {
		t.Errorf("note without identity: Actor %q, want unknown", got)
	}
	b.ok("", "note", "T-1", "explicit unknown", "--as", "unknown")
	if got := b.actorTrailer(); got != "unknown" {
		t.Errorf("note --as unknown: Actor %q, want unknown", got)
	}
}

// Another's claim, steal, release, repeat, id without T-, codes 2 4 5 6.
func TestCLI_Take_ForeignClaimStealReleaseCodes(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.newTask("Work", "todo")
	b.ok("", "new", "Idea")
	reply := regexp.MustCompile(`^ok T-1 todo > doing  @\S+ until \d\d:\d\d\n$`)
	if r := b.ok("", "take", "T-1", "--as", "claude:a"); !reply.MatchString(r.out) {
		t.Errorf("take: %q, want `ok T-1 todo > doing  @claude... until HH:MM`", r.out)
	}
	n := b.commitCount()
	if r := b.ok("", "take", "T-1", "--as", "claude:a"); !strings.HasPrefix(r.out, "ok T-1") || b.commitCount() != n {
		t.Errorf("repeated take: %q, commits %d, was %d", r.out, b.commitCount(), n)
	}

	r := b.tix("", "take", "T-1", "--as", "claude:b")
	if r.code != 2 || !oneErrLine(r.err) || !strings.Contains(r.err, "claude:a") || !strings.Contains(r.err, "tix take T-1 --steal") {
		t.Errorf("take of another's: code %d, stderr %q, want code 2 and `claimed by claude:a ... tix take T-1 --steal`", r.code, r.err)
	}
	if b.commitCount() != n {
		t.Error("a take refusal added a commit")
	}
	if r := b.tix("", "take", "T-1", "--release", "--as", "claude:b"); r.code != 2 {
		t.Errorf("release of another's live claim: code %d, want 2", r.code)
	}
	b.ok("", "take", "T-1", "--steal", "--as", "claude:b")
	if subj := strings.TrimSpace(b.git("log", "-1", "--format=%s")); !strings.HasPrefix(subj, "T-1: steal ") {
		t.Errorf("steal in history: subject %q, want `T-1: steal <owner>`", subj)
	}
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:b until ") {
		t.Errorf("after --steal claim %q", got)
	}
	b.ok("", "take", "T-1", "--release", "--as", "claude:b")
	if got := b.claimOf("T-1"); got != "" {
		t.Errorf("after --release claim %q", got)
	}

	for _, c := range []struct {
		args []string
		code int
		why  string
	}{
		{[]string{"take", "T-99", "--as", "claude:a"}, 4, "no task"},
		{[]string{"take", "T-2", "--as", "claude:a"}, 5, "from inbox"},
		{[]string{"take", "foo", "--as", "claude:a"}, 6, "bad id"},
		{[]string{"take", "--as", "claude:a"}, 6, "no id"},
	} {
		r := b.tix("", c.args...)
		if r.code != c.code || r.out != "" || !oneErrLine(r.err) {
			t.Errorf("%s: tix %v: code %d, stdout %q, stderr %q, want code %d and one err line", c.why, c.args, r.code, r.out, r.err, c.code)
		}
	}
	if r := b.tix("", "take", "T-2", "--as", "claude:a"); !strings.Contains(r.err, "tix move T-2") {
		t.Errorf("take from inbox: %q, want the hint `tix move T-2 ...`", r.err)
	}
	// A number without T- expands to T-N.
	b.ok("", "take", "1", "--as", "claude:c")
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:c until ") {
		t.Errorf("`take 1` did not take T-1: claim %q", got)
	}
	if a, c := b.ok("", "show", "1").out, b.ok("", "show", "T-1").out; a != c {
		t.Errorf("`show 1` and `show T-1` differ:\n%s\n---\n%s", a, c)
	}
}

// An expired claim is free and not visible in ctx.
func TestCLI_ExpiredClaim_TakenWithoutSteal(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Work", "--to", "doing")
	b.rewrite("T-1", func(s string) string {
		return strings.Replace(s, "\n---\n", "\nclaim: claude:ghost until 2000-01-01T00:00\n---\n", 1)
	})
	if r := b.ok("", "ctx"); strings.Contains(r.out, "ghost") {
		t.Errorf("expired claim shown in ctx:\n%s", r.out)
	}
	b.ok("", "take", "T-1", "--as", "claude:a")
	if got := b.claimOf("T-1"); !strings.HasPrefix(got, "claude:a until ") {
		t.Errorf("claim after take of an expired one: %q", got)
	}
	if subj := strings.TrimSpace(b.git("log", "-1", "--format=%s")); strings.Contains(subj, "steal") {
		t.Errorf("subject %q: an expired claim is not a steal", subj)
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

// ruleBox - a store with a review column without a role: T-1 in idea without criteria, T-2 in idea with criteria,
// T-3 in doing without criteria.
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
	b.ok("", "note", "T-1", "flush config") // an external edit is committed by the first operation
	return b
}

// rule: the command refuses with code 5 in one line and writes no commit; returns stderr.
func (b *box) rule(args ...string) string {
	b.t.Helper()
	n := b.commitCount()
	r := b.tix("", args...)
	ensure(b.t, r.code == 5 && r.out == "" && oneErrLine(r.err), "tix %v: code %d, stdout %q, stderr %q, want code 5 and one err line", args, r.code, r.out, r.err)
	ensure(b.t, b.commitCount() == n, "tix %v: the refusal added a commit", args)
	return r.err
}

// noCommit: the command passes and writes no commit.
func (b *box) noCommit(args ...string) {
	b.t.Helper()
	n := b.commitCount()
	b.ok("", args...)
	ensure(b.t, b.commitCount() == n, "tix %v: the repeat made a commit", args)
}

// move refusals (D51), waiting (D52), repeat.
func TestCLI_Move_RefusalsWaitingRepeat(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	ensure(t, strings.Contains(b.rule("move", "T-1", "todo"), "criterion"), "move to ready without a criterion")
	ensure(t, strings.Contains(b.rule("move", "T-2", "review"), "review has no role"), "move to a column without a role")
	ensure(t, strings.Contains(b.rule("move", "T-3", "blocked"), "waiting needs a reason"), "move to waiting without a reason")
	b.rule("move", "T-3", "todo", "-m", "text")

	r := b.ok("", "move", "T-2", "todo")
	ensure(t, r.out == "ok T-2 idea > todo\n", "move: %q", r.out)
	b.noCommit("move", "T-2", "todo")
	r = b.ok("", "move", "T-3", "blocked", "-m", "need key")
	ensure(t, r.out == "ok T-3 doing > blocked\n", "move to waiting: %q", r.out)
	ensure(t, strings.Contains(string(mustRead(t, b.taskFile("T-3"))), "need key"), "-m not written to the waiting field")
	b.ok("", "take", "T-3")
	var js struct{ Waiting, Status string }
	_ = json.Unmarshal([]byte(b.ok("", "show", "T-3", "--json").out), &js)
	ensure(t, js.Waiting == "" && js.Status == "doing", "take from waiting: status %q, waiting %q, want doing and empty", js.Status, js.Waiting)
	ensure(t, b.tix("", "move", "T-99", "todo").code == 4, "move of a nonexistent task: want code 4")
}

// set status= refusal; fields; repeat; the value after the first = sign.
func TestCLI_Set_RulesRepeatFirstEquals(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	ensure(t, strings.Contains(b.rule("set", "T-3", "status=done"), "tix move"), "set status=: want the hint `tix move`")
	b.rule("set", "T-3", "title=")
	r := b.ok("", "set", "T-2", "summary=a=b")
	ensure(t, r.out == "ok T-2 summary\n", "set: %q", r.out)
	b.noCommit("set", "T-2", "summary=a=b")
	ensure(t, strings.Contains(string(mustRead(t, b.taskFile("T-2"))), "a=b"), "the value after the first `=` is lost")
}

// check, done: the list of unchecked criteria, --force requires -m, repeats.
func TestCLI_CheckDone_CriteriaForceRepeat(t *testing.T) {
	defer guard(t)
	b := ruleBox(t)
	b.ok("", "move", "T-2", "todo")
	b.ok("", "take", "T-2")
	r := b.ok("", "check", "T-2", "1")
	ensure(t, r.out == "ok T-2 1/2\n", "check: %q", r.out)
	e := b.rule("done", "T-2")
	ensure(t, strings.Contains(e, "1/2 criteria checked") && strings.Contains(e, "tix check T-2 2"), "done with unchecked: %q", e)
	b.rule("done", "T-2", "--force")
	b.ok("", "check", "2", "2") // number without T-
	r = b.ok("", "done", "T-2")
	ensure(t, r.out == "ok T-2 doing > done\n", "done: %q", r.out)
	b.noCommit("done", "T-2")

	ensure(t, strings.Contains(b.rule("done", "T-3"), "no criteria"), "done without criteria and -m")
	b.ok("", "done", "T-3", "-m", "all shipped")
	ensure(t, strings.Contains(b.ok("", "show", "T-3").out, "Result: all shipped"), "the result `Result: all shipped` is not written as a comment")
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

// Order: --first, --after (including in the same column, for the TUI), another column.
func TestCLI_Move_FirstAfterSameColumn(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	for _, title := range []string{"A", "B", "C"} {
		b.ok("", "new", title, "--to", "doing")
	}
	b.ok("", "new", "Foreign") // T-4 in idea
	join := func(ids []string) string { return strings.Join(ids, ",") }
	if got := join(b.columnOrder("doing")); got != "T-1,T-2,T-3" {
		t.Fatalf("initial order %s", got)
	}

	n := b.commitCount()
	b.ok("", "move", "T-3", "doing")
	if b.commitCount() != n || join(b.columnOrder("doing")) != "T-1,T-2,T-3" {
		t.Errorf("move to the same column without a place: commits %d (was %d), order %v", b.commitCount(), n, b.columnOrder("doing"))
	}
	if r := b.ok("", "move", "T-3", "doing", "--first"); !strings.HasPrefix(r.out, "ok T-3") {
		t.Errorf("--first: %q", r.out)
	}
	if b.commitCount() != n+1 || join(b.columnOrder("doing")) != "T-3,T-1,T-2" {
		t.Errorf("--first in the same column: commits %d (want %d), order %v", b.commitCount(), n+1, b.columnOrder("doing"))
	}
	if r := b.ok("", "move", "3", "doing", "--after", "2"); !strings.HasPrefix(r.out, "ok T-3") {
		t.Errorf("--after: %q", r.out)
	}
	if b.commitCount() != n+2 || join(b.columnOrder("doing")) != "T-1,T-2,T-3" {
		t.Errorf("--after 2 (number without T-): commits %d (want %d), order %v", b.commitCount(), n+2, b.columnOrder("doing"))
	}
	r := b.tix("", "move", "T-1", "doing", "--after", "T-4")
	if r.code != 5 || !oneErrLine(r.err) {
		t.Errorf("--after a task from another column: code %d, stderr %q, want code 5", r.code, r.err)
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

// seedProject: project myproj with 7 tasks in ready and 1 in inbox, a task of another project.
func seedProject(t *testing.T) (*box, string) {
	t.Helper()
	b := newBox(t)
	proj := projectDir(t, b, "myproj")
	for i := 1; i <= 7; i++ {
		b.newTaskIn(proj, fmt.Sprintf("Ready %d", i), "todo")
	}
	b.newTaskIn(b.cwd, "Other project", "todo", "--project", "other")
	b.runIn(proj, "", "new", "Idea task") // inbox is not part of ctx
	return b, proj
}

// An empty ~/tasks - one line, a read creates nothing.
func TestCLI_Ctx_EmptyBase_OneLine(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	r := b.runIn(projectDir(t, b, "myproj"), "", "ctx")
	ensure(t, r.code == 0 && r.out == msgNoTasks+"\n" && r.err == "", "ctx in an empty store: code %d, stdout %q, stderr %q, want one line %q", r.code, r.out, r.err, msgNoTasks)
	_, err := os.Stat(filepath.Join(b.home, ".git"))
	ensure(t, err != nil, "ctx created a repository: a read must not write")
}

// 5 lines per column, "2 more", the next task; only the cwd project.
func TestCLI_Ctx_SevenReady_FiveRowsTwoMore(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	head := b.git("rev-parse", "HEAD")
	r := b.runIn(proj, "", "ctx")
	if r.code != 0 {
		t.Fatalf("ctx: code %d, stderr %q", r.code, r.err)
	}
	first := strings.SplitN(r.out, "\n", 2)[0]
	ensure(t, strings.HasPrefix(first, "project myproj") && strings.Contains(first, "rules:") && strings.Contains(first, "RULES.md"),
		"first line %q, want `project myproj   rules: ...RULES.md`", first)
	rows := regexp.MustCompile(`(?m)^todo\s+T-\d+ Ready \d`).FindAllString(r.out, -1)
	ensure(t, len(rows) == 5 && strings.Contains(r.out, "2 more"), "want 5 ready lines and `2 more`, got %d lines:\n%s", len(rows), r.out)
	ensure(t, !strings.Contains(r.out, "Other project") && !strings.Contains(r.out, "Idea task"), "foreign tasks in the project ctx:\n%s", r.out)
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	ensure(t, lines[len(lines)-1] == "next T-1", "last line %q, want `next T-1`", lines[len(lines)-1])
	ensure(t, b.git("rev-parse", "HEAD") == head, "ctx made a commit")
}

// Outside a project - "project all"; --project overrides; --json.
func TestCLI_Ctx_OutsideProjectFlagJSON(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	all := b.ok("", "ctx").out
	ensure(t, strings.HasPrefix(all, "project all"), "ctx outside a project: want `project all`")
	ensure(t, regexp.MustCompile(`(?m)^todo\s+8 more$`).MatchString(all) &&
		regexp.MustCompile(`(?m)^idea\s+1 more$`).MatchString(all) &&
		!strings.Contains(all, "Ready 1") && !strings.Contains(all, "Other project") &&
		strings.Contains(all, "next T-1"), "ctx all should count ready and inbox tasks:\n%s", all)
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
	err = json.Unmarshal([]byte(b.ok("", "ctx", "--json").out), &js)
	ensure(t, err == nil && js.Project == "all" && js.Next == "T-1" && len(js.Sections) == 2,
		"ctx all --json: %v, %+v", err, js)
	for _, s := range js.Sections {
		want := map[string]int{"idea": 1, "todo": 8}[s.Column]
		ensure(t, len(s.Rows) == 0 && s.More == want && want > 0, "ctx all JSON section: %+v", s)
	}
}

// A broken file: warn, code 0, the rest is shown.
func TestCLI_Ctx_BrokenFile_WarnExitZero(t *testing.T) {
	defer guard(t)
	b, proj := seedProject(t)
	broken := filepath.Join(b.home, "T-50-broken.md")
	if err := os.WriteFile(broken, []byte("---\nid: T-50\ntitle: a: b\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := b.runIn(proj, "", "ctx")
	ensure(t, r.code == 0 && strings.Contains(r.out, "Ready 1") && regexp.MustCompile(`^warn .*T-50-broken\.md`).MatchString(r.err),
		"ctx with a broken file: code %d, stderr %q", r.code, r.err)
}

// ctx without git, 2000 chars, a line up to 160, its own claim "@you".
func TestCLI_Ctx_NoGit_LimitsAndOwnClaim(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	long := strings.Repeat("Длинное название задачи ", 10)
	for i := 0; i < 30; i++ {
		b.newTask(fmt.Sprintf("%d %s", i, long), []string{"todo", "doing", "blocked"}[i%3])
	}
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "me1"}
	b.ok("", "take", "T-2") // doing: own
	b.extra["TIX_GIT"] = "/nonexistent/git"
	r := b.tix("", "ctx")
	if r.code != 0 {
		t.Fatalf("ctx without git: code %d, stderr %q", r.code, r.err)
	}
	if n := utf8.RuneCountInString(r.out); n > 2000 {
		t.Errorf("ctx %d runes, limit 2000", n)
	}
	for _, line := range strings.Split(r.out, "\n") {
		if n := utf8.RuneCountInString(line); n > 160 {
			t.Errorf("line %d runes, limit 160: %q", n, line)
		}
	}
	if !strings.Contains(r.out, "@you") {
		t.Errorf("own claim not marked @you:\n%s", r.out)
	}
	b.extra = map[string]string{"CLAUDE_CODE_SESSION_ID": "other"}
	if out := b.ok("", "ctx").out; strings.Contains(out, "@you") || !strings.Contains(out, "me1") {
		t.Errorf("another's claim: want the owner name without @you:\n%s", out)
	}
}

// Any write creates RULES.md if absent; another's edit is not overwritten; a read does not create it.
func TestCLI_RulesFile_CreatedByWriteNeverOverwritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "list")
	b.ok("", "ctx")
	rules := filepath.Join(b.home, "RULES.md")
	if _, err := os.Stat(rules); err == nil {
		t.Error("a read created RULES.md")
	}
	b.ok("", "new", "First")
	data, err := os.ReadFile(rules)
	if err != nil || len(data) == 0 {
		t.Fatalf("the first write did not create a non-empty RULES.md: %v", err)
	}
	custom := "# my rules\n"
	if err := os.WriteFile(rules, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	b.ok("", "new", "Second")
	if got, _ := os.ReadFile(rules); string(got) != custom {
		t.Errorf("the user's RULES.md was overwritten:\n%s", got)
	}
}

func TestCLI_Backup_NoRemote_OneLineHint(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Work")
	r := b.tix("", "backup")
	if r.code == 0 || r.out != "" || !oneErrLine(r.err) || !strings.Contains(r.err, "git remote add origin") {
		t.Errorf("backup without origin: code %d, stdout %q, stderr %q, want an error with the hint `git remote add origin`", r.code, r.out, r.err)
	}
}
