// Package view рисует доску и карточку строкой; без IO и без состояния.
package view

import (
	_ "charm.land/lipgloss/v2" // стили зоны V; импорт держит зависимость в go.mod

	"tracker/internal/history"
)

// Row - строка колонки доски.
type Row struct {
	ID, Project, Title, Summary, Meta string
	Star                              bool
}

// Col - колонка доски.
type Col struct {
	Name string
	Rows []Row
}

// Board - всё, что нужно для кадра доски.
type Board struct {
	Project  string
	Total    int
	Cols     []Col
	Col, Row int
	W, H     int
	Status   string
	IsErr    bool
}

// Card - всё, что нужно для кадра карточки.
type Card struct {
	ID, Title, Column, Fields, Ask, Abstract, Summary, Details, Checks string
	History                                                            []history.Entry
	Cursor, Scroll, W, H                                               int
	Status                                                             string
	IsErr                                                              bool
}

// RenderBoard рисует доску. Заглушка зоны 0.
func RenderBoard(b Board) string { return "" }

// RenderCard рисует карточку. Заглушка зоны 0.
func RenderCard(c Card) string { return "" }

// RenderInput рисует строку ввода. Заглушка зоны 0.
func RenderInput(prompt, text string, w int) string { return "" }
