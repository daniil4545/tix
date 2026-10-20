package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"tracker/internal/config"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("паника: %v", r)
	}
}

func TestDefault_FiveRolesOneColumnEach(t *testing.T) {
	defer guard(t)
	want := []config.Column{
		{Name: "idea", Role: "inbox"}, {Name: "todo", Role: "ready"}, {Name: "doing", Role: "active"},
		{Name: "blocked", Role: "waiting"}, {Name: "done", Role: "closed"},
	}
	if got := config.Default().Columns; !reflect.DeepEqual(got, want) {
		t.Errorf("колонки по умолчанию %+v, ждали (спека 2.5) %+v", got, want)
	}
}

func TestLoad_MissingFile_Default(t *testing.T) {
	defer guard(t)
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Columns, config.Default().Columns) {
		t.Errorf("нет файла: %+v, ждали Default", c.Columns)
	}
}

func TestLoad_CustomFile_ColumnsOrderAndProjects(t *testing.T) {
	defer guard(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	src := "[[column]]\nname = \"backlog\"\nrole = \"inbox\"\n\n[[column]]\nname = \"wip\"\nrole = \"active\"\n\n[[column]]\nname = \"misc\"\n\n[projects]\ntracker = \"трекер\"\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Column{{Name: "backlog", Role: "inbox"}, {Name: "wip", Role: "active"}, {Name: "misc"}}
	if !reflect.DeepEqual(c.Columns, want) {
		t.Errorf("колонки %+v, ждали %+v", c.Columns, want)
	}
	if c.Projects["tracker"] != "трекер" {
		t.Errorf("projects %v", c.Projects)
	}
}

func TestRender_RoundTrip(t *testing.T) {
	defer guard(t)
	in := &config.Config{
		Columns:  []config.Column{{Name: "a", Role: "inbox"}, {Name: "б", Role: "closed"}},
		Projects: map[string]string{"dir": "имя"},
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, in.Render(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(p)
	if err != nil {
		t.Fatalf("Render не разбирается Load: %v\n%s", err, in.Render())
	}
	if !reflect.DeepEqual(out.Columns, in.Columns) || !reflect.DeepEqual(out.Projects, in.Projects) {
		t.Errorf("круг Render-Load: %+v, ждали %+v", out, in)
	}
}

func TestLoad_Broken_ErrorHasPathAndLine(t *testing.T) {
	defer guard(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[[column]]\nname = \"idea\"\nrole = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(p)
	if err == nil {
		t.Fatal("битый TOML разобран без ошибки")
	}
	if !regexp.MustCompile(`config\.toml:\d+`).MatchString(err.Error()) {
		t.Errorf("ошибка %q, ждали `config.toml:<строка>`", err)
	}
}
