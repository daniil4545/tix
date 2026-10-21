package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Миграция: база прежней версии (.tracker, записи в .gitignore) после первого Open получает
// .tix, задачи и журнал на месте, .gitignore обновлён, следующая запись коммитит правки.
func TestOpen_LegacyTrackerDir_MigratedToTix(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(0)
	r := mustCreate(t, s, "Legacy")
	// Состояние прежней версии: служебная папка и .gitignore со старым именем.
	if err := os.Rename(filepath.Join(e.root, ".tix"), filepath.Join(e.root, ".tracker")); err != nil {
		t.Fatal(err)
	}
	ign := filepath.Join(e.root, ".gitignore")
	data, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ign, []byte(strings.ReplaceAll(string(data), ".tix/", ".tracker/")), 0o600); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", "legacy layout")
	before := e.count()

	s = e.open(0)
	if _, err := os.Stat(filepath.Join(e.root, ".tix", "config.toml")); err != nil {
		t.Errorf("после Open нет .tix/config.toml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.root, ".tracker")); !os.IsNotExist(err) {
		t.Errorf(".tracker остался после миграции: %v", err)
	}
	got, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), ".tracker") || !strings.Contains(string(got), ".tix/lock\n") {
		t.Errorf(".gitignore после миграции:\n%s", got)
	}
	if _, _, err := s.Get(r.ID); err != nil {
		t.Errorf("задача %s после миграции: %v", r.ID, err)
	}
	if e.count() != before {
		t.Errorf("Open создал коммит: было %d, стало %d", before, e.count())
	}
	if _, err := s.Comment(r.ID, "after", "tester"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if out := e.git("status", "--porcelain"); out != "" {
		t.Errorf("правки миграции не закоммичены:\n%s", out)
	}
	if e.count() <= before {
		t.Errorf("запись после миграции не создала коммит")
	}
}

// Если .tix уже есть, а .tracker остался, Open ничего не трогает: данные не затираются.
func TestOpen_BothDirs_LegacyUntouched(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(0)
	mustCreate(t, s, "Both")
	legacy := filepath.Join(e.root, ".tracker")
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	e.open(0)
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf(".tracker удалён при наличии .tix: %v", err)
	}
}

// legacyBase готовит базу прежней версии: служебная папка .tracker с журналом, .gitignore
// с текстом ign (nil - файла нет), всё закоммичено.
func legacyBase(t *testing.T, e *env, ign *string) {
	t.Helper()
	mustCreate(t, e.open(0), "Legacy")
	if err := os.Rename(filepath.Join(e.root, ".tix"), filepath.Join(e.root, ".tracker")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, ".tracker", "log"), []byte("old log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.root, ".gitignore")
	if ign == nil {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(path, []byte(*ign), 0o600); err != nil {
		t.Fatal(err)
	}
	e.git("add", "-A")
	e.git("commit", "-q", "-m", "legacy layout")
}

// Любой вариант .gitignore прежней версии: после миграции служебные файлы .tix не
// попадают в git, журнал на месте, повторный Open .gitignore не меняет.
func TestOpen_LegacyGitignoreVariants_TixFilesIgnored(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name string
		ign  *string
	}{
		{"partial", str(".tracker/lock\n*.swp\n")},
		{"rooted", str("/.tracker/\n")},
		{"bare dir", str(".tracker\n")},
		{"no entry", str("*.swp\n")},
		{"no newline at end", str("*.swp")},
		{"no file", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer guard(t)
			e := newEnv(t)
			legacyBase(t, e, c.ign)
			s := e.open(0)
			ign := filepath.Join(e.root, ".gitignore")
			first, _ := os.ReadFile(ign)
			e.open(0)
			if again, _ := os.ReadFile(ign); string(again) != string(first) {
				t.Errorf("повторный Open изменил .gitignore:\n%s\nстало:\n%s", first, again)
			}
			if got, err := os.ReadFile(filepath.Join(e.root, ".tix", "log")); err != nil || !strings.HasPrefix(string(got), "old log\n") {
				t.Errorf("журнал после миграции: %q, %v", got, err)
			}
			r := mustCreate(t, s, "After")
			if _, err := s.Comment(r.ID, "one", "tester"); err != nil {
				t.Fatalf("Comment: %v", err)
			}
			if out := e.git("ls-files", ".tix/lock", ".tix/log", ".tix/tmp"); out != "" {
				t.Errorf("служебные файлы .tix в git:\n%s\n.gitignore:\n%s", out, first)
			}
			if out := e.git("status", "--porcelain"); out != "" {
				t.Errorf("после записи грязно:\n%s", out)
			}
		})
	}
}

// Обе папки (старый бинарь пересоздал .tracker): .tracker не трогается, а .gitignore
// получает недостающие строки .tix.
func TestOpen_BothDirs_GitignoreGetsTixLines(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	mustCreate(t, e.open(0), "Both")
	if err := os.MkdirAll(filepath.Join(e.root, ".tracker"), 0o750); err != nil {
		t.Fatal(err)
	}
	ign := filepath.Join(e.root, ".gitignore")
	if err := os.WriteFile(ign, []byte(".tracker/lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.open(0)
	got, err := os.ReadFile(ign)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), ".tracker/lock\n") || !strings.Contains(string(got), ".tix/lock\n") {
		t.Errorf(".gitignore при двух папках:\n%s", got)
	}
}
