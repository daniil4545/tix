// Package actor - identity of a record author: source parsing and name validation.
package actor

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Unknown - the record name when no identity is found.
const Unknown = "unknown"

// shortID - how many id characters Short keeps.
const shortID = 8

// ErrAmbiguous - both agent variables are set: one agent runs inside another, and the environment
// does not tell which one calls.
var ErrAmbiguous = errors.New("both CLAUDE_CODE_SESSION_ID and CODEX_THREAD_ID are set")

// Resolve determines the identity in the order of spec 2.4: flag, agent environment,
// TIX_ACTOR. Agent forms: "claude:<id>", "codex:<id>". known=false: no identity
// found, name equals "unknown". Both agent variables without a flag: ErrAmbiguous with the --as hint.
func Resolve(flag string, getenv func(string) string) (name string, known bool, err error) {
	claude, codex := getenv("CLAUDE_CODE_SESSION_ID"), getenv("CODEX_THREAD_ID")
	switch {
	case flag != "":
		name = flag
	case claude != "" && codex != "":
		return Unknown, false, fmt.Errorf("%w; pass --as codex:%s inside Codex or --as claude:%s inside Claude Code",
			ErrAmbiguous, codex, claude)
	case claude != "":
		name = "claude:" + claude
	case codex != "":
		name = "codex:" + codex
	default:
		name = getenv("TIX_ACTOR")
	}
	if name == "" || name == Unknown {
		return Unknown, false, nil
	}
	return name, true, nil
}

// Valid checks an entered name (`--as`, `TIX_ACTOR`): no spaces, `|`, `-->`
// or line breaks. `unknown` is allowed.
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

// maxModel - the limit on model name length.
const maxModel = 100

// modelRe - allowed characters of a model name. The value goes to `export` for the shell and to
// the commit trailer: quotes, spaces, semicolons, $, backticks and line breaks do not pass.
var modelRe = regexp.MustCompile(`^[A-Za-z0-9._:/\[\]-]+$`)

// ValidModel checks the model name for the Model trailer and for CLAUDE_ENV_FILE.
func ValidModel(name string) bool {
	return len(name) <= maxModel && modelRe.MatchString(name)
}

// Short shortens the name to "kind:" and 8 id characters. For an id with the "ses_" prefix (OpenCode) the prefix
// is dropped and the last 8 characters are taken: the starts of session ids almost coincide.
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
