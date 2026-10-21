package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/daniil4545/tix/internal/actor"
	"github.com/daniil4545/tix/internal/gitx"
	"github.com/daniil4545/tix/internal/project"
)

// gcEvery - число коммитов между запусками gc (docs/api/git.md, раздел 5: 200-500).
const gcEvery = 300

// codeTrailer возвращает трейлер Code: <проект>@<sha HEAD> для рабочей папки процесса.
// Папка вне проекта (внутри базы, вне git) или нет HEAD - пустой трейлер, он не пишется.
func (s *Store) codeTrailer() gitx.Trailer {
	cwd, err := os.Getwd()
	if err != nil {
		return gitx.Trailer{}
	}
	name, ok := project.Detect(cwd, s.root, s.cfg)
	if !ok {
		return gitx.Trailer{}
	}
	dir := repoDir(cwd)
	if dir == "" {
		return gitx.Trailer{}
	}
	sha, ok := project.Head(dir)
	if !ok {
		return gitx.Trailer{}
	}
	return gitx.Trailer{Key: "Code", Value: name + "@" + sha}
}

// modelTrailer возвращает трейлер Model: <TIX_MODEL>. Переменная пуста или не проходит
// проверку - пустой трейлер, он не пишется, запись не отказывает.
func modelTrailer() gitx.Trailer {
	model := os.Getenv("TIX_MODEL")
	if !actor.ValidModel(model) {
		return gitx.Trailer{}
	}
	return gitx.Trailer{Key: "Model", Value: model}
}

// repoDir поднимается от dir к папке с .git (каталогом или файлом worktree).
func repoDir(dir string) string {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// countCommits прибавляет n к счётчику под локом. true: набралось gcEvery, пора запускать gc
// (после снятия лока). Счётчик живёт в файле лока: он уже в .gitignore, а содержимое
// лока никто не читает. Не критичен: сбой записи сдвигает gc, поэтому пишется без temp и rename.
func (s *Store) countCommits(n int) bool {
	if n == 0 {
		return false
	}
	path := filepath.Join(s.root, tixDir, lockFile)
	cur := 0
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // путь строит ядро
		cur, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}
	total := cur + n
	due := total >= gcEvery
	if due {
		total %= gcEvery
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(total)+"\n"), 0o600); err != nil {
		s.log.Warn("gc counter not written", "op", "gc_counter", "err", err)
	}
	return due
}
