package task_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/task"
)

// The criteria section heading: when the format language changes, edit it here.
const detailsHeading = "## Details"

func parseText(t *testing.T, text string) *task.Task {
	t.Helper()
	tk, err := task.Parse("/b/T-1-x.md", []byte(text))
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, text)
	}
	return tk
}

const fm = "---\nid: T-1\ntitle: X\nproject: p\nstatus: todo\nrank: 1\ncreated: 2026-10-20T10:00:00\n---\n"

func TestClaim_RoundTrip_ClearAndBroken(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	if _, ok := tk.ClaimInfo(); ok {
		t.Fatal("ClaimInfo returned true for a task without a claim")
	}
	until := time.Date(2026, 10, 20, 20, 40, 0, 0, time.Local)
	tk.SetClaim(task.Claim{Actor: "claude:abc", Until: until})
	out := string(tk.Render())
	if !strings.Contains(out, "claim: claude:abc until 2026-10-20T20:40") {
		t.Errorf("no `claim: claude:abc until 2026-10-20T20:40` in the file:\n%s", out)
	}
	back := parseText(t, out)
	got, ok := back.ClaimInfo()
	if !ok || got.Actor != "claude:abc" || !got.Until.Equal(until) {
		t.Errorf("ClaimInfo after parsing = %+v, %v; want claude:abc, %v", got, ok, until)
	}
	if !strings.Contains(out, "x-custom: keep me") {
		t.Errorf("SetClaim lost a foreign field:\n%s", out)
	}
	back.ClearClaim()
	if _, ok := back.ClaimInfo(); ok || strings.Contains(string(back.Render()), "claim:") {
		t.Errorf("ClearClaim did not clear the claim:\n%s", back.Render())
	}
	for _, bad := range []string{"", "garbage", "claude:abc until yesterday", "until 2026-10-20T20:40"} {
		broken := load(t, "valid.md")
		broken.Claim = bad
		if c, ok := broken.ClaimInfo(); ok {
			t.Errorf("ClaimInfo(%q) = %+v, true; want false", bad, c)
		}
	}
}

func TestCriteria_NumberingAndFormats(t *testing.T) {
	defer guard(t)
	body := "## Abstract\n- [ ] not a criterion: not the Details section\n\n" + detailsHeading + "\n" +
		"- [ ] one\n  * [x] two\n- [X] three\njust a line\n-[ ] no space\n- [] empty\n"
	tk := parseText(t, fm+body)
	want := []task.Criterion{{N: 1, Text: "one", Done: false}, {N: 2, Text: "two", Done: true}, {N: 3, Text: "three", Done: true}}
	if got := tk.Criteria(); !reflect.DeepEqual(got, want) {
		t.Errorf("Criteria = %+v, want %+v", got, want)
	}
	if got := parseText(t, fm).Criteria(); len(got) != 0 {
		t.Errorf("task without a body: criteria %+v", got)
	}
}

func TestCheck_MarksOnlyNamed_RepeatUnchanged_BadNumberRejected(t *testing.T) {
	defer guard(t)
	tk := parseText(t, fm+detailsHeading+"\n- [ ] one\n- [ ] two\n- [x] three\n\n## Comments\n")
	changed, err := tk.Check(1)
	if err != nil || !changed {
		t.Fatalf("Check(1) = %v, %v; want true, nil", changed, err)
	}
	out := string(tk.Render())
	if !strings.Contains(out, "- [x] one\n- [ ] two\n- [x] three\n") {
		t.Errorf("wrong criterion checked:\n%s", out)
	}
	if changed, err := tk.Check(1, 3); err != nil || changed {
		t.Errorf("repeated Check(1, 3) = %v, %v; want false, nil", changed, err)
	}
	if changed, err := tk.Check(2, 3); err != nil || !changed {
		t.Errorf("Check(2, 3) = %v, %v; want true, nil (3 is already checked)", changed, err)
	}
	before := string(tk.Render())
	for _, n := range []int{0, 4, -1} {
		if _, err := tk.Check(n); err == nil {
			t.Errorf("Check(%d) accepted: 3 criteria", n)
		}
	}
	if after := string(tk.Render()); after != before {
		t.Errorf("a refused Check changed the task:\n%s\n---\n%s", before, after)
	}
}

func TestSetField_AllowedNames_RenderStable(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	for _, f := range [][2]string{{"title", "New title"}, {"waiting", "need key"}, {"due", "2026-11-01"},
		{"tags", "a,b,c"}, {"ask", "which db?"}, {"external", "JIRA-7"}} {
		if err := tk.SetField(f[0], f[1]); err != nil {
			t.Fatalf("SetField(%s): %v", f[0], err)
		}
	}
	if tk.Title != "New title" || tk.Waiting != "need key" || tk.Due != "2026-11-01" || tk.Ask != "which db?" ||
		tk.External != "JIRA-7" || !reflect.DeepEqual(tk.Tags, []string{"a", "b", "c"}) {
		t.Errorf("fields after SetField: %+v", tk)
	}
	out := string(tk.Render())
	back := parseText(t, out)
	if back.Title != "New title" || back.Waiting != "need key" || !reflect.DeepEqual(back.Tags, []string{"a", "b", "c"}) {
		t.Errorf("fields did not survive Render and Parse:\n%s", out)
	}
	if !strings.Contains(out, "x-custom: keep me") {
		t.Errorf("SetField lost a foreign field:\n%s", out)
	}
	for _, name := range []string{"status", "id", "claim", "rank", "nosuch"} {
		if err := tk.SetField(name, "x"); err == nil {
			t.Errorf("SetField(%q) accepted: name outside title waiting due tags ask external", name)
		}
	}
}

func TestSetSection_ReplacesOrCreates_KeepsRest(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	if err := tk.SetSection("Summary", "Done: all good.\nSecond line."); err != nil {
		t.Fatal(err)
	}
	out := string(tk.Render())
	for _, want := range []string{"Short task summary.", "Done: all good.\nSecond line.", "- [ ] criterion one", "- [x] criterion two"} {
		if !strings.Contains(out, want) {
			t.Errorf("after SetSection no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "First summary line.") {
		t.Errorf("old section text remained:\n%s", out)
	}
	if got := parseText(t, out).Summary(); got != "Done: all good." {
		t.Errorf("Summary() = %q", got)
	}

	// A fresh task: no body, the section is created.
	fresh := parseText(t, fm)
	if err := fresh.SetSection("Abstract", "Short."); err != nil {
		t.Fatal(err)
	}
	if err := fresh.SetSection("Summary", "Result."); err != nil {
		t.Fatal(err)
	}
	if got := parseText(t, string(fresh.Render())); got.Summary() != "Result." || !strings.Contains(got.Body, "Short.") {
		t.Errorf("sections not created in an empty task: %q", got.Body)
	}

	before := string(tk.Render())
	if err := tk.SetSection("Summary", "text\n## Fake heading\nmore"); err == nil {
		t.Error("`## ` in the section text accepted")
	}
	if after := string(tk.Render()); after != before {
		t.Error("a refused SetSection changed the task")
	}
}
