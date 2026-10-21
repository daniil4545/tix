package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/daniil4545/tix/internal/config"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

func TestDefault_FiveRolesOneColumnEach(t *testing.T) {
	defer guard(t)
	want := []config.Column{
		{Name: "idea", Role: "inbox"}, {Name: "todo", Role: "ready"}, {Name: "doing", Role: "active"},
		{Name: "blocked", Role: "waiting"}, {Name: "done", Role: "closed"},
	}
	if got := config.Default().Columns; !reflect.DeepEqual(got, want) {
		t.Errorf("default columns %+v, want (spec 2.5) %+v", got, want)
	}
}

func TestLoad_MissingFile_Default(t *testing.T) {
	defer guard(t)
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Columns, config.Default().Columns) {
		t.Errorf("no file: %+v, want Default", c.Columns)
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
		t.Errorf("columns %+v, want %+v", c.Columns, want)
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
		t.Fatalf("Load cannot parse Render output: %v\n%s", err, in.Render())
	}
	if !reflect.DeepEqual(out.Columns, in.Columns) || !reflect.DeepEqual(out.Projects, in.Projects) {
		t.Errorf("Render-Load round trip: %+v, want %+v", out, in)
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
		t.Fatal("broken TOML parsed without an error")
	}
	if !regexp.MustCompile(`config\.toml:\d+`).MatchString(err.Error()) {
		t.Errorf("error %q, want `config.toml:<line>`", err)
	}
}

const customConfig = `[[column]]
name = "ideas"
role = "inbox"

[[column]]
name = "todo"
role = "ready"

[[column]]
name = "review"

[[column]]
name = "doing"
role = "active"
`

func TestByRoleAndRoleOf(t *testing.T) {
	defer guard(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(customConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if col, ok := c.ByRole("inbox"); !ok || col.Name != "ideas" {
		t.Errorf("ByRole(inbox) = %+v, %v; want ideas", col, ok)
	}
	if col, ok := c.ByRole("waiting"); ok {
		t.Errorf("ByRole(waiting) = %+v, true; the config has no such role", col)
	}
	for name, want := range map[string]string{"ideas": "inbox", "doing": "active", "review": "", "nosuch": ""} {
		if got := c.RoleOf(name); got != want {
			t.Errorf("RoleOf(%q) = %q, want %q", name, got, want)
		}
	}
	if got := config.Default().RoleOf("blocked"); got != "waiting" {
		t.Errorf("RoleOf(blocked) in the default config = %q, want waiting", got)
	}
}

// Resolve: a column name wins over a role; a role gives the column with that role; an unknown name stays as is.
func TestResolve_NameBeforeRole(t *testing.T) {
	defer guard(t)
	c := &config.Config{Columns: []config.Column{
		{Name: "todo", Role: "ready"}, {Name: "ready", Role: "inbox"}, {Name: "blocked", Role: "waiting"},
	}}
	for in, want := range map[string]string{"waiting": "blocked", "ready": "ready", "todo": "todo", "nope": "nope"} {
		if got := c.Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}
