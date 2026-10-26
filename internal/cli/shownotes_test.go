package cli_test

import (
	"fmt"
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

func TestCLI_Show_Notes_OtherTaskAndCommentsExcluded(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Alpha")
	b.ok("", "new", "Beta")
	b.ok("", "note", "T-2", "other task note")
	b.ok("", "comment", "T-1", "a comment")
	out := b.ok("", "show", "T-1").out
	if strings.Contains(out, "other task note") || strings.Contains(out, "Notes") {
		t.Errorf("show T-1 must not list foreign notes or comments as notes:\n%s", out)
	}
}
