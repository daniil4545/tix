package project

import (
	"os"
	"path/filepath"
	"strings"
)

// Head возвращает полный sha HEAD репозитория в dir без вызова git. false: нет HEAD
// (нет коммитов, reftable, вне git). Формат файлов - docs/api/git.md, раздел 7.
func Head(dir string) (string, bool) {
	gitDir, ok := gitDirOf(dir)
	if !ok {
		return "", false
	}
	common := gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil { //nolint:gosec // путь из файла .git
		common = absFrom(gitDir, strings.TrimSpace(string(data)))
	}
	if _, err := os.Stat(filepath.Join(common, "reftable")); err == nil { //nolint:gosec // путь из файла .git
		return "", false
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // путь из файла .git
	if err != nil {
		return "", false
	}
	text := strings.TrimSpace(string(head))
	ref, symbolic := strings.CutPrefix(text, "ref:")
	if !symbolic {
		return sha(text)
	}
	return resolveRef(common, strings.TrimSpace(ref))
}

// gitDirOf возвращает git dir: сам .git или папку из "gitdir:" (worktree, submodule).
func gitDirOf(dir string) (string, bool) {
	root := findGitDir(dir)
	if root == "" {
		return "", false
	}
	gitPath := filepath.Join(root, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if fi.IsDir() {
		return gitPath, true
	}
	data, err := os.ReadFile(gitPath) //nolint:gosec // путь .git найден поиском вверх от dir
	if err != nil {
		return "", false
	}
	path, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return "", false
	}
	return absFrom(root, strings.TrimSpace(path)), true
}

// resolveRef читает ref как loose-файл, затем из packed-refs.
func resolveRef(common, ref string) (string, bool) {
	if !strings.HasPrefix(ref, "refs/") || !filepath.IsLocal(ref) {
		return "", false
	}
	if data, err := os.ReadFile(filepath.Join(common, filepath.FromSlash(ref))); err == nil { //nolint:gosec // ref проверен выше
		return sha(strings.TrimSpace(string(data)))
	}
	packed, err := os.ReadFile(filepath.Join(common, "packed-refs")) //nolint:gosec // путь из файла .git
	if err != nil {
		return "", false
	}
	for _, ln := range strings.Split(string(packed), "\n") {
		if id, name, ok := strings.Cut(strings.TrimSpace(ln), " "); ok && name == ref {
			return sha(id)
		}
	}
	return "", false
}

// sha принимает строку из 40 (sha1) или 64 (sha256) шестнадцатеричных знаков.
func sha(s string) (string, bool) {
	if len(s) != 40 && len(s) != 64 {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", false
		}
	}
	return s, true
}
