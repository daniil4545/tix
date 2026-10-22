package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func projectOf(t *testing.T, b *box, id string) string {
	t.Helper()
	var js struct{ Project string }
	if err := json.Unmarshal([]byte(b.ok("", "show", id, "--json").out), &js); err != nil {
		t.Fatal(err)
	}
	return js.Project
}

// spec: T-73 scenario 6
func TestCLI_New_ProjectFlagStoredLowerCase(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha", "--project", "Galera")
	if got := projectOf(t, b, "T-1"); got != "galera" {
		t.Errorf("project %q, want galera", got)
	}
}

// spec: T-73 scenario 6
func TestCLI_SetProject_LowerCasedAndChanged(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha", "--project", "other")
	r := b.ok("", "set", "T-1", "project=Tarn")
	if !strings.Contains(r.out, "project") {
		t.Errorf("set output %q does not name the field", r.out)
	}
	if got := projectOf(t, b, "T-1"); got != "tarn" {
		t.Errorf("project %q, want tarn", got)
	}
}

// spec: T-73 scenario 6
func TestCLI_SetProject_EmptyOrSeveralLines_Refused(t *testing.T) {
	defer guard(t)
	for _, value := range []string{"", "   ", "a\nb"} {
		t.Run(value, func(t *testing.T) {
			b := newBox(t)
			b.ok("", "new", "Alpha", "--project", "other")
			before := b.snapshot()
			b.rule("set", "T-1", "project="+value)
			ensure(t, b.snapshot() == before, "the refused set changed the store")
		})
	}
}

// The project filter ignores case: the folder name may be capitalized, new tasks are stored in lower
// case and old files may still hold capitals.
func TestCLI_ListProject_FilterIgnoresCase(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Fresh", "--project", "Galera")
	b.ok("", "new", "Old", "--project", "tarn")
	b.rewrite("T-2", func(s string) string { return strings.Replace(s, "project: tarn", "project: Tarn", 1) })
	for _, c := range []struct{ filter, want string }{
		{"Galera", "T-1 Fresh"}, {"galera", "T-1 Fresh"}, {"tarn", "T-2 Old"}, {"Tarn", "T-2 Old"},
	} {
		if got := b.ok("", "list", "--project", c.filter).out; !strings.Contains(got, c.want) {
			t.Errorf("list --project %s: %q, want it to contain %q", c.filter, got, c.want)
		}
	}
}
