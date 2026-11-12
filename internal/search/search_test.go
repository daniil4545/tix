package search_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/search"
	"github.com/daniil4545/tix/internal/task"
)

func tk(id, title, body string, tags ...string) *task.Task {
	return &task.Task{ID: id, Title: title, Body: body, Project: "p", Status: "open", Tags: tags}
}

func ids(rs []search.Result) string {
	var s []string
	for _, r := range rs {
		s = append(s, r.ID)
	}
	return strings.Join(s, " ")
}

func TestSimilar_TwoStems(t *testing.T) {
	ts := []*task.Task{
		tk("T-1", "Карточка рамки", ""),
		tk("T-2", "Дубли карточки", ""),
		tk("T-3", "Дубли в списке", ""),
	}
	got := search.Similar(search.Query{Text: "карточки дубли", Project: "p"}, ts)
	if ids(got) != "T-2" {
		t.Errorf("got %q, want T-2 (both stems in the title)", ids(got))
	}
}

func TestSimilar_Rune_Stem(t *testing.T) {
	ts := []*task.Task{tk("T-1", "Карточка дубли", "")}
	if got := search.Similar(search.Query{Text: "карточки дубли", Project: "p"}, ts); ids(got) != "T-1" {
		t.Errorf("карточка/карточки: %q", ids(got))
	}
}

func TestSimilar_OneStemNothing(t *testing.T) {
	ts := []*task.Task{tk("T-1", "Карточка", "")}
	if got := search.Similar(search.Query{Text: "карточка", Project: "p"}, ts); got != nil {
		t.Errorf("got %v", got)
	}
	// stop words and the project name do not count as stems
	if got := search.Similar(search.Query{Text: "для tix tarn карточка", Project: "tarn"}, ts); got != nil {
		t.Errorf("stop words counted: %v", got)
	}
}

func TestSimilar_ThreeStemsAbstract(t *testing.T) {
	ts := []*task.Task{
		tk("T-1", "Alpha beta", "## Abstract\ngamma delta here\n## Details\nepsilon"),
		tk("T-2", "Alpha beta", "## Details\ngamma"),
		tk("T-3", "Alpha", "## Abstract\nbeta gamma"),
	}
	got := search.Similar(search.Query{Text: "alpha beta gamma delta epsilon", Project: "p"}, ts)
	// 5 stems: need >=3 and >=60% (3 of 5)
	if ids(got) != "T-1 T-3" {
		t.Errorf("got %q", ids(got))
	}
	got = search.Similar(search.Query{Text: "alpha beta gamma", Project: "p"}, ts)
	if ids(got) != "T-1 T-3" {
		t.Errorf("got %q, want T-1 T-3 (T-2 has no abstract match)", ids(got))
	}
}

func TestSimilar_Filters(t *testing.T) {
	closed := tk("T-2", "Дубли карточки", "")
	closed.Status = "closed"
	other := tk("T-3", "Дубли карточки", "")
	other.Project = "q"
	ts := []*task.Task{
		tk("T-1", "Дубли карточки", ""), closed, other,
		tk("T-4", "Дубли карточки", "", "epic"),
	}
	q := search.Query{Text: "карточки дубли", Project: "p", SelfID: "T-1"}
	if got := search.Similar(q, ts); got != nil {
		t.Errorf("self, closed, other project, epic: %v", ids(got))
	}
	q.All = true
	if got := search.Similar(q, ts); ids(got) != "T-2" {
		t.Errorf("all: %q", ids(got))
	}
}

func TestSimilar_Order(t *testing.T) {
	ts := []*task.Task{
		tk("T-9", "alpha beta gamma", ""),
		tk("T-3", "alpha beta gamma delta", ""),
		tk("T-5", "alpha beta gamma", ""),
	}
	q := search.Query{Text: "alpha beta gamma", Project: "p"}
	for range 3 {
		if got := search.Similar(q, ts); ids(got) != "T-3 T-5 T-9" {
			t.Fatalf("order %q", ids(got))
		}
	}
}

func TestSimilar_Excerpt(t *testing.T) {
	long := strings.Repeat("filler ", 20)
	ts := []*task.Task{tk("T-1", "Alpha beta gamma", "## Abstract\n"+long+"\n")}
	r := search.Similar(search.Query{Text: "alpha beta gamma", Project: "p"}, ts)
	if len(r) != 1 || !strings.Contains(r[0].Excerpt, "Alpha") || len([]rune(r[0].Excerpt)) > 80 {
		t.Errorf("excerpt %+v", r)
	}
}

func TestSimilar_SkipsParents(t *testing.T) {
	parent := tk("T-1", "Дубли карточки", "")
	child := tk("T-2", "Дубли карточки список", "")
	child.Parent = "T-1"
	got := search.Similar(search.Query{Text: "карточки дубли", Project: "p"}, []*task.Task{parent, child})
	if ids(got) != "T-2" {
		t.Errorf("got %q, want T-2 only", ids(got))
	}
}

// The parent is skipped when its only child is not among the candidates (closed, another project).
func TestSimilar_SkipsParentsOfHiddenChildren(t *testing.T) {
	parent := tk("T-1", "Дубли карточки", "")
	got := search.Similar(search.Query{Text: "карточки дубли", Project: "p", Parents: map[string]bool{"T-1": true}}, []*task.Task{parent})
	if ids(got) != "" {
		t.Errorf("got %q, want nothing", ids(got))
	}
}
