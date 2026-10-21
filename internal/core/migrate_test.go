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
