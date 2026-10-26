package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// racyWindow - a file changed less than racyWindow ago is not cached: the mtime of a coarse
// file system cannot tell a second same-size edit apart (the racy git trick).
const racyWindow = 2 * time.Second

// Cache file frame: a header line and an end line. A file cut anywhere lacks the end line
// and is dropped whole, so a half-written rank never reads as a smaller number.
const (
	rankCacheHeader = "v1"
	rankCacheEnd    = "end"
	rankCacheFields = 7
	rankTmpPrefix   = tmpPrefix + "rank-"
)

// rankEntry - what the column order needs from a task file.
type rankEntry struct {
	id, status, path string
	rank             float64
}

// fileSig tells that a file did not change: inode (the core writes through rename, which
// changes it), size and mtime.
type fileSig struct {
	ino, size, mtime int64
}

type cachedRank struct {
	sig  fileSig
	name string
	rankEntry
}

// rankCachePath is inside .git: outside the task tree, so it never reaches commits or status.
func (s *Store) rankCachePath() string { return filepath.Join(s.root, ".git", "tix-rank") }

func sigOf(fi os.FileInfo) fileSig {
	sig := fileSig{size: fi.Size(), mtime: fi.ModTime().UnixNano()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		sig.ino = int64(st.Ino) //nolint:gosec // the inode is only compared
	}
	return sig
}

// rankIndex returns id, column and rank of every readable task. A file with an unchanged
// signature is not opened: its data comes from the cache. Broken files are skipped, as in
// List. The result equals the one without the cache. Call under the lock.
func (s *Store) rankIndex() []rankEntry {
	dirEntries, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	old := s.loadRankCache()
	out := make([]rankEntry, 0, len(dirEntries))
	keep := make([]cachedRank, 0, len(dirEntries))
	fresh := false
	now := s.now()
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasPrefix(de.Name(), "T-") || !fileIDRe.MatchString(de.Name()) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		sig := sigOf(fi)
		if c, ok := old[de.Name()]; ok && c.sig == sig {
			out = append(out, c.rankEntry)
			keep = append(keep, c)
			continue
		}
		path := filepath.Join(s.root, de.Name())
		t, err := readTask(path)
		if err != nil {
			continue
		}
		e := rankEntry{id: t.ID, status: t.Status, path: path, rank: t.Rank}
		out = append(out, e)
		if now.Sub(fi.ModTime()) >= racyWindow {
			keep = append(keep, cachedRank{sig: sig, name: de.Name(), rankEntry: e})
			fresh = true
		}
	}
	if fresh || len(keep) != len(old) {
		s.saveRankCache(keep)
	}
	return out
}

// loadRankCache reads the cache; any format fault gives an empty cache and the files are read.
func (s *Store) loadRankCache() map[string]cachedRank {
	data, err := os.ReadFile(s.rankCachePath()) //nolint:gosec // the path is built by the core
	if err != nil {
		return nil
	}
	// A whole file ends with the end line; a file cut anywhere does not.
	body, ok := strings.CutSuffix(string(data), rankCacheEnd+"\n")
	if !ok {
		return nil
	}
	lines := strings.Split(body, "\n")
	if lines[0] != rankCacheHeader {
		return nil
	}
	old := make(map[string]cachedRank, len(lines))
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		c, ok := s.parseRankLine(ln)
		if !ok {
			return nil
		}
		old[c.name] = c
	}
	return old
}

func (s *Store) parseRankLine(ln string) (cachedRank, bool) {
	f := strings.Split(ln, "\t")
	if len(f) != rankCacheFields {
		return cachedRank{}, false
	}
	ino, e1 := strconv.ParseInt(f[1], 10, 64)
	size, e2 := strconv.ParseInt(f[2], 10, 64)
	mtime, e3 := strconv.ParseInt(f[3], 10, 64)
	rank, e4 := strconv.ParseFloat(f[6], 64)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return cachedRank{}, false
	}
	return cachedRank{
		sig:       fileSig{ino: ino, size: size, mtime: mtime},
		name:      f[0],
		rankEntry: rankEntry{id: f[4], status: f[5], path: filepath.Join(s.root, f[0]), rank: rank},
	}, true
}

// saveRankCache writes the cache through temp and rename. A failure is not critical: the
// next call reads the files.
func (s *Store) saveRankCache(keep []cachedRank) {
	var b strings.Builder
	b.WriteString(rankCacheHeader + "\n")
	for _, c := range keep {
		if strings.ContainsAny(c.name+c.id+c.status, "\t\n") {
			continue
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\t%s\t%s\n", c.name, c.sig.ino, c.sig.size, c.sig.mtime,
			c.id, c.status, strconv.FormatFloat(c.rank, 'g', -1, 64))
	}
	b.WriteString(rankCacheEnd + "\n")
	path := s.rankCachePath()
	// The temp file lives where cleanTmp sweeps it, so a killed write leaves nothing. Same
	// volume as .git, so the rename stays atomic.
	tmp, err := os.CreateTemp(filepath.Join(s.root, tixDir, tmpDir), rankTmpPrefix+"*.tmp")
	if err != nil {
		s.log.Warn("rank cache not written", "op", "rank_cache", "err", err)
		return
	}
	_, err = tmp.WriteString(b.String())
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		s.log.Warn("rank cache not written", "op", "rank_cache", "err", err)
	}
}
