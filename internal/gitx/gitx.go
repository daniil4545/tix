// Package gitx - единственное место с os/exec: вызовы git.
package gitx

// Repo - репозиторий задач и путь к бинарю git.
type Repo struct{ Dir, Bin string }

// Change - строка git status: путь и код состояния.
type Change struct{ Path, Code string }

// Resolve находит путь к git: override (TRACKER_GIT), кеш, xcrun на darwin.
func Resolve(override string) (string, error) {
	panic("not implemented")
}

// Init создаёт репозиторий.
func (r Repo) Init() error {
	panic("not implemented")
}

// Status читает изменения: --porcelain -z --untracked-files=all.
func (r Repo) Status() ([]Change, error) {
	panic("not implemented")
}

// Commit делает add -A -- paths и commit --only -- paths, возвращает хеш.
// Пустой список путей - commit --allow-empty --only.
func (r Repo) Commit(paths []string, subject, actor string) (string, error) {
	panic("not implemented")
}

// HashObject пишет blob в git (hash-object -w) и возвращает его id.
func (r Repo) HashObject(data []byte) (string, error) {
	panic("not implemented")
}

// Blob читает blob по id.
func (r Repo) Blob(rev string) ([]byte, error) {
	panic("not implemented")
}

// MergeFile сливает три версии через git merge-file.
func (r Repo) MergeFile(ours, base, theirs []byte) (out []byte, conflict bool, err error) {
	panic("not implemented")
}

// Remove удаляет файл из дерева и индекса (git rm).
func (r Repo) Remove(path string) error {
	panic("not implemented")
}

// DropIndexLock удаляет висящий index.lock; true - файл был.
func (r Repo) DropIndexLock() (bool, error) {
	panic("not implemented")
}
