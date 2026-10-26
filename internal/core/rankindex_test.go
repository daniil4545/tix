package core_test

import (
	"fmt"
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
// without the cache.

var rankLine = regexp.MustCompile(`(?m)^rank: .*$`)

// rankRig is a store with idea tasks A, B, C and D. The store clock is fixed;
// file mtimes are set against it, so no test sleeps.
type rankRig struct {
	t     *testing.T
	e     *env
	c     *clock
	s     *core.Store
	todo  []string
	extra string
}

func newRankRig(t *testing.T) *rankRig {
	t.Helper()
	e := newEnv(t)
	c := newClock()
	s := e.openAt(c)
	r := &rankRig{t: t, e: e, c: c, s: s}
	for _, title := range []string{"A", "B", "C"} {
		r.todo = append(r.todo, mkIn(t, s, title, "idea"))
	}
	r.extra = mkIn(t, s, "D", "idea")
	return r
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
	for _, id := range append([]string{r.extra}, r.todo...) {
		r.age(id, d)
	}
}

// warm runs a move that fails after the order is read: it fills the cache and changes no task.
func (r *rankRig) warm() {
	r.t.Helper()
	_, err := r.s.Move(r.extra, "idea", core.MoveOpts{Place: core.Place{After: "T-999"}}, "tester")
	if err == nil {
		r.t.Fatal("warm move succeeded")
	}
}

// setRank rewrites the rank in place (same inode); a one-digit value keeps the size.
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

// outcome moves D to the start of idea and returns the error and all tasks as id:status:rank.
func (r *rankRig) outcome() string {
	r.t.Helper()
	_, err := r.s.Move(r.extra, "idea", core.MoveOpts{Place: core.Place{First: true}}, "tester")
	tasks, _ := r.s.List()
	var rows []string
	for _, tk := range tasks {
		rows = append(rows, fmt.Sprintf("%s:%s:%v", tk.ID, tk.Status, tk.Rank))
	}
	sort.Strings(rows)
	return fmt.Sprintf("err=%v %s", err, strings.Join(rows, " "))
}

// putCache replaces the cache file content.
func (r *rankRig) putCache(data []byte) {
	r.t.Helper()
	if err := os.WriteFile(r.cachePath(), data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// cutCache keeps the cache up to the last separator sep plus keep bytes.
func (r *rankRig) cutCache(sep string, keep int) {
	r.t.Helper()
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		r.t.Fatal(err)
	}
	body := strings.TrimSuffix(string(data), "end\n")
	r.putCache(data[:strings.LastIndex(body, sep)+keep])
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

func TestRankCache_BadCache_SameResultAsWithout(t *testing.T) {
	tests := []struct {
		name string
		// prep runs before the cache is filled, damage after it.
		prep   func(r *rankRig)
		damage func(r *rankRig)
	}{
		{name: "warm cache, nothing changed", damage: func(*rankRig) {}},
		{name: "task file rewritten after the cache", damage: func(r *rankRig) {
			r.setRank(r.todo[0], "9")
			r.age(r.todo[0], time.Hour+time.Minute) // a different mtime than the cached one
		}},
		{name: "task file renamed over after the cache", damage: func(r *rankRig) {
			r.replaceFile(r.todo[0], "9")
			r.age(r.todo[0], time.Hour) // the cached mtime, only the inode tells
		}},
		{name: "task deleted after the cache", damage: func(r *rankRig) {
			if err := os.Remove(r.e.file(r.todo[0])); err != nil {
				r.t.Fatal(err)
			}
		}},
		{name: "cache is garbage", damage: func(r *rankRig) { r.putCache([]byte("\x00garbage\n\xff")) }},
		{name: "cache is empty", damage: func(r *rankRig) { r.putCache(nil) }},
		{name: "cache is truncated inside a rank", damage: func(r *rankRig) { r.cutCache("\t", 2) }},
		{name: "cache is truncated at a line end", damage: func(r *rankRig) { r.cutCache("\n", 1) }},
		{name: "cache is a directory", damage: func(r *rankRig) {
			if err := os.Remove(r.cachePath()); err != nil {
				r.t.Fatal(err)
			}
			if err := os.Mkdir(r.cachePath(), 0o755); err != nil {
				r.t.Fatal(err)
			}
		}},
		{
			name: "file changed inside the 2 second window",
			prep: func(r *rankRig) { r.age(r.todo[0], time.Second) },
			damage: func(r *rankRig) {
				r.setRank(r.todo[0], "9")
				r.age(r.todo[0], time.Second) // same inode, size and mtime as at the fill
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := func(useCache bool) string {
				r := newRankRig(t)
				r.ageAll(time.Hour)
				if tc.prep != nil {
					tc.prep(r)
				}
				r.warm()
				tc.damage(r)
				if !useCache {
					_ = os.RemoveAll(r.cachePath())
				}
				return r.outcome()
			}
			with, without := run(true), run(false)
			if with != without {
				t.Errorf("with cache:    %s\nwithout cache: %s", with, without)
			}
		})
	}
}

func TestRankCache_RecentFile_NotCached(t *testing.T) {
	r := newRankRig(t)
	r.ageAll(time.Hour)
	r.age(r.todo[0], racyWindowForTest-time.Millisecond)
	r.warm()
	data, err := os.ReadFile(r.cachePath())
	if err != nil {
		t.Fatal(err)
	}
	id, old := r.e.file(r.todo[0]), r.e.file(r.todo[1])
	if strings.Contains(string(data), filepath.Base(id)) {
		t.Errorf("file changed %v ago is in the cache:\n%s", racyWindowForTest-time.Millisecond, data)
	}
	if !strings.Contains(string(data), filepath.Base(old)) {
		t.Errorf("old file is not in the cache:\n%s", data)
	}
}

// racyWindowForTest mirrors the documented window of 2 seconds.
const racyWindowForTest = 2 * time.Second

func TestRankCache_UnchangedFile_ServedFromCache(t *testing.T) {
	r := newRankRig(t)
	r.ageAll(time.Hour)
	r.warm()
	top := r.e.file(r.todo[0])
	if err := os.Chmod(top, 0); err != nil { // metadata only: the signature stays
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(top, 0o644) })
	if _, err := r.s.Move(r.extra, "idea", core.MoveOpts{Place: core.Place{First: true}}, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(top, 0o644); err != nil {
		t.Fatal(err)
	}
	cached := get(t, r.s, r.extra).Rank
	if want := get(t, r.s, r.todo[0]).Rank - 1; cached != want {
		t.Errorf("rank of D = %v, want %v: the unreadable file was not taken from the cache", cached, want)
	}
}

func TestRankCache_WriteFails_CommandSucceeds(t *testing.T) {
	r := newRankRig(t)
	r.ageAll(time.Hour)
	// A directory in place of .git/tix-rank makes the final rename fail.
	if err := os.Mkdir(r.cachePath(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.cachePath(), "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Move(r.extra, "idea", core.MoveOpts{Place: core.Place{First: true}}, "tester"); err != nil {
		t.Fatalf("Move with an unwritable cache: %v", err)
	}
	if got := get(t, r.s, r.extra); got.Status != "idea" {
		t.Errorf("status = %q, want idea", got.Status)
	}
	if !strings.Contains(r.e.log.String(), "rank cache not written") {
		t.Errorf("no warning in the log:\n%s", r.e.log)
	}
	left, _ := filepath.Glob(filepath.Join(r.e.root, ".git", "tix-rank.*"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
}
