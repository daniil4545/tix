// Package assets хранит тексты, встроенные в бинарь.
package assets

import _ "embed"

// Rules - RULES.md: правила работы агента с трекером.
//
//go:embed RULES.md
var Rules string
