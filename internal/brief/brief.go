// Package brief собирает вывод `tk ctx`. Без IO.
package brief

import (
	"time"

	"tracker/internal/config"
	"tracker/internal/task"
)

// Input - вход Build. PerColumn - сколько задач на колонку до сжатия, Limit - лимит рун текста.
type Input struct {
	Tasks                     []*task.Task
	Cfg                       *config.Config
	Project, RulesPath, Actor string
	Now                       time.Time
	PerColumn, Limit          int
}

// Row - строка задачи. Detail: summary, "Waiting: ...", "k/n"; свой claim - "@you".
type Row struct{ ID, Title, Detail, Claim string }

// Section - колонка с ролью active, waiting или ready; More - сколько задач скрыто.
type Section struct {
	Column string
	Rows   []Row
	More   int
}

// Brief - собранный ctx. В JSON имена полей в нижнем регистре, лимит действует только на текст.
type Brief struct {
	Project, Rules string
	Sections       []Section
	Next           string
}

// Build собирает Brief: роли active, waiting, ready; сжатие до Limit.
func Build(in Input) Brief {
	return Brief{}
}

// Text печатает Brief строками `tk ctx`.
func (b Brief) Text() string {
	return ""
}
