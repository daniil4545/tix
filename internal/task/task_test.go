package task_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/task"
)

// The comments section heading: when the format language changes, edit it here.
// The headings in testdata/valid.md (Abstract, Summary, Details) and the summary text in TestParse_ValidFile_Fields too.
const commentsHeading = "## Comments"

func guard(t *testing.T) {
	if r := recover(); r != nil {
		t.Fatalf("panic: %v", r)
	}
}

func load(t *testing.T, name string) *task.Task {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := task.Parse("testdata/"+name, data)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return tk
}

func TestParse_ValidFile_Fields(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	if tk.ID != "T-12" || tk.Title != "Natasha benchmark" || tk.Project != "tracker" || tk.Status != "doing" || tk.Rank != 3.5 {
		t.Errorf("frontmatter fields: %+v", tk)
	}
	if len(tk.Tags) != 2 || tk.Tags[0] != "asr" || tk.Tags[1] != "bench" {
		t.Errorf("tags %v, want [asr bench]", tk.Tags)
	}
	if tk.Created.Year() != 2026 || tk.Created.Month() != 10 || tk.Created.Day() != 20 || tk.Created.Hour() != 14 || tk.Created.Minute() != 5 {
		t.Errorf("created %v, want local 2026-10-20 14:05", tk.Created)
	}
	if !strings.Contains(tk.Body, "First summary line.") || !strings.Contains(tk.Body, "criterion two") {
		t.Errorf("Body without the file sections: %q", tk.Body)
	}
	if strings.Contains(tk.Body, commentsHeading) || strings.Contains(tk.Body, "x-custom") {
		t.Errorf("Body contains frontmatter or comments: %q", tk.Body)
	}
	if tk.Path != "testdata/valid.md" {
		t.Errorf("Path %q", tk.Path)
	}
	if got := tk.Summary(); got != "First summary line." {
		t.Errorf("Summary %q, want the first line of the summary section", got)
	}
}

func TestParse_Broken_ErrorHasPathAndLine(t *testing.T) {
	defer guard(t)
	data, _ := os.ReadFile("testdata/broken.md")
	_, err := task.Parse("/base/T-3-broken.md", data)
	if err == nil {
		t.Fatal("broken frontmatter parsed without an error")
	}
	if !strings.Contains(err.Error(), "T-3-broken.md:3") {
		t.Errorf("error %q, want `T-3-broken.md:3` (a file line, not a frontmatter line)", err)
	}
}

func TestParse_NoFrontmatter_Error(t *testing.T) {
	defer guard(t)
	if _, err := task.Parse("/b/T-4-x.md", []byte("just text\n")); err == nil || !strings.Contains(err.Error(), "T-4-x.md") {
		t.Errorf("file without frontmatter: error %v, want an error with the path", err)
	}
}

func TestRender_KeepsUnknownFieldsOrderAndIsStable(t *testing.T) {
	defer guard(t)
	out := string(load(t, "valid.md").Render())
	if !strings.Contains(out, "x-custom: keep me") {
		t.Errorf("foreign field lost:\n%s", out)
	}
	last := -1
	for _, k := range []string{"id:", "title:", "project:", "status:", "rank:", "created:", "tags:", "x-custom:"} {
		i := strings.Index(out, "\n"+k)
		if i < 0 {
			i = strings.Index(out, k)
		}
		if i <= last {
			t.Errorf("field order broken at %q:\n%s", k, out)
		}
		last = i
	}
	again, err := task.Parse("x.md", []byte(out))
	if err != nil {
		t.Fatalf("Render output cannot be parsed: %v\n%s", err, out)
	}
	if second := string(again.Render()); second != out {
		t.Errorf("Render is not stable:\n%s\n---\n%s", out, second)
	}
	for _, s := range []string{"Short task summary.", "- [ ] criterion one", "- [x] criterion two"} {
		if !strings.Contains(out, s) {
			t.Errorf("body lost: no %q", s)
		}
	}
}

func TestSetBody_RejectsCommentsHeader(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	if err := tk.SetBody("text\n" + commentsHeading + "\nforeign\n"); err == nil {
		t.Error("comments heading in the body accepted")
	}
	if err := tk.SetBody("new body\n"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tk.Render()), "new body") {
		t.Error("SetBody not reflected in Render")
	}
}

