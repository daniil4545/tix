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

// Agent cycle: the core scenarios, the agent works through the tix CLI.

// gitIn runs git in an arbitrary folder with the same environment as tix.
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

// project creates a project folder with a real repository; withCommit - whether there is a commit.
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

// Spec 3, happy path: the hook gave ctx; take, note, check, set summary=, comment, done -m.
func TestScenario_HappyPath_AgentCycle(t *testing.T) {
	b := newBox(t)
	b.cwd = b.project("myproj", true)
	b.env = b.with("CLAUDE_CODE_SESSION_ID=happy-1")

	b.ok("", "new", "Fix parser", "--to", "todo")
	b.ok("## Details\n- [ ] reproduce\n- [ ] fix\n", "edit", "T-1", "--base", b.showRev("T-1"))

	ctx := b.ok("", "ctx").out
	if !strings.HasPrefix(ctx, "project myproj") || !regexp.MustCompile(`(?m)^todo\s+T-1 Fix parser`).MatchString(ctx) ||
		!strings.HasSuffix(strings.TrimSpace(ctx), "next T-1") {
		t.Fatalf("ctx before work:\n%s", ctx)
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
		r := b.tix("", s.args...)
		if r.code != 0 || !regexp.MustCompile(s.want).MatchString(r.out) {
			t.Fatalf("tix %v: code %d, stdout %q, stderr %q, want %s", s.args, r.code, r.out, r.err, s.want)
		}
	}

	b.checkHistory([]string{"T-1: new", "T-1: edit", "T-1: take", "T-1: note", "T-1: check", "T-1: set", "T-1: comment", "T-1: done"})
	show := b.ok("", "show", "T-1").out
	for _, want := range []string{"status: done", "- [x] reproduce", "- [x] fix", "Fixed the parser", "Result: merged", "done and verified"} {
		if !strings.Contains(show, want) {
			t.Errorf("the final file lacks %q:\n%s", want, show)
		}
	}
	if strings.Contains(show, "claim") {
		t.Errorf("the closed task kept the claim key:\n%s", show)
	}
}

// checkHistory: commit subjects go in this order; the steps after edit have the author from the environment.
func (b *box) checkHistory(prefixes []string) {
	b.t.Helper()
	commits := b.commits()
	at := 0
	for _, c := range commits {
		if at < len(prefixes) && strings.HasPrefix(c.Subject, prefixes[at]) {
			if at >= 2 && c.Actor != "claude:happy-1" {
				b.t.Errorf("commit %q: Actor %q, want claude:happy-1", c.Subject, c.Actor)
			}
			at++
		}
	}
	if at != len(prefixes) {
		var subs []string
		for _, c := range commits {
			subs = append(subs, c.Subject)
		}
		b.t.Errorf("the history lacks step %q; subjects: %v", prefixes[at], subs)
	}
}

// A race of 5 take processes - one ok, the others code 2, without --steal.
func TestScenario_FiveTakesRace_OneWinner(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Contended", "--to", "todo")
	codes := make([]int, 5)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = b.tix("", "take", "T-1", "--as", fmt.Sprintf("claude:r%d", i)).code
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
			t.Errorf("process %d: code %d, want 0 or 2", i, c)
		}
	}
	if wins != 1 {
		t.Fatalf("winners %d (codes %v), want exactly one", wins, codes)
	}
	if got := b.read(b.taskFile("T-1")); !strings.Contains(got, fmt.Sprintf("claim: claude:r%d until", winner)) {
		t.Errorf("the owner is not the winner r%d:\n%s", winner, got)
	}
	takes := 0
	for _, c := range b.commits() {
		if c.Subject == "T-1: take" {
			takes++
		}
	}
	if takes != 1 {
		t.Errorf("take commits %d, want 1", takes)
	}
	b.fsck()
}

