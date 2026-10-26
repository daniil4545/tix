package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Show_Notes_NoneNoSection(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	out := b.ok("", "show", "T-1").out
	if strings.Contains(out, "Notes") {
		t.Errorf("show of a task without notes prints a notes section:\n%s", out)
	}
}

func TestCLI_Show_Notes_LastFiveByDefault_VerboseAll(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	for i := 1; i <= 7; i++ {
		b.ok("", "note", "T-1", fmt.Sprintf("step %d", i), "--as", "cli:1")
	}
	def := b.ok("", "show", "T-1").out
	if !strings.Contains(def, "Notes (last 5 of 7, -v shows all)") {
		t.Errorf("default show lacks the notes header:\n%s", def)
	}
	if strings.Contains(def, "step 2") || !strings.Contains(def, "step 3") || !strings.Contains(def, "step 7") {
		t.Errorf("default show must print notes 3..7 only:\n%s", def)
	}
	if strings.Index(def, "step 3") > strings.Index(def, "step 7") {
		t.Errorf("notes must go from old to new:\n%s", def)
	}
	if !strings.Contains(def, "cli:1") {
		t.Errorf("note line lacks the actor:\n%s", def)
	}
	all := b.ok("", "show", "T-1", "-v").out
	if !strings.Contains(all, "Notes (7)") || !strings.Contains(all, "step 1") {
		t.Errorf("show -v must print all 7 notes:\n%s", all)
	}
}

func TestCLI_Show_Notes_SurviveMoveAndRename(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "note", "T-1", "before changes")
	b.ok("", "set", "T-1", "title=Renamed")
	b.ok("", "move", "T-1", "doing")
	b.ok("", "note", "T-1", "after changes")
	out := b.ok("", "show", "T-1").out
	if !strings.Contains(out, "before changes") || !strings.Contains(out, "after changes") {
		t.Errorf("notes lost after rename and move:\n%s", out)
	}
}

func TestCLI_Show_Notes_PrefixIDCommentsExcluded(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	for i := 1; i <= 12; i++ {
		b.ok("", "new", fmt.Sprintf("Task %d", i))
	}
	b.ok("", "note", "T-12", "note of twelve")
	b.ok("", "note", "T-1", "note of one")
	b.ok("", "comment", "T-1", "a comment")
	out := b.ok("", "show", "T-1").out
	if !strings.Contains(out, "note of one") {
		t.Errorf("show T-1 lacks its own note:\n%s", out)
	}
	if strings.Contains(out, "note of twelve") || strings.Contains(out, "Notes (2)") {
		t.Errorf("show T-1 lists the note of T-12:\n%s", out)
	}
	if !strings.Contains(out, "Notes (1)") {
		t.Errorf("comment counted as a note or header wrong:\n%s", out)
	}
}

func TestCLI_Show_Notes_AfterFileAndAfterDone_NotInJSON(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "note", "T-1", "decision one")
	b.ok("", "done", "T-1", "--force", "-m", "result")
	text := b.ok("", "show", "T-1").out
	file, _ := os.ReadFile(b.taskFile("T-1"))
	want := "rev "
	if !strings.HasPrefix(text, want) || !strings.Contains(text, string(file)+"\n## Notes (1)\n- ") {
		t.Errorf("notes must follow the file text exactly:\n%s", text)
	}
	if !strings.Contains(text, "decision one") {
		t.Errorf("note lost after done:\n%s", text)
	}
	js := b.ok("", "show", "T-1", "--json").out
	if strings.Contains(js, "decision one") || strings.Contains(js, `"notes"`) {
		t.Errorf("--json must not carry notes:\n%s", js)
	}
}

func TestCLI_Show_Notes_GitFailure_WarnsAndShowsFile(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "note", "T-1", "hidden by failure")
	wrap := filepath.Join(b.root, "gitwrap")
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = log ] && { echo 'fatal: boom' >&2; exit 128; }; done\nexec " + gitBin + " \"$@\"\n"
	if err := os.WriteFile(wrap, []byte(script), 0o755); err != nil { //nolint:gosec // test helper script
		t.Fatal(err)
	}
	b.extra = map[string]string{"TIX_GIT": wrap}
	r := b.ok("", "show", "T-1")
	if !strings.Contains(r.out, "title: Alpha") || strings.Contains(r.out, "Notes") {
		t.Errorf("show must print the file and no notes on a git failure:\n%s", r.out)
	}
	if !strings.Contains(r.err, "notes of T-1 not read") {
		t.Errorf("stderr lacks the warning: %q", r.err)
	}
}

func TestCLI_Edit_RejectsShowNotesSection(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "note", "T-1", "a decision")
	shown := b.ok("", "show", "T-1").out
	rev, text, _ := strings.Cut(shown, "\n")
	rev = strings.TrimPrefix(rev, "rev ")
	r := b.tix(text, "edit", "T-1", "--base", rev)
	if r.code == 0 {
		t.Fatalf("edit accepted the show output with the notes section:\n%s", r.out)
	}
	if after := b.ok("", "show", "T-1").out; strings.Count(after, "a decision") != 1 {
		t.Errorf("notes leaked into the task body:\n%s", after)
	}
}

// The "last N of M" header is built in cli and recognized in task: this test ties the two formats.
func TestCLI_Edit_RejectsShowOutputWithTruncatedNotes(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	for i := 1; i <= 6; i++ {
		b.ok("", "note", "T-1", fmt.Sprintf("step %d", i))
	}
	shown := b.ok("", "show", "T-1").out
	if !strings.Contains(shown, "last 5 of 6") {
		t.Fatalf("show lacks the truncated header:\n%s", shown)
	}
	rev, text, _ := strings.Cut(shown, "\n")
	if r := b.tix(text, "edit", "T-1", "--base", strings.TrimPrefix(rev, "rev ")); r.code == 0 {
		t.Errorf("edit accepted show output with the truncated notes header:\n%s", r.out)
	}
}