func TestAddComment_RoundTrip_ArrowEscaped(t *testing.T) {
	defer guard(t)
	tk := load(t, "valid.md")
	at := time.Date(2026, 10, 20, 14, 30, 0, 0, time.Local)
	tk.AddComment(at, "claude:abc", "first")
	tk.AddComment(at.Add(time.Hour), "alice", "line 1\nline 2 with --> an arrow")
	out := string(tk.Render())
	if !strings.Contains(out, "<!-- c: 2026-10-20T14:30 | claude:abc -->") {
		t.Errorf("no marker `<!-- c: 2026-10-20T14:30 | claude:abc -->`:\n%s", out)
	}
	if n, m := strings.Count(out, "-->"), len(regexp.MustCompile(`<!-- c: `).FindAllString(out, -1)); n != m {
		t.Errorf("`-->` in the text is not escaped: closing %d, markers %d\n%s", n, m, out)
	}
	back, err := task.Parse("x.md", []byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Comments) != 2 {
		t.Fatalf("comments %d, want 2:\n%s", len(back.Comments), out)
	}
	c := back.Comments[0]
	if c.Actor != "claude:abc" || !c.At.Equal(at) || strings.TrimSpace(c.Text) != "first" {
		t.Errorf("comment 1: %+v", c)
	}
	if back.Comments[1].Actor != "alice" || !strings.Contains(back.Comments[1].Text, "line 2") {
		t.Errorf("comment 2: %+v", back.Comments[1])
	}
	if strings.Contains(back.Body, "first") {
		t.Errorf("comment got into Body: %q", back.Body)
	}
}

func TestFileName_SlugRules(t *testing.T) {
	defer guard(t)
	cases := []struct{ title, slug string }{
		{"Жук хата цап чан шар", "zhuk-khata-tsap-chan-shar"},
		{"щука юла яма", "shchuka-yula-yama"},
		{"объект соль", "obekt-sol"},
		{"Hello,   World!!", "hello-world"},
		{"  --x--  ", "x"},
		{"Замер Natasha: 2 звонка", "zamer-natasha-2-zvonka"},
		{"!!!", "task"},
		{"", "task"},
		{strings.Repeat("a", 60), strings.Repeat("a", 40)},
		{strings.Repeat("щ", 20), strings.Repeat("shch", 10)},
	}
	re := regexp.MustCompile(`^T-12[ -](.*)\.md$`)
	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			defer guard(t)
			name := task.FileName("T-12", c.title)
			m := re.FindStringSubmatch(name)
			if m == nil {
				t.Fatalf("FileName(T-12, %q) = %q, want `T-12-<slug>.md`", c.title, name)
			}
			if m[1] != c.slug {
				t.Errorf("slug %q, want %q", m[1], c.slug)
			}
		})
	}
}

func TestTask_JSON_CreatedIsLocalLayout(t *testing.T) {
	defer guard(t)
	data, err := os.ReadFile("testdata/valid.md")
	if err != nil {
		t.Fatal(err)
	}
	tk, err := task.Parse("/b/T-12.md", data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"created":"2026-10-20T14:05:09"`) {
		t.Errorf("created not in local layout 2006-01-02T15:04:05: %s", out)
	}
}

func TestSetBody_RejectsShowNotesHeading(t *testing.T) {
	defer guard(t)
	reject := []string{"a\n\n## Notes (3)\n- x\n", "a\n## Notes (last 5 of 7, -v shows all)\r\n- x\n"}
	accept := []string{"a\n## Notes\n", "a\n## Notes (my list)\n", "a ## Notes (3)\n"}
	for _, body := range reject {
		if err := load(t, "valid.md").SetBody(body); err == nil {
			t.Errorf("show notes heading in body %q accepted", body)
		}
	}
	for _, body := range accept {
		if err := load(t, "valid.md").SetBody(body); err != nil {
			t.Errorf("body %q rejected: %v", body, err)
		}
	}
}

func TestSetBody_CommentsHeadingVariants(t *testing.T) {
	defer guard(t)
	reject := []string{commentsHeading, "a\n" + commentsHeading + " \t\r\nb", "a\n" + commentsHeading + "\r\nb", "x\n\n" + commentsHeading}
	accept := []string{"a " + commentsHeading, "a\n" + commentsHeading + "X\n", "a\n " + commentsHeading + "\n", "a\n" + commentsHeading + " x\n"}
	for _, body := range reject {
		tk := load(t, "valid.md")
		if err := tk.SetBody(body); err == nil {
			t.Errorf("heading in body %q accepted", body)
		}
	}
	for _, body := range accept {
		tk := load(t, "valid.md")
		if err := tk.SetBody(body); err != nil {
			t.Errorf("body %q rejected: %v", body, err)
		}
	}
}
