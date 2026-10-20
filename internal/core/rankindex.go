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

// racyWindow - файл, изменённый позже чем racyWindow назад, в кеш не идёт: mtime грубой
// файловой системы не отличит вторую правку того же размера (приём racy git).
const racyWindow = 2 * time.Second

const rankCacheHeader = "v1"

// rankEntry - то, что порядку колонки нужно от файла задачи.
type rankEntry struct {
	id, status, path string
	num              int
	rank             float64
}

// fileSig - признак, что файл не менялся: inode (запись ядра идёт через rename и меняет его),
// размер и mtime.
type fileSig struct {
	ino, size, mtime int64
}

type cachedRank struct {
	sig fileSig
	rankEntry
}

// rankCachePath лежит в .git: вне дерева задач, в коммиты и status не попадает.
func (s *Store) rankCachePath() string { return filepath.Join(s.root, ".git", "tracker-rank") }

// rankIndex возвращает id, колонку и ранг каждой читаемой задачи. Файл с прежним признаком
// не открывается: ранги берутся из кеша. Битые файлы пропускаются, как в List. Вызывать под локом.
func (s *Store) rankIndex() []rankEntry {
	dirEntries, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	old := s.loadRankCache()
	out := make([]rankEntry, 0, len(dirEntries))
	var keep []cachedRank
	fresh := false
	now := time.Now()
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasPrefix(de.Name(), "T-") || !fileIDRe.MatchString(de.Name()) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue
		}
		sig := fileSig{size: fi.Size(), mtime: fi.ModTime().UnixNano()}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			sig.ino = int64(st.Ino) //nolint:gosec // inode только сравнивается
		}
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
		e := rankEntry{id: t.ID, status: t.Status, path: path, num: IDNum(t.ID), rank: t.Rank}
		out = append(out, e)
		if now.Sub(fi.ModTime()) >= racyWindow {
			keep = append(keep, cachedRank{sig: sig, rankEntry: e})
			fresh = true
		}
	}
	if fresh || len(keep) != len(old) {
		s.saveRankCache(keep)
	}
	return out
}

// loadRankCache читает кеш; любой сбой формата - пустой кеш, файлы перечитаются.
func (s *Store) loadRankCache() map[string]cachedRank {
	data, err := os.ReadFile(s.rankCachePath()) //nolint:gosec // путь строит ядро
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if lines[0] != rankCacheHeader {
		return nil
	}
	old := make(map[string]cachedRank, len(lines))
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 7 {
			return nil
		}
		ino, e1 := strconv.ParseInt(f[1], 10, 64)
		size, e2 := strconv.ParseInt(f[2], 10, 64)
		mtime, e3 := strconv.ParseInt(f[3], 10, 64)
		rank, e4 := strconv.ParseFloat(f[6], 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return nil
		}
		old[f[0]] = cachedRank{
			sig:       fileSig{ino: ino, size: size, mtime: mtime},
			rankEntry: rankEntry{id: f[4], status: f[5], path: filepath.Join(s.root, f[0]), num: IDNum(f[4]), rank: rank},
		}
	}
	return old
}

// saveRankCache пишет кеш через temp и rename. Сбой не критичен: следующий вызов перечитает файлы.
func (s *Store) saveRankCache(keep []cachedRank) {
	var b strings.Builder
	b.WriteString(rankCacheHeader + "\n")
	for _, c := range keep {
		name := filepath.Base(c.path)
		if strings.ContainsAny(name+c.status, "\t\n") {
			continue
		}
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\t%s\t%s\n", name, c.sig.ino, c.sig.size, c.sig.mtime,
			c.id, c.status, strconv.FormatFloat(c.rank, 'g', -1, 64))
	}
	path := s.rankCachePath()
	tmp, err := os.CreateTemp(filepath.Dir(path), "tracker-rank.*")
	if err != nil {
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
