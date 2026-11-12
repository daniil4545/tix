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

func TestKinds_FiveInBoardOrder(t *testing.T) {
	defer guard(t)
	want := []string{"idea", "open", "in_progress", "blocked", "closed"}
	if !reflect.DeepEqual(config.Kinds, want) {
		t.Errorf("statuses %v, want (spec 2.5) %v", config.Kinds, want)
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

func TestCheckStatus_FiveAccepted_OldWordsHinted(t *testing.T) {
	defer guard(t)
	for _, s := range config.Kinds {
		if err := config.Default().CheckStatus(s); err != nil {
			t.Errorf("Check(%q) = %v", s, err)
		}
	}
	for old, want := range map[string]string{"todo": "open", "ready": "open", "doing": "in_progress",
		"active": "in_progress", "waiting": "blocked", "inbox": "idea", "done": "closed"} {
		err := config.Default().CheckStatus(old)
		if err == nil || !strings.Contains(err.Error(), "maybe "+want+"?") {
			t.Errorf("Check(%q) = %v, want the hint %q", old, err, "maybe "+want+"?")
		}
	}
	if err := config.Default().CheckStatus("later"); err == nil || strings.Contains(err.Error(), "maybe") {
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

func TestRender_KeepsProjectsPathsForeign(t *testing.T) {
	defer guard(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	src := "[projects]\ndir = \"имя\"\n\n[paths]\nalpha = \"/a/b\"\nnone = \"\"\n\n[adapter]\nkind = \"x\"\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, c.Render(), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := config.Load(p)
	if err != nil {
		t.Fatalf("%v\n%s", err, c.Render())
	}
	if out.Projects["dir"] != "имя" || out.Paths["alpha"] != "/a/b" || len(out.Paths) != 2 {
		t.Errorf("lost: %+v", out)
	}
	if got := string(out.Render()); !strings.Contains(got, "[adapter]") || !strings.Contains(got, `kind = "x"`) {
		t.Errorf("foreign section lost:\n%s", got)
	}
}

func TestRenderKeepsTopLevelScalarOutOfPaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	src := "version = \"1\"\n\n[projects]\ntarn = \"main\"\n\n[paths]\ntarn = \"/abs/tarn\"\n"
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, c.Render(), 0o600); err != nil {
		t.Fatal(err)
	}
	c2, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := c2.Paths["version"]; bad || len(c2.Paths) != 1 || c2.Paths["tarn"] != "/abs/tarn" {
		t.Fatalf("paths after render: %v\n%s", c2.Paths, c.Render())
	}
}

func TestLoadLowersPathKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[paths]\nTarn = \"/abs/tarn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Paths["tarn"] != "/abs/tarn" || len(c.Paths) != 1 {
		t.Fatalf("paths: %v", c.Paths)
	}
}

func loadSrc(t *testing.T, src string) (*config.Config, string, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	return c, p, err
}

func names(c *config.Config) []string {
	var n []string
	for _, s := range c.StatusList() {
		n = append(n, s.Name)
	}
	return n
}

func TestS1_NoStatusSection_FiveDefaults_RenderSilent(t *testing.T) {
	defer guard(t)
	c, _, err := loadSrc(t, "[projects]\ntracker = \"трекер\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names(c), config.Kinds) {
		t.Errorf("statuses %v", names(c))
	}
	for _, k := range config.Kinds {
		if c.DefaultOf(k) != k {
			t.Errorf("default of %s = %q", k, c.DefaultOf(k))
		}
		if got, ok := c.KindOf(k); !ok || got != k {
			t.Errorf("kind of %s = %q %v", k, got, ok)
		}
	}
	if _, ok := c.KindOf("review"); ok {
		t.Error("review should not exist")
	}
	out := string(c.Render())
	if strings.Contains(out, "status") || strings.Contains(out, "format") {
		t.Errorf("render added a section:\n%s", out)
	}
}

func TestS2_InvalidStatuses_ErrorHasFileAndLine(t *testing.T) {
	defer guard(t)
	st := func(n, k string) string { return "[[status]]\nname = \"" + n + "\"\nkind = \"" + k + "\"\n\n" }
	base := st("idea", "idea") + st("open", "open") + st("in_progress", "in_progress") + st("blocked", "blocked")
	cases := map[string]string{
		"kind without status": base + st("shut", "idea"),
		"duplicate name":      base + st("closed", "closed") + st("closed", "closed"),
		"closed of kind open": base + st("closed", "open") + st("fin", "closed"),
		"bad name":            base + st("closed", "closed") + st("Bad-Name", "open"),
		"long name":           base + st("closed", "closed") + st("abcdefghijklmnopqrstu", "open"),
		"unknown kind":        base + st("closed", "closed") + st("x", "weird"),
	}
	for name, src := range cases {
		_, p, err := loadSrc(t, src)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !regexp.MustCompile("^" + regexp.QuoteMeta(p) + `:\d+: `).MatchString(err.Error()) {
			t.Errorf("%s: %v, want path:line", name, err)
		}
	}
}

func TestS2_ErrorLineIsTheSectionAtFault(t *testing.T) {
	defer guard(t)
	src := "[[status]] # first\nname = \"idea\"\nkind = \"idea\"\n\n" + // line 1
		"[[ status ]]\nname = \"open\"\nkind = \"open\"\n\n" + // line 5
		"[[status]]\nname = \"Bad-Name\"\nkind = \"open\"\n" // line 9
	_, p, err := loadSrc(t, src)
	if err == nil || !strings.HasPrefix(err.Error(), p+":9: ") {
		t.Fatalf("got %v, want line 9", err)
	}
}

func TestLoadRender_SixStatuses_Format2_RoundTrip(t *testing.T) {
	defer guard(t)
	src := "format = 2\n\n[projects]\ntracker = \"трекер\"\n\n"
	for _, s := range [][2]string{{"idea", "idea"}, {"open", "open"}, {"in_progress", "in_progress"},
		{"review", "in_progress"}, {"blocked", "blocked"}, {"closed", "closed"}} {
		src += "[[status]]\nname = \"" + s[0] + "\"\nkind = \"" + s[1] + "\"\n\n"
	}
	c, _, err := loadSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"idea", "open", "in_progress", "review", "blocked", "closed"}
	if c.Format != 2 || !reflect.DeepEqual(names(c), want) {
		t.Fatalf("format %d, names %v", c.Format, names(c))
	}
	if k, _ := c.KindOf("review"); k != "in_progress" || c.DefaultOf("in_progress") != "in_progress" {
		t.Errorf("review kind %q, default %q", k, c.DefaultOf("in_progress"))
	}
	if err := c.CheckStatus("nope"); err == nil || !strings.Contains(err.Error(), "review") {
		t.Errorf("CheckStatus: %v", err)
	}
	c2, _, err := loadSrc(t, string(c.Render()))
	if err != nil {
		t.Fatal(err)
	}
	if c2.Format != 2 || !reflect.DeepEqual(c2.StatusList(), c.StatusList()) || c2.Projects["tracker"] != "трекер" {
		t.Errorf("round trip lost data:\n%s", c.Render())
	}
}

