package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/config"
)

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

func TestStatuses_FiveInBoardOrder(t *testing.T) {
	defer guard(t)
	want := []string{"idea", "open", "in_progress", "blocked", "closed"}
	if !reflect.DeepEqual(config.Statuses, want) {
		t.Errorf("statuses %v, want (spec 2.5) %v", config.Statuses, want)
	}
}

func TestLoad_MissingFile_Default(t *testing.T) {
	defer guard(t)
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Projects) != 0 {
		t.Errorf("no file: %+v, want Default", c)
	}
}

func TestLoad_OldColumnsSkipped_ProjectsKept(t *testing.T) {
	defer guard(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	src := "[[column]]\nname = \"backlog\"\nrole = \"inbox\"\n\n[projects]\ntracker = \"трекер\"\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Projects["tracker"] != "трекер" {
		t.Errorf("projects %v", c.Projects)
	}
}

func TestRender_RoundTrip(t *testing.T) {
	defer guard(t)
	in := &config.Config{Projects: map[string]string{"dir": "имя"}}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, in.Render(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load cannot parse Render output: %v\n%s", err, in.Render())
	}
	if !reflect.DeepEqual(out.Projects, in.Projects) {
		t.Errorf("Render-Load round trip: %+v, want %+v", out, in)
	}
	if strings.Contains(string(in.Render()), "column") {
		t.Errorf("Render writes columns:\n%s", in.Render())
	}
}

func TestLoad_Broken_ErrorHasPathAndLine(t *testing.T) {
	defer guard(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[projects]\nname = \n"), 0o644); err != nil {
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

func TestCheck_FiveAccepted_OldWordsHinted(t *testing.T) {
	defer guard(t)
	for _, s := range config.Statuses {
		if err := config.Check(s); err != nil {
			t.Errorf("Check(%q) = %v", s, err)
		}
	}
	for old, want := range map[string]string{"todo": "open", "ready": "open", "doing": "in_progress",
		"active": "in_progress", "waiting": "blocked", "inbox": "idea", "done": "closed"} {
		err := config.Check(old)
		if err == nil || !strings.Contains(err.Error(), "maybe "+want+"?") {
			t.Errorf("Check(%q) = %v, want the hint %q", old, err, "maybe "+want+"?")
		}
	}
	if err := config.Check("later"); err == nil || strings.Contains(err.Error(), "maybe") {
		t.Errorf("Check(later) = %v, want a refusal without a hint", err)
	}
}

func TestLegacyMap_ByRoleNotByName(t *testing.T) {
	defer guard(t)
	src := "[[column]]\nname = \"ideas\"\nrole = \"inbox\"\n[[column]]\nname = \"open\"\nrole = \"active\"\n" +
		"[[column]]\nname = \"review\"\n[[column]]\nname = \"fin\"\nrole = \"closed\"\n"
	m, has, err := config.LegacyMap(src)
	if err != nil || !has {
		t.Fatalf("LegacyMap: %v, has %v", err, has)
	}
	want := map[string]string{"ideas": "idea", "open": "in_progress", "fin": "closed"}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("map %v, want %v (a column without a role is absent)", m, want)
	}
}

func TestLegacyMap_NoColumns_DefaultNames(t *testing.T) {
	defer guard(t)
	m, has, err := config.LegacyMap("[projects]\na = \"b\"\n")
	if err != nil || has {
		t.Fatalf("LegacyMap: %v, has %v", err, has)
	}
	want := map[string]string{"idea": "idea", "todo": "open", "doing": "in_progress", "blocked": "blocked", "done": "closed"}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("map %v, want %v", m, want)
	}
}
