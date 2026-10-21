package cli_test

// Фокус сессии агента: docs/plans/mvp-actions.md, раздел 4a.1 и сценарий 8 (запись файла).
// Пишутся по спеке, без чтения кода зоны H. Окружение подаётся через box.extra.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const warpURL = "warp://session/0123456789abcdef0123456789abcdef"

func (b *box) focusFiles() []string {
	b.t.Helper()
	files, err := filepath.Glob(filepath.Join(b.focusDir(), "*"))
	if err != nil {
		b.t.Fatal(err)
	}
	return files
}

// focusDir - каталог фокуса вне базы (TIX_FOCUS_DIR перекрывает os.UserCacheDir()/tix/focus).
func (b *box) focusDir() string { return filepath.Join(b.root, "focus") }

// agentEnv - окружение сессии Claude "c1" с адресом вкладки url (пусто - переменной нет).
func (b *box) agentEnv(url string) map[string]string {
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": "c1", "TIX_FOCUS_DIR": b.focusDir()}
	if url != "" {
		env["WARP_FOCUS_URL"] = url
	}
	return env
}

// 4a.1: любая команда записи агента пишет адрес в каталог фокуса, файл <агент>-<id сессии>, 0600, одна строка.
func TestFocus_AgentWriteCommands_StoreURL(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"new", []string{"new", "Second", "--to", "todo"}},
		{"note", []string{"note", "T-1", "progress"}},
		{"comment", []string{"comment", "T-1", "remark"}},
		{"take", []string{"take", "T-1"}},
		{"set", []string{"set", "T-1", "due=2026-11-01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.ok("", "new", "First", "--to", "todo")
			b.extra = b.agentEnv(warpURL)
			b.ok("", c.args...)
			files := b.focusFiles()
			if len(files) != 1 {
				t.Fatalf("файлов фокуса %d (%v), ждали один", len(files), files)
			}
			if name := filepath.Base(files[0]); name != "claude-c1" {
				t.Errorf("имя файла %q, ждали claude-c1 (<агент>-<id сессии>)", name)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimRight(string(data), "\n"); got != warpURL || strings.Contains(got, "\n") {
				t.Errorf("содержимое %q, ждали одну строку %q", data, warpURL)
			}
			info, err := os.Stat(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("права %v, ждали 0600", info.Mode().Perm())
			}
		})
	}
}

// 4a.1: значение не вида ^warp://session/[0-9a-f]{32}$ не пишется; команда при этом проходит.
func TestFocus_ValueOfOtherShape_NotWritten(t *testing.T) {
	hex32 := "0123456789abcdef0123456789abcdef"
	cases := []struct{ name, url string }{
		{"не warp", "https://example.com/session/" + hex32},
		{"буквы вне hex", "warp://session/zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"заглавные", "warp://session/0123456789ABCDEF0123456789ABCDEF"},
		{"31 знак", "warp://session/" + hex32[:31]},
		{"33 знака", "warp://session/" + hex32 + "a"},
		{"хвост после адреса", warpURL + "/extra"},
		{"перевод строки в конце", warpURL + "\n"},
		{"вторая строка", warpURL + "\nwarp://session/" + hex32},
		{"пробел в начале", " " + warpURL},
		{"схема другого терминала", "ghostty://session/" + hex32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			b := newBox(t)
			b.extra = b.agentEnv(c.url)
			b.ok("", "new", "Work", "--to", "todo")
			if files := b.focusFiles(); len(files) != 0 {
				t.Errorf("значение %q записано в %v", c.url, files)
			}
		})
	}
}

// 4a.1: человек (личность human) файл не пишет; без WARP_FOCUS_URL файла нет.
func TestFocus_HumanOrNoURL_NotWritten(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.extra = b.agentEnv(warpURL)
	b.ok("", "new", "By human", "--to", "todo", "--as", "human")
	if files := b.focusFiles(); len(files) != 0 {
		t.Errorf("запись human оставила файл фокуса: %v", files)
	}

	b2 := newBox(t)
	b2.extra = b2.agentEnv("")
	b2.ok("", "new", "No url", "--to", "todo")
	if files := b2.focusFiles(); len(files) != 0 {
		t.Errorf("без WARP_FOCUS_URL появился файл: %v", files)
	}
}

// 4a.1: ошибка записи файла не роняет команду: запись в базу прошла ровно одним коммитом, код 0.
func TestFocus_FileWriteFails_CommandStillSucceeds(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "First", "--to", "todo")
	// на месте каталога фокуса лежит обычный файл: mkdir и запись невозможны
	if err := os.WriteFile(b.focusDir(), []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := b.commitCount()
	b.extra = b.agentEnv(warpURL)
	r := b.tix("", "note", "T-1", "still works")
	if r.code != 0 {
		t.Fatalf("note при сбое записи фокуса: код %d, stderr %q, ждали 0", r.code, r.err)
	}
	if after := b.commitCount(); after != before+1 {
		t.Errorf("коммитов %d -> %d, ждали ровно один (запись note)", before, after)
	}
}

// Адрес вкладки привязан к машине: файл фокуса лежит вне базы и не уходит в git
// (ни как отслеживаемый, ни как external edit следующей операции).
func TestFocus_FileNeverCommittedToGit(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.extra = b.agentEnv(warpURL)
	b.ok("", "new", "First", "--to", "todo")
	if len(b.focusFiles()) != 1 {
		t.Fatalf("файл фокуса не записан: %v", b.focusFiles())
	}
	b.extra = nil
	b.ok("", "new", "Second", "--to", "todo") // следующая операция подбирает чужие правки дерева
	if tracked := b.git("ls-files"); strings.Contains(tracked, "focus") {
		t.Errorf("файл фокуса попал в git: %q", tracked)
	}
	if st := b.git("status", "--porcelain"); strings.Contains(st, "focus") {
		t.Errorf("файл фокуса висит в git status: %q", st)
	}
}
