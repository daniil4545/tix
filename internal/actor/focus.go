package actor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// focusURLRe - единственный допустимый вид адреса вкладки Warp (спека mvp-actions, 4a).
var focusURLRe = regexp.MustCompile(`^warp://session/[0-9a-f]{32}$`)

// maxFocusName - предел длины имени файла фокуса.
const maxFocusName = 120

// ValidFocusURL: адрес вкладки терминала годится для записи в файл фокуса и для `open`.
func ValidFocusURL(s string) bool { return focusURLRe.MatchString(s) }

// FocusFile - имя файла фокуса агента в каталоге фокуса: "claude:8f3a" - "claude-8f3a".
// Пусто для человека и неизвестной личности: сессии для перехода у них нет.
func FocusFile(name string) string {
	if name == "" || name == Unknown || name == "human" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ':':
			b.WriteByte('-')
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > maxFocusName {
		out = out[:maxFocusName]
	}
	// Имя из точек не должно указывать на каталог.
	return strings.TrimLeft(out, ".")
}

// FocusDir - каталог файлов фокуса: данные машины, не базы. TIX_FOCUS_DIR перекрывает путь
// (тесты), иначе os.UserCacheDir()/tix/focus. Пусто, если каталог кэша неизвестен.
func FocusDir(getenv func(string) string) string {
	if dir := getenv("TIX_FOCUS_DIR"); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "tix", "focus")
}
