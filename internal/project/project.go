// Package project detects the project by the working folder. No git.
package project

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/daniil4545/tix/internal/config"
)

// Detect returns the project name for cwd: the name of the folder above .git (for a worktree - above
// the main one), renamed by the c.Projects map. false: cwd in root or outside git.
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

// resolve expands symlinks (macOS: /var and /private/var); if that fails - the path as is.
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

// findGitDir climbs from cwd to the folder with .git (a directory or a worktree file).
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

// mainWorktree: if .git is a file of a linked worktree, returns the folder of the main
// copy; otherwise an empty string. The path is read from "gitdir:" and commondir.
func mainWorktree(dir string) string {
	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Lstat(gitPath)
	if err != nil || fi.IsDir() {
		return ""
	}
	data, err := os.ReadFile(gitPath) //nolint:gosec // the .git path was found by searching up from cwd
	if err != nil {
		return ""
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return ""
	}
	gitdir = absFrom(dir, strings.TrimSpace(gitdir))
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir")) //nolint:gosec // path from the .git file
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
