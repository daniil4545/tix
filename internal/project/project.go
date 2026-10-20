// Package project определяет проект по рабочей папке. Без git.
package project

import "tracker/internal/config"

// Detect возвращает имя проекта для cwd. false: cwd в root или вне git.
func Detect(cwd, root string, c *config.Config) (string, bool) {
	panic("not implemented")
}
