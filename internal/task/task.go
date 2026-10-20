// Package task разбирает и печатает файл задачи. Без IO.
package task

import (
	"time"

	"gopkg.in/yaml.v3"
)

// Зона A заменяет заглушку настоящим разбором frontmatter через yaml.Node.
var _ yaml.Node

// Comment - один комментарий после заголовка "## Комментарии".
type Comment struct {
	At    time.Time `json:"at"`
	Actor string    `json:"actor"`
	Text  string    `json:"text"`
}

// Task - задача целиком. JSON-имена: поля frontmatter, rev, body, comments.
type Task struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project"`
	Status   string    `json:"status"`
	External string    `json:"external"`
	Waiting  string    `json:"waiting"`
	Claim    string    `json:"claim"`
	Due      string    `json:"due"`
	Ask      string    `json:"ask"`
	Rank     float64   `json:"rank"`
	Created  time.Time `json:"created"`
	Tags     []string  `json:"tags"`
	Body     string    `json:"body"` // до "## Комментарии"
	Comments []Comment `json:"comments"`
	Rev      string    `json:"rev"` // blob id из Go, не из файла
	Path     string    `json:"path"`
}

// Parse разбирает файл задачи. Ошибка несёт путь и строку.
func Parse(path string, data []byte) (*Task, error) {
	panic("not implemented")
}

// Render печатает файл задачи: frontmatter, тело, комментарии.
func (t *Task) Render() []byte {
	panic("not implemented")
}

// SetBody заменяет тело. "## Комментарии" в теле - ошибка.
func (t *Task) SetBody(body string) error {
	panic("not implemented")
}

// AddComment дописывает комментарий с маркером времени и автора.
func (t *Task) AddComment(at time.Time, actor, text string) {
	panic("not implemented")
}

// Summary возвращает первую строку сводки для list.
func (t *Task) Summary() string {
	panic("not implemented")
}

// FileName строит имя файла T-N-слаг.md из id и названия.
func FileName(id, title string) string {
	panic("not implemented")
}
