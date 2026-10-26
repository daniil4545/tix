package core_test

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
)

// The rank cache is an optimization: every scenario below must give the result of a run
// without the cache, for take and for move.

var rankLine = regexp.MustCompile(`(?m)^rank: .*$`)

// rankOp is the command under test. A, B, C sit in column col, D is the moved task.
type rankOp struct {
	name string
	col  string // column of A, B, C and the column D goes to
	dCol string // column of D before the command
	tail bool   // the command puts D after the last task of col (Place{}); else before the first
	run  func(s *core.Store, d string) error
}

var rankOps = []rankOp{
	{name: "move first", col: "idea", dCol: "idea", run: func(s *core.Store, d string) error {
		_, err := s.Move(d, "idea", core.MoveOpts{Place: core.Place{First: true}}, "tester")
		return err
	}},
	{name: "move to column tail", col: "doing", dCol: "todo", tail: true, run: func(s *core.Store, d string) error {
		_, err := s.Move(d, "doing", core.MoveOpts{}, "tester")
		return err
	}},
	{name: "take", col: "doing", dCol: "todo", tail: true, run: func(s *core.Store, d string) error {
		_, err := s.Take(d, core.Take, "claude:a")
		return err
	}},
}

// rankRig is a store with tasks A, B, C and D. The store clock is fixed; file mtimes are set
// against it, so no test sleeps.
type rankRig struct {
	t   *testing.T
	e   *env
	c   *clock
	s   *core.Store
	op  rankOp
	abc []string
	d   string
}

func newRankRig(t *testing.T, op rankOp) *rankRig {
	t.Helper()
	e := newEnv(t)
	c := newClock()
	r := &rankRig{t: t, e: e, c: c, s: e.openAt(c), op: op}
	for _, title := range []string{"A", "B", "C"} {
		r.abc = append(r.abc, mkIn(t, r.s, title, op.col))
	}
	r.d = mkIn(t, r.s, "D", op.dCol)
	r.ageAll(time.Hour)
	return r
}

// target is the task whose rank decides the command result: the last one for a tail
// command, the first one otherwise.
func (r *rankRig) target() string {
	if r.op.tail {
		return r.abc[2]
	}
	return r.abc[0]
}

func (r *rankRig) cachePath() string { return filepath.Join(r.e.root, ".git", "tix-rank") }

// age sets the file mtime to the clock minus d.
func (r *rankRig) age(id string, d time.Duration) {
	r.t.Helper()
	at := r.c.now().Add(-d)
	if err := os.Chtimes(r.e.file(id), at, at); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rankRig) ageAll(d time.Duration) {
	for _, id := range append([]string{r.d}, r.abc...) {
		r.age(id, d)
	}
}

// warm runs a move that fails after the order is read: it fills the cache and changes no task.
func (r *rankRig) warm() {
	r.t.Helper()
	_, err := r.s.Move(r.d, r.op.col, core.MoveOpts{Place: core.Place{After: "T-999"}}, "tester")
	if err == nil {
		r.t.Fatal("warm move succeeded")
	}
}

// setRank rewrites the rank in place (same inode).
func (r *rankRig) setRank(id, rank string) {
	r.t.Helper()
	p := r.e.file(id)
	data, err := os.ReadFile(p)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, rankLine.ReplaceAll(data, []byte("rank: "+rank)), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// replaceFile swaps a task file for a new one with another rank, as an editor that saves
// through temp and rename does: the inode changes.
func (r *rankRig) replaceFile(id, rank string) {
	r.t.Helper()
	p := r.e.file(id)
	data, err := os.ReadFile(p)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p+".x", rankLine.ReplaceAll(data, []byte("rank: "+rank)), 0o644); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(p+".x", p); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rankRig) remove(path string) {
	r.t.Helper()
	if err := os.Remove(path); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rankRig) putCache(data []byte) {
	r.t.Helper()
	if err := os.WriteFile(r.cachePath(), data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// cutLastLines drops the last entry and the end marker, cutting at a line end.
func (r *rankRig) cutLastLines() {
	r.t.Helper()
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		r.t.Fatal(err)
	}
	body := strings.TrimSuffix(string(data), "end\n")
	r.putCache(data[:strings.LastIndex(strings.TrimSuffix(body, "\n"), "\n")+1])
}

// cutInsideRank cuts the cache after keep bytes of the id rank, dropping the rest.
func (r *rankRig) cutInsideRank(id string, keep int) {
	r.t.Helper()
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		r.t.Fatal(err)
	}
	start := strings.Index(string(data), filepath.Base(r.e.file(id)))
	if start < 0 {
		r.t.Fatalf("%s is not in the cache:\n%s", id, data)
	}
	at := start
	for range 6 { // the rank follows the sixth tab of the line
		at += strings.Index(string(data[at:]), "\t") + 1
	}
	r.putCache(data[:at+keep])
}