// The Code: trailer (D58): in a project, outside a project, in a project without commits.
func TestScenario_CodeTrailer_InsideOutsideNoCommits(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Work")
	proj := b.project("myproj", true)
	empty := b.project("fresh", false)

	b.cwd = proj
	b.ok("", "note", "T-1", "inside", "--as", "claude:a")
	sha := b.gitIn(proj, "rev-parse", "HEAD")
	if got := b.trailer("Code"); got != "myproj@"+sha {
		t.Errorf("in a project: Code %q, want %q", got, "myproj@"+sha)
	}
	if got := b.trailer("Actor"); got != "claude:a" {
		t.Errorf("Actor %q next to Code, want claude:a", got)
	}
	b.gitIn(proj, "commit", "-q", "--allow-empty", "-m", "second")
	b.ok("", "note", "T-1", "after new commit", "--as", "claude:a")
	if got, want := b.trailer("Code"), "myproj@"+b.gitIn(proj, "rev-parse", "HEAD"); got != want {
		t.Errorf("after a new project commit: Code %q, want %q", got, want)
	}

	b.cwd = filepath.Join(b.root, "work")
	b.ok("", "note", "T-1", "outside", "--as", "claude:a")
	if got := b.trailer("Code"); got != "" {
		t.Errorf("outside a project: Code %q, want no trailer", got)
	}

	b.cwd = empty
	r := b.tix("", "note", "T-1", "no commits", "--as", "claude:a")
	if r.code != 0 {
		t.Fatalf("project without commits: code %d, stderr %q", r.code, r.err)
	}
	if got := b.trailer("Code"); got != "" {
		t.Errorf("project without commits: Code %q, want no trailer", got)
	}
}

// backup to a local bare, an external edit, a broken path, an address with a password, no store.
func TestScenario_Backup_BareExternalEditBrokenRemote(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Work")
	bare := filepath.Join(b.root, "remote.git")
	if out, err := exec.CommandContext(context.Background(), gitBin, "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init --bare: %v\n%s", err, out)
	}
	b.git("remote", "add", "origin", bare)
	branch := strings.TrimSpace(b.git("symbolic-ref", "--short", "HEAD"))

	if r := b.tix("", "backup"); r.code != 0 || r.out != "ok backup origin\n" {
		t.Fatalf("backup: code %d, stdout %q, stderr %q, want `ok backup origin`", r.code, r.out, r.err)
	}
	if got, want := b.branchHead(bare, branch), strings.TrimSpace(b.git("rev-parse", "HEAD")); got != want {
		t.Fatalf("bare HEAD %q, local %q", got, want)
	}

	// An external edit gets into the backup.
	f := b.taskFile("T-1")
	b.write(f, b.read(f)+"edited in Obsidian\n")
	b.ok("", "backup")
	if st := strings.TrimSpace(b.git("status", "--porcelain")); st != "" {
		t.Errorf("after backup the tree is not clean:\n%s", st)
	}
	if got, want := b.branchHead(bare, branch), strings.TrimSpace(b.git("rev-parse", "HEAD")); got != want {
		t.Errorf("after an external edit bare HEAD %q, local %q", got, want)
	}

	oneErr := regexp.MustCompile(`^err .+\n$`)
	b.git("remote", "set-url", "origin", filepath.Join(b.root, "no", "such.git"))
	if r := b.tix("", "backup"); r.code != 1 || r.out != "" || !oneErr.MatchString(r.err) {
		t.Errorf("broken path: code %d, stdout %q, stderr %q, want code 1 and one err line", r.code, r.out, r.err)
	}
	b.git("remote", "set-url", "origin", "http://user:pw123@127.0.0.1:1/x.git")
	start := time.Now()
	r := b.tix("", "backup")
	if r.code != 1 || !oneErr.MatchString(r.err) || strings.Contains(r.err, "pw123") {
		t.Errorf("address with a password: code %d, stderr %q, want code 1, one line without the password", r.code, r.err)
	}
	if time.Since(start) > 40*time.Second {
		t.Errorf("backup waited %v: a 30 s timeout and no password prompt are needed", time.Since(start))
	}

	nobase := b.runEnv(b.with("TIX_HOME="+filepath.Join(b.root, "missing")), "", "backup")
	if nobase.code == 0 || !oneErr.MatchString(nobase.err) {
		t.Errorf("backup without a store: code %d, stderr %q, want a one-line error", nobase.code, nobase.err)
	}
}
