// Package actor - личность автора записи: разбор источников и проверка имени.
package actor

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// Unknown - имя записи, когда личность не найдена.
const Unknown = "unknown"

// shortID - сколько знаков id остаётся в Short.
const shortID = 8

// Resolve определяет личность по порядку 2.4 спеки: флаг, окружение агента,
// TRACKER_ACTOR. Формы агента: "claude:<id>", "codex:<id>". known=false: личность
// не найдена, name равен "unknown".
func Resolve(flag string, getenv func(string) string) (name string, known bool) {
	switch {
	case flag != "":
		name = flag
	case getenv("CLAUDE_CODE_SESSION_ID") != "":
		name = "claude:" + getenv("CLAUDE_CODE_SESSION_ID")
	case getenv("CODEX_THREAD_ID") != "":
		name = "codex:" + getenv("CODEX_THREAD_ID")
	default:
		name = getenv("TRACKER_ACTOR")
	}
	if name == "" || name == Unknown {
		return Unknown, false
	}
	return name, true
}

// Valid проверяет введённое имя (`--as`, `TRACKER_ACTOR`): без пробелов, `|`, `-->`
// и переводов строки. `unknown` допустим.
func Valid(name string) error {
	if name == "" {
		return errors.New("actor name is empty")
	}
	if strings.Contains(name, "|") || strings.Contains(name, "-->") {
		return errors.New("actor name must not contain | or -->")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("actor name must not contain spaces or line breaks")
		}
	}
	return nil
}

// maxModel - предел длины имени модели.
const maxModel = 100

// modelRe - допустимые знаки имени модели. Значение уходит в `export` для shell и в
// трейлер коммита: кавычки, пробелы, точка с запятой, $, обратный апостроф и переводы строк не проходят.
var modelRe = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]-]+$`)

// ValidModel проверяет имя модели для трейлера Model и для CLAUDE_ENV_FILE.
func ValidModel(name string) bool {
	return len(name) <= maxModel && modelRe.MatchString(name)
}

// Short сокращает имя до "kind:" и 8 знаков id. У id с префиксом "ses_" (OpenCode) префикс
// отбрасывается и берутся последние 8 знаков: начало id у сессий почти совпадает.
func Short(name string) string {
	kind, id, ok := strings.Cut(name, ":")
	if !ok {
		return name
	}
	r := []rune(id)
	if tail, found := strings.CutPrefix(id, "ses_"); found {
		r = []rune(tail)
		return kind + ":" + string(r[max(len(r)-shortID, 0):])
	}
	if len(r) > shortID {
		r = r[:shortID]
	}
	return kind + ":" + string(r)
}
