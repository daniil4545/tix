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

// Spec 3, happy path: the hook gave ctx; take, note, check, set summary=, comment, done -m.
func TestScenario_HappyPath_AgentCycle(t *testing.T) {
	b := newBox(t)
	b.cwd = b.project("myproj", true)
	b.env = b.with("CLAUDE_CODE_SESSION_ID=happy-1")

	b.ok("", "new", "Fix parser")
	b.ok("## Details\n- [ ] reproduce\n- [ ] fix\n", "edit", "T-1", "--base", b.showRev("T-1"))
	b.ok("", "move", "T-1", "open")

	ctx := b.ok("", "ctx").out
	if !strings.HasPrefix(ctx, "project myproj") || !regexp.MustCompile(`(?m)^open\s+T-1 Fix parser`).MatchString(ctx) ||
		!strings.HasSuffix(strings.TrimSpace(ctx), "next T-1") {
		t.Fatalf("ctx before work:\n%s", ctx)
	}
	steps := []struct {
		args []string
		want string
	}{
		{[]string{"take", "T-1"}, `^ok T-1 open > in_progress  @\S+ until \d\d:\d\d\n$`},
		{[]string{"note", "T-1", "parser located"}, `^ok T-1 note\n$`},
		{[]string{"check", "T-1", "1", "2"}, "^ok T-1 2/2\n$"},
		{[]string{"set", "T-1", "summary=Fixed the parser"}, "^ok T-1 summary\n$"},
		{[]string{"comment", "T-1", "done and verified"}, "^ok T-1 comment\n$"},
		{[]string{"done", "T-1", "-m", "merged"}, "^ok T-1 in_progress > closed\n$"},
	}
	for _, s := range steps {
		r := b.tix("", s.args...)
		if r.code != 0 || !regexp.MustCompile(s.want).MatchString(r.out) {
			t.Fatalf("tix %v: code %d, stdout %q, stderr %q, want %s", s.args, r.code, r.out, r.err, s.want)
		}
	}

	b.checkHistory([]string{"T-1: new", "T-1: edit", "T-1: take", "T-1: note", "T-1: check", "T-1: set", "T-1: comment", "T-1: done"})
	show := b.ok("", "show", "T-1").out
	for _, want := range []string{"status: closed", "- [x] reproduce", "- [x] fix", "Fixed the parser", "Result: merged", "done and verified"} {
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
	b.ok("", "new", "Contended")
	b.ok("## Details\n- [ ] one\n", "edit", "T-1", "--base", b.showRev("T-1"))
	b.ok("", "move", "T-1", "open")
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

// backup and the ask field are gone: backup is an unknown command, set ask= an unknown field, and a
// file with an old ask line is read, kept and not shown.
func TestScenario_BackupAndAsk_Removed(t *testing.T) {
	b := newBox(t)
	b.ok("", "new", "Work")
	if r := b.tix("", "backup"); r.code == 0 || !strings.HasPrefix(r.err, "err unknown command backup") {
		t.Errorf("backup: code %d, stderr %q, want an unknown command error", r.code, r.err)
	}
	if r := b.tix("", "set", "T-1", "ask=q"); r.code == 0 || !strings.Contains(r.err, "allowed: title project waiting tags external") {
		t.Errorf("set ask=: code %d, stderr %q, want an unknown field error", r.code, r.err)
	}
	f := b.taskFile("T-1")
	b.write(f, strings.Replace(b.read(f), "\n---\n", "\nask: old question\n---\n", 1))
	b.ok("", "set", "T-1", "tags=x")
	if got := b.read(f); !strings.Contains(got, "ask: old question") {
		t.Errorf("the old ask line was lost on save:\n%s", got)
	}
	for _, args := range [][]string{{"show", "T-1", "--json"}, {"list", "--json"}, {"ctx"}} {
		if r := b.tix("", args...); r.code != 0 || strings.Contains(r.out, "old question") || strings.Contains(r.out, `"ask"`) {
			t.Errorf("%v: code %d, ask in the output:\n%s", args, r.code, r.out)
		}
	}
}
