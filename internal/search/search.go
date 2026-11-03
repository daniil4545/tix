// Package search finds tasks similar to a query by shared word stems. No IO.
package search

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

const (
	minWord    = 3 // runes
	stemLen    = 5 // runes kept as the stem
	minShare   = 0.6
	minCommon  = 3
	excerptLen = 80 // runes
)

var stop = map[string]bool{
	"для": true, "что": true, "как": true, "при": true,
	"the": true, "and": true, "for": true, "with": true, "tix": true,
}

// Query is what to look for. Project limits the candidates and its name is a stop word.
type Query struct {
	Text    string
	Project string
	SelfID  string // a task that never matches itself
	All     bool   // closed tasks are candidates too
}

// Result is one similar task.
type Result struct {
	ID      string
	Title   string
	Common  int     // shared stems
	Share   float64 // Common / stems of the query
	Excerpt string
}

type word struct {
	stem       string
	start, end int // rune offsets in the text
}

func words(text, project string) []word {
	rs := []rune(text)
	proj := strings.ToLower(project)
	var out []word
	for i := 0; i < len(rs); {
		if !isWordRune(rs[i]) {
			i++
			continue
		}
		j := i
		for j < len(rs) && isWordRune(rs[j]) {
			j++
		}
		w := []rune(strings.ToLower(string(rs[i:j])))
		if len(w) >= minWord && !stop[string(w)] && string(w) != proj {
			out = append(out, word{string(w[:min(len(w), stemLen)]), i, j})
		}
		i = j
	}
	return out
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func stems(ws []word) map[string]bool {
	m := make(map[string]bool, len(ws))
	for _, w := range ws {
		m[w.stem] = true
	}
	return m
}

// abstract returns the text under "## Abstract".
func abstract(body string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimRight(line, " \t\r") == "## Abstract"
			continue
		}
		if in {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Similar returns the tasks resembling the query, best first.
func Similar(q Query, tasks []*task.Task) []Result {
	qs := stems(words(q.Text, q.Project))
	n := len(qs)
	if n < 2 {
		return nil
	}
	var out []Result
	for _, t := range tasks {
		if t.ID == q.SelfID || t.IsEpic() || (t.Status == config.Closed && !q.All) {
			continue
		}
		if q.Project != "" && t.Project != q.Project {
			continue
		}
		if r, ok := match(t, qs, q.Project); ok {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Result) int {
		if a.Common != b.Common {
			return b.Common - a.Common
		}
		if a.Share != b.Share {
			if a.Share > b.Share {
				return -1
			}
			return 1
		}
		return num(a.ID) - num(b.ID)
	})
	return out
}

// match applies the similarity rule of Similar to one task.
func match(t *task.Task, qs map[string]bool, project string) (Result, bool) {
	n := len(qs)
	text := t.Title
	if n > 2 {
		text += "\n" + abstract(t.Body)
	}
	common, first := 0, -1
	seen := map[string]bool{}
	for _, w := range words(text, project) {
		if !qs[w.stem] || seen[w.stem] {
			continue
		}
		seen[w.stem] = true
		common++
		if first < 0 {
			first = w.start
		}
	}
	share := float64(common) / float64(n)
	if n == 2 && common < 2 || n > 2 && (common < minCommon || share < minShare) {
		return Result{}, false
	}
	return Result{ID: t.ID, Title: t.Title, Common: common, Share: share, Excerpt: excerpt(text, first)}, true
}

func num(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "T-"))
	return n
}

// excerpt is up to excerptLen runes around the rune offset at.
func excerpt(text string, at int) string {
	rs := []rune(strings.Join(strings.Fields(text), " "))
	// offsets were counted in the raw text; re-find the word start in the squashed one
	raw := []rune(text)
	if at > len(raw) {
		at = len(raw)
	}
	pre := len([]rune(strings.Join(strings.Fields(string(raw[:at])), " ")))
	if at > 0 && pre > 0 && unicode.IsSpace(raw[at-1]) {
		pre++ // the joined prefix lost its trailing space
	}
	at = pre
	lo := max(0, at-excerptLen/3)
	hi := min(len(rs), lo+excerptLen)
	lo = max(0, hi-excerptLen)
	return string(rs[lo:hi])
}
