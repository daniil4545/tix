// Package actor - личность автора записи: разбор источников и проверка имени.
package actor

import "errors"

var errNotImplemented = errors.New("not implemented")

// Resolve определяет личность по порядку 2.4 спеки: флаг, окружение агента,
// TRACKER_ACTOR. Формы агента: "claude:<id>", "codex:<id>". known=false: личность не найдена.
func Resolve(flag string, getenv func(string) string) (name string, known bool) {
	return "", false
}

// Valid проверяет введённое имя (`--as`, `TRACKER_ACTOR`): без пробелов, `|`, `-->`
// и переводов строки. `unknown` допустим.
func Valid(name string) error {
	return errNotImplemented
}

// Short сокращает имя до "kind:" и 8 знаков id.
func Short(name string) string {
	return ""
}
