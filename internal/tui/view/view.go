// Package view рисует доску и карточку строкой; без IO и без состояния.
package view

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"tracker/internal/history"
)

// Row - строка колонки доски. Третья строка карточки (пульс) - первое из: вопрос Ask (жёлтый),
// заметка Note (готовая строка "<возраст> <текст>", пусто вне колонки active), Summary.
type Row struct {
	ID, Project, Title, Summary, Meta string
	Ask, Note                         string
	Star                              bool // звёздочка в первой строке; ненулевой Ask включает её сам
	Grabbed                           bool // карточка захвачена: подсветка и маркер "=" вместо ">"
}

// UnknownColumn - имя колонки для задач со статусом, которого нет в config.toml.
const UnknownColumn = "?"

// Col - колонка доски.
type Col struct {
	Name string
	Rows []Row
}

// Board - всё, что нужно для кадра доски. W, H <= 0: размер не ограничен.
type Board struct {
	Project  string
	Total    int
	Cols     []Col
	Col, Row int
	W, H     int
	Status   string
	IsErr    bool
	ByDue    bool // колонки отсортированы по сроку, не по rank
	// FirstCol и RowTop - прежнее положение окон (первая показанная колонка и верхняя карточка
	// выбранной колонки). Нули дают окна по курсору; окно сдвигается, только когда курсор вышел.
	FirstCol, RowTop int
}

// Card - всё, что нужно для кадра карточки. Scroll - смещение тела в строках; кадр
// сдвигает его сам, чтобы строка Cursor (индекс в History) была видна. W, H <= 0: размер не ограничен.
type Card struct {
	ID, Title, Column, Fields, Ask, Abstract, Summary, Details, Checks string
	History                                                            []history.Entry
	Cursor, Scroll, W, H                                               int
	CritAt                                                             []int // строки-критерии в Details (индексы по "\n"); Cursor считает их перед историей
	Status                                                             string
	IsErr                                                              bool
}

// Цвета макета: основной - цвет терминала, приглушённый, акцент, жёлтый (нужно действие), оранжевый (ошибка).
var (
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#69756F"))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7FD1C3"))
	askStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E9CF6A"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A06A"))
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#7FD1C3")).Foreground(lipgloss.Color("#111416"))
	selAskStyle = lipgloss.NewStyle().Background(lipgloss.Color("#7FD1C3")).Foreground(lipgloss.Color("#E9CF6A"))
	grabStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#E9CF6A")).Foreground(lipgloss.Color("#111416"))
)

// RenderInput рисует строку ввода: приглашение, текст и курсор. Длинный текст показывается хвостом.
func RenderInput(prompt, text string, w int) string {
	prompt = clean(prompt)
	text = clean(text)
	if w > 0 {
		room := max(w-lipgloss.Width(prompt)-2, 0)
		text = tail(text, room)
	}
	return accentStyle.Render(prompt) + " " + text + selStyle.Render(" ")
}

// statusLine рисует строку состояния; пустая заменяется подсказкой клавиш.
func statusLine(status, hint string, isErr bool, w int) string {
	if status == "" {
		return dimStyle.Render(truncate(hint, w))
	}
	status = truncate(clean(status), w)
	if isErr {
		return errStyle.Render(status)
	}
	return status
}

// clean заменяет управляющие знаки пробелами: текст задач приходит из файлов и не должен двигать курсор терминала.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// truncate обрезает строку до n колонок; n <= 0 - без обрезки.
func truncate(s string, n int) string {
	if n <= 0 || lipgloss.Width(s) <= n {
		return s
	}
	head, _ := cut(s, n)
	return head
}

// tail оставляет последние n колонок строки.
func tail(s string, n int) string {
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > n {
		r = r[1:]
	}
	return string(r)
}

// cut делит строку на голову шириной до n колонок и остаток; хотя бы один знак уходит в голову.
func cut(s string, n int) (head, rest string) {
	used := 0
	for i, r := range s {
		width := lipgloss.Width(string(r))
		if used+width > n && i > 0 {
			return s[:i], s[i:]
		}
		used += width
	}
	return s, ""
}

// fit обрезает строку до n колонок и добивает пробелами.
func fit(s string, n int) string {
	s = truncate(s, n)
	return s + strings.Repeat(" ", max(n-lipgloss.Width(s), 0))
}

// wrap переносит текст по словам в строки шириной до w; w <= 0 - без переноса.
func wrap(text string, w int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = clean(line)
		if w <= 0 {
			out = append(out, line)
			continue
		}
		out = append(out, wrapLine(line, w)...)
	}
	return out
}

func wrapLine(line string, w int) []string {
	var out []string
	cur := ""
	for _, word := range strings.Fields(line) {
		for lipgloss.Width(word) > w {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			var head string
			head, word = cut(word, w)
			out = append(out, head)
		}
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	return append(out, cur)
}

// ruleLine рисует приглушённую линию из дефисов.
func ruleLine(n int) string {
	return dimStyle.Render(strings.Repeat("-", max(n, 0)))
}

// joinEnds ставит left слева и right справа в строке шириной w; не влезло - только left.
func joinEnds(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if w <= 0 || right == "" {
		return left
	}
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
