// Package project определяет проект по рабочей папке. Без git.
package project

import (
	"os"
	"path/filepath"
	"strings"

	"tracker/internal/config"
)

// Detect возвращает имя проекта для cwd: имя папки над .git (у worktree - над
// основной), переименованное картой c.Projects. false: cwd в root или вне git.
func Detect(cwd, root string, c *config.Config) (string, bool) {
	cwd, root = resolve(cwd), resolve(root)
	if root != "" && within(cwd, root) {
		return "", false
	}
	dir := findGitDir(cwd)
	if dir == "" {
		return "", false
	}
	if main := mainWorktree(dir); main != "" {
		dir = main
	}
	name := filepath.Base(dir)
	if c != nil {
		if mapped, ok := c.Projects[name]; ok && mapped != "" {
			name = mapped
		}
	}
	return name, true
}

// resolve раскрывает симлинки (macOS: /var и /private/var); не вышло - путь как есть.
func resolve(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// findGitDir поднимается от cwd к папке с .git (каталогом или файлом worktree).
func findGitDir(cwd string) string {
	for dir := cwd; ; {
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

// mainWorktree: если .git - файл linked worktree, возвращает папку основной
// копии; иначе пустая строка. Путь читается из "gitdir:" и commondir.
func mainWorktree(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Lstat(gitPath)
	if err != nil || fi.IsDir() {
		return ""
	}
	data, err := os.ReadFile(gitPath) //nolint:gosec // путь .git найден поиском вверх от cwd
	if err != nil {
		return ""
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	gitdir = absFrom(dir, strings.TrimSpace(gitdir))
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir")) //nolint:gosec // путь из файла .git
	if err != nil {
		return ""
	}
	commonDir := absFrom(gitdir, strings.TrimSpace(string(common)))
	if filepath.Base(commonDir) != ".git" {
		return ""
	}
	return filepath.Dir(commonDir)
}

func absFrom(base, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}
