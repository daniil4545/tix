package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unitFocusURL = "warp://session/0123456789abcdef0123456789abcdef"

func unitFocusBox(t *testing.T, vars map[string]string) (*box, string) {
	t.Helper()
	b := newBox(t)
	dir := filepath.Join(t.TempDir(), "focus")
	vars["TIX_FOCUS_DIR"] = dir
	b.extra = vars
	return b, dir
}

// Спека 4a.1: файл 0600 с одной строкой в каталоге фокуса, не в базе и не в git.
func TestFocusUnit_AgentWrite_FileOutsideBase(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL})
	b.ok("", "new", "task")
	path := filepath.Join(dir, "claude-s1")
	data, err := os.ReadFile(path)
	info, serr := os.Stat(path)
	if err != nil || serr != nil || string(data) != unitFocusURL+"\n" || info.Mode().Perm() != 0o600 {
		t.Fatalf("файл фокуса: %q, %v, %v; ждали одну строку и 0600", data, err, serr)
	}
	if _, err := os.Stat(filepath.Join(b.home, ".tix", "focus")); err == nil {
		t.Error("фокус создан внутри базы")
	}
	if st := b.git("status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(st) != "" {
		t.Errorf("git status не пуст: %q", st)
	}
}

// Значение не того вида, человек и неизвестная личность не пишутся.
func TestFocusUnit_NotWritten(t *testing.T) {
	cases := map[string]map[string]string{
		"чужой вид":  {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": "warp://session/xyz"},
		"заглавные":  {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": strings.ToUpper(unitFocusURL)},
		"с хвостом":  {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL + "\nx"},
		"аргумент":   {"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": "-a Calculator"},
		"человек":    {"TIX_ACTOR": "human", "WARP_FOCUS_URL": unitFocusURL},
		"без личной": {"WARP_FOCUS_URL": unitFocusURL},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			b, dir := unitFocusBox(t, vars)
			b.tix("", "new", "task")
			if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
				t.Errorf("записано %v", entries)
			}
		})
	}
}

// Имя файла не выходит из каталога фокуса.
func TestFocusUnit_ActorWithSlashes_StaysInDir(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"TIX_ACTOR": "x:../../evil", "WARP_FOCUS_URL": unitFocusURL})
	b.ok("", "new", "task")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || strings.ContainsAny(entries[0].Name(), "/\\") {
		t.Fatalf("каталог фокуса = %v, %v; ждали один файл без слэшей", entries, err)
	}
}

// Ошибка записи файла не роняет команду.
func TestFocusUnit_DirIsFile_CommandStillSucceeds(t *testing.T) {
	b, dir := unitFocusBox(t, map[string]string{"CLAUDE_CODE_SESSION_ID": "s1", "WARP_FOCUS_URL": unitFocusURL})
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := b.tix("", "new", "task"); r.code != 0 {
		t.Errorf("код %d, stderr %q", r.code, r.err)
	}
}
