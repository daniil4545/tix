package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Рабочий цикл агента, З2: docs/plans/z2-agent-cycle.md, разделы 3 и 8.

// gitIn запускает git в произвольной папке с тем же окружением, что и tk.
func (b *box) gitIn(dir string, args ...string) string {
	b.t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	c := exec.CommandContext(context.Background(), gitBin, full...)
	c.Env = b.env
	out, err := c.CombinedOutput()
	if err != nil {
		b.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// project создаёт папку проекта с настоящим репозиторием; withCommit - есть ли коммит.
func (b *box) project(name string, withCommit bool) string {
	b.t.Helper()
	dir := filepath.Join(b.root, "dev", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.t.Fatal(err)
	}
	b.gitIn(dir, "init", "-q")
	if withCommit {
		b.gitIn(dir, "commit", "-q", "--allow-empty", "-m", "init")
	}
	return dir
}

func (b *box) trailer(key string) string {
	b.t.Helper()
	return strings.TrimSpace(b.git("log", "-1", "--format=%(trailers:key="+key+",valueonly)"))
}

func (b *box) branchHead(gitDir, branch string) string {
	c := exec.CommandContext(context.Background(), gitBin, "-C", gitDir, "rev-parse", "refs/heads/"+branch)
	c.Env = b.env
	out, _ := c.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// Спека 3, happy path: хук отдал ctx; take, note, check, set summary=, comment, done -m.
func TestScenario_HappyPath_AgentCycle(t *testing.T) {
	b := newBox(t)
	b.cwd = b.project("myproj", true)
	b.env = b.with("CLAUDE_CODE_SESSION_ID=happy-1")

	b.ok("", "new", "Fix parser", "--to", "todo")
	b.ok("## Details\n- [ ] reproduce\n- [ ] fix\n", "edit", "T-1", "--base", b.showRev("T-1"))

	ctx := b.ok("", "ctx").out
	if !strings.HasPrefix(ctx, "project myproj") || !regexp.MustCompile(`(?m)^todo\s+T-1 Fix parser`).MatchString(ctx) ||
		!strings.HasSuffix(strings.TrimSpace(ctx), "next T-1") {
		t.Fatalf("ctx перед работой:\n%s", ctx)
	}
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"take", "T-1"}, `^ok T-1 todo > doing  @\S+ until \d\d:\d\d\n$`},
		{[]string{"note", "T-1", "parser located"}, `^ok T-1 note\n$`},
		{[]string{"check", "T-1", "1", "2"}, "^ok T-1 2/2\n$"},
		{[]string{"set", "T-1", "summary=Fixed the parser"}, "^ok T-1 summary\n$"},
		{[]string{"comment", "T-1", "done and verified"}, "^ok T-1 comment\n$"},
		{[]string{"done", "T-1", "-m", "merged"}, "^ok T-1 doing > done\n$"},
	}
	for _, s := range steps {
		r := b.tk("", s.args...)
		if r.code != 0 || !regexp.MustCompile(s.want).MatchString(r.out) {
			t.Fatalf("tk %v: код %d, stdout %q, stderr %q, ждали %s", s.args, r.code, r.out, r.err, s.want)
		}
	}

	b.checkHistory([]string{"T-1: new", "T-1: edit", "T-1: take", "T-1: note", "T-1: check", "T-1: set", "T-1: comment", "T-1: done"})
	show := b.ok("", "show", "T-1").out
	for _, want := range []string{"status: done", "- [x] reproduce", "- [x] fix", "Fixed the parser", "Result: merged", "done and verified"} {
		if !strings.Contains(show, want) {
			t.Errorf("в итоговом файле нет %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "claim") {
		t.Errorf("закрытая задача сохранила ключ claim:\n%s", show)
	}
}

// checkHistory: темы коммитов идут в этом порядке; у шагов после edit автор из окружения.
func (b *box) checkHistory(prefixes []string) {
	b.t.Helper()
	commits := b.commits()
	at := 0
	for _, c := range commits {
		if at < len(prefixes) && strings.HasPrefix(c.Subject, prefixes[at]) {
			if at >= 2 && c.Actor != "claude:happy-1" {
				b.t.Errorf("коммит %q: Actor %q, ждали claude:happy-1", c.Subject, c.Actor)
			}
			at++
		}
	}
	if at != len(prefixes) {
		var subs []string
		for _, c := range commits {
			subs = append(subs, c.Subject)
		}
		b.t.Errorf("в истории нет шага %q; темы: %v", prefixes[at], subs)
	}
}

// Раздел 3: гонка 5 процессов take - один ok, остальные код 2, без --steal.
func TestScenario_FiveTakesRace_OneWinner(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Contended", "--to", "todo")
	codes := make([]int, 5)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = b.tk("", "take", "T-1", "--as", fmt.Sprintf("claude:r%d", i)).code
		}()
	}
	wg.Wait()
	wins, winner := 0, -1
	for i, c := range codes {
		switch c {
		case 0:
			wins++
			winner = i
		case 2:
		default:
			t.Errorf("процесс %d: код %d, ждали 0 или 2", i, c)
		}
	}
	if wins != 1 {
		t.Fatalf("победителей %d (коды %v), ждали ровно одного", wins, codes)
	}
	if got := b.read(b.taskFile("T-1")); !strings.Contains(got, fmt.Sprintf("claim: claude:r%d until", winner)) {
		t.Errorf("владелец не победитель r%d:\n%s", winner, got)
	}
	takes := 0
	for _, c := range b.commits() {
		if c.Subject == "T-1: take" {
			takes++
		}
	}
	if takes != 1 {
		t.Errorf("коммитов take %d, ждали 1", takes)
	}
	b.fsck()
}

