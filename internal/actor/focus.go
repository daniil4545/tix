package actor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// focusURLRe - the only allowed form of a Warp tab address.
var focusURLRe = regexp.MustCompile(`^warp://session/[0-9a-f]{32}$`)

// maxFocusName - the limit on focus file name length.
const maxFocusName = 120

// ValidFocusURL: a terminal tab address fits writing to a focus file and `open`.
func ValidFocusURL(s string) bool { return focusURLRe.MatchString(s) }

// FocusFile - the agent focus file name in the focus directory: "claude:8f3a" - "claude-8f3a".
// Empty for a human and an unknown identity: they have no session to jump to.
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
	// A name made of dots must not point to a directory.
	return strings.TrimLeft(out, ".")
}

// FocusDir - the directory of focus files: machine data, not the store. TIX_FOCUS_DIR overrides the path
// (tests), otherwise os.UserCacheDir()/tix/focus. Empty if the cache directory is unknown.
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