// outcome runs the command and returns the error and all tasks as id:status:rank.
func (r *rankRig) outcome(s *core.Store) string {
	r.t.Helper()
	err := r.op.run(s, r.d)
	tasks, _ := s.List()
	var rows []string
	for _, tk := range tasks {
		rows = append(rows, fmt.Sprintf("%s:%s:%v", tk.ID, tk.Status, tk.Rank))
	}
	sort.Strings(rows)
	return fmt.Sprintf("err=%v %s", err, strings.Join(rows, " "))
}

// withoutCache copies the store to a new place, drops the cache there and opens it.
func (r *rankRig) withoutCache() *core.Store {
	r.t.Helper()
	dst := filepath.Join(r.t.TempDir(), "tasks")
	err := filepath.WalkDir(r.e.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(r.e.root, p)
		switch {
		case rel == filepath.Join(".git", "tix-rank"):
			return nil
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		r.t.Fatal(err)
	}
	s, err := core.Open(dst, core.Options{
		LockTimeout: 5 * time.Second, GitBin: filepath.Join(r.e.fake, "git"),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: r.c.now,
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func TestRankCache_BadCache_SameResultAsWithout(t *testing.T) {
	tests := []struct {
		name     string
		tailOnly bool // the scenario is visible only in the tail of the column
		// prep runs before the cache is filled, damage after it.
		prep   func(r *rankRig)
		damage func(r *rankRig)
	}{
		{name: "warm cache, nothing changed", damage: func(*rankRig) {}},
		{name: "task file rewritten after the cache", damage: func(r *rankRig) {
			r.setRank(r.target(), "9")
			r.age(r.target(), time.Hour+time.Minute) // a different mtime than the cached one
		}},
		{name: "task file renamed over after the cache", damage: func(r *rankRig) {
			r.replaceFile(r.target(), "9")
			r.age(r.target(), time.Hour) // the cached mtime, only the inode tells
		}},
		{name: "task deleted after the cache", damage: func(r *rankRig) { r.remove(r.e.file(r.target())) }},
		{name: "cache is garbage", damage: func(r *rankRig) { r.putCache([]byte("\x00garbage\n\xff")) }},
		{name: "cache is empty", damage: func(r *rankRig) { r.putCache(nil) }},
		{name: "cache is truncated at a line end", damage: func(r *rankRig) { r.cutLastLines() }},
		{
			// The cut keeps "12" of the rank 12.5: a whole line with a valid but wrong rank.
			// Only the end marker tells it from a complete file.
			name: "cache is truncated inside a fractional rank", tailOnly: true,
			prep: func(r *rankRig) {
				r.setRank(r.target(), "12.5")
				r.age(r.target(), time.Hour)
			},
			damage: func(r *rankRig) { r.cutInsideRank(r.target(), 2) },
		},
		{
			// The cut keeps "1" of the rank 12.
			name: "cache is truncated inside a two-digit rank", tailOnly: true,
			prep: func(r *rankRig) {
				r.setRank(r.target(), "12")
				r.age(r.target(), time.Hour)
			},
			damage: func(r *rankRig) { r.cutInsideRank(r.target(), 1) },
		},
		{name: "cache is a directory", damage: func(r *rankRig) {
			r.remove(r.cachePath())
			if err := os.Mkdir(r.cachePath(), 0o755); err != nil {
				r.t.Fatal(err)
			}
		}},
		{
			name: "file changed inside the 2 second window",
			prep: func(r *rankRig) { r.age(r.target(), time.Second) },
			damage: func(r *rankRig) {
				r.setRank(r.target(), "9")
				r.age(r.target(), time.Second) // same inode, size and mtime as at the fill
			},
		},
	}
	for _, op := range rankOps {
		for _, tc := range tests {
			if tc.tailOnly && !op.tail {
				continue
			}
			t.Run(op.name+"/"+tc.name, func(t *testing.T) {
				r := newRankRig(t, op)
				if tc.prep != nil {
					tc.prep(r)
				}
				r.warm()
				tc.damage(r)
				baseline := r.withoutCache()
				with, without := r.outcome(r.s), r.outcome(baseline)
				if with != without {
					t.Errorf("with cache:    %s\nwithout cache: %s", with, without)
				}
			})
		}
	}
}

func TestRankCache_RecentFile_NotCached(t *testing.T) {
	r := newRankRig(t, rankOps[0])
	r.age(r.abc[0], 2*time.Second-time.Millisecond)
	r.warm()
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		t.Fatal(err)
	}
	recent, old := filepath.Base(r.e.file(r.abc[0])), filepath.Base(r.e.file(r.abc[1]))
	if strings.Contains(string(data), recent) {
		t.Errorf("file changed just under 2 s ago is in the cache:\n%s", data)
	}
	if !strings.Contains(string(data), old) {
		t.Errorf("old file is not in the cache:\n%s", data)
	}
}

func TestRankCache_UnchangedFile_ServedFromCache(t *testing.T) {
	r := newRankRig(t, rankOps[0])
	r.warm()
	top := r.e.file(r.abc[0])
	if err := os.Chmod(top, 0); err != nil { // metadata only: the signature stays
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(top, 0o644) })
	if err := r.op.run(r.s, r.d); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(top, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := get(t, r.s, r.d).Rank, get(t, r.s, r.abc[0]).Rank-1; got != want {
		t.Errorf("rank of D = %v, want %v: the unreadable file was not taken from the cache", got, want)
	}
}

func TestRankCache_WriteFails_CommandSucceeds(t *testing.T) {
	r := newRankRig(t, rankOps[0])
	// A non-empty directory in place of .git/tix-rank makes the final rename fail.
	if err := os.MkdirAll(filepath.Join(r.cachePath(), "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.op.run(r.s, r.d); err != nil {
		t.Fatalf("command with an unwritable cache: %v", err)
	}
	if !strings.Contains(r.e.log.String(), "rank cache not written") {
		t.Errorf("no warning in the log:\n%s", r.e.log)
	}
	left, _ := filepath.Glob(filepath.Join(r.e.root, ".tix", "tmp", "*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}

// A write killed between the temp file and the rename leaves the temp file behind. The
// next locked operation must sweep it.
func TestRankCache_KilledWrite_TempSwept(t *testing.T) {
	r := newRankRig(t, rankOps[0])
	leftover := filepath.Join(r.e.root, ".tix", "tmp", core.RankTmpPrefix+"killed.tmp")
	if err := os.WriteFile(leftover, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.op.run(r.s, r.d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("rank temp file survived the next operation: %v", err)
	}
}

// The temp file must live in .tix/tmp, the directory the next operation sweeps. With that
// directory unwritable no temp file can be made, so no cache is written.
func TestRankCache_TempDir_IsTheSweptOne(t *testing.T) {
	r := newRankRig(t, rankOps[0])
	tmp := filepath.Join(r.e.root, ".tix", "tmp")
	if err := os.Chmod(tmp, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o755) })
	r.warm()
	if _, err := os.Stat(r.cachePath()); !os.IsNotExist(err) {
		t.Errorf("cache written without a writable .tix/tmp: %v", err)
	}
}