// Раздел 8 (Р58): трейлер Code: в проекте, вне проекта, в проекте без коммитов.
func TestScenario_CodeTrailer_InsideOutsideNoCommits(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Work")
	proj := b.project("myproj", true)
	empty := b.project("fresh", false)

	b.cwd = proj
	b.ok("", "note", "T-1", "inside", "--as", "claude:a")
	sha := b.gitIn(proj, "rev-parse", "HEAD")
	if got := b.trailer("Code"); got != "myproj@"+sha {
		t.Errorf("в проекте: Code %q, ждали %q", got, "myproj@"+sha)
	}
	if got := b.trailer("Actor"); got != "claude:a" {
		t.Errorf("Actor %q рядом с Code, ждали claude:a", got)
	}
	b.gitIn(proj, "commit", "-q", "--allow-empty", "-m", "second")
	b.ok("", "note", "T-1", "after new commit", "--as", "claude:a")
	if got, want := b.trailer("Code"), "myproj@"+b.gitIn(proj, "rev-parse", "HEAD"); got != want {
		t.Errorf("после нового коммита проекта: Code %q, ждали %q", got, want)
	}

	b.cwd = filepath.Join(b.root, "work")
	b.ok("", "note", "T-1", "outside", "--as", "claude:a")
	if got := b.trailer("Code"); got != "" {
		t.Errorf("вне проекта: Code %q, ждали без трейлера", got)
	}

	b.cwd = empty
	r := b.tk("", "note", "T-1", "no commits", "--as", "claude:a")
	if r.code != 0 {
		t.Fatalf("проект без коммитов: код %d, stderr %q", r.code, r.err)
	}
	if got := b.trailer("Code"); got != "" {
		t.Errorf("проект без коммитов: Code %q, ждали без трейлера", got)
	}
}

// Раздел 3: backup в локальный bare, правка мимо ядра, битый путь, адрес с паролем, нет базы.
func TestScenario_Backup_BareExternalEditBrokenRemote(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Work")
	bare := filepath.Join(b.root, "remote.git")
	if out, err := exec.CommandContext(context.Background(), gitBin, "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init --bare: %v\n%s", err, out)
	}
	b.git("remote", "add", "origin", bare)
	branch := strings.TrimSpace(b.git("symbolic-ref", "--short", "HEAD"))

	if r := b.tk("", "backup"); r.code != 0 || r.out != "ok backup origin\n" {
		t.Fatalf("backup: код %d, stdout %q, stderr %q, ждали `ok backup origin`", r.code, r.out, r.err)
	}
	if got, want := b.branchHead(bare, branch), strings.TrimSpace(b.git("rev-parse", "HEAD")); got != want {
		t.Fatalf("HEAD bare %q, локальный %q", got, want)
	}

	// Правка мимо ядра попадает в backup.
	f := b.taskFile("T-1")
	b.write(f, b.read(f)+"edited in Obsidian\n")
	b.ok("", "backup")
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("после backup дерево не чисто:\n%s", st)
	}
	if got, want := b.branchHead(bare, branch), strings.TrimSpace(b.git("rev-parse", "HEAD")); got != want {
		t.Errorf("после внешней правки HEAD bare %q, локальный %q", got, want)
	}

	oneErr := regexp.MustCompile(`^err .+\n$`)
	b.git("remote", "set-url", "origin", filepath.Join(b.root, "no", "such.git"))
	if r := b.tk("", "backup"); r.code != 1 || r.out != "" || !oneErr.MatchString(r.err) {
		t.Errorf("битый путь: код %d, stdout %q, stderr %q, ждали код 1 и одну строку err", r.code, r.out, r.err)
	}
	b.git("remote", "set-url", "origin", "http://user:pw123@127.0.0.1:1/x.git")
	start := time.Now()
	r := b.tk("", "backup")
	if r.code != 1 || !oneErr.MatchString(r.err) || strings.Contains(r.err, "pw123") {
		t.Errorf("адрес с паролем: код %d, stderr %q, ждали код 1, одну строку без пароля", r.code, r.err)
	}
	if time.Since(start) > 40*time.Second {
		t.Errorf("backup ждал %v: нужен таймаут 30 с и без запроса пароля", time.Since(start))
	}

	nobase := b.runEnv(b.with("TRACKER_HOME="+filepath.Join(b.root, "missing")), "", "backup")
	if nobase.code == 0 || !oneErr.MatchString(nobase.err) {
		t.Errorf("backup без базы: код %d, stderr %q, ждали ошибку одной строкой", nobase.code, nobase.err)
	}
}