func TestStatusEdit_AddRenameRemove(t *testing.T) {
	defer guard(t)
	list := func(c *config.Config) string { return strings.Join(names(c), " ") }
	c := config.Default()
	if err := c.AddStatus("review", config.InProgress); err != nil {
		t.Fatal(err)
	}
	if got := list(c); got != "idea open in_progress review blocked closed" {
		t.Errorf("after add: %s", got)
	}
	if c.DefaultOf(config.InProgress) != "in_progress" {
		t.Error("add moved the default of the kind")
	}
	if err := c.RenameStatus("review", "qa"); err != nil || list(c) != "idea open in_progress qa blocked closed" {
		t.Errorf("rename: %v, %s", err, list(c))
	}
	if err := c.RemoveStatus("qa"); err != nil || list(c) != "idea open in_progress blocked closed" {
		t.Errorf("remove: %v, %s", err, list(c))
	}
	if err := c.RemoveStatus("closed"); err == nil || !strings.Contains(err.Error(), "last of the kind closed") {
		t.Errorf("remove last of kind: %v", err)
	}
	if err := c.RenameStatus("closed", "open"); err == nil || !strings.Contains(err.Error(), "kind word") {
		t.Errorf("rename to a kind word of another kind: %v", err)
	}
	if err := c.RenameStatus("nope", "x"); err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Errorf("rename unknown: %v", err)
	}
}
