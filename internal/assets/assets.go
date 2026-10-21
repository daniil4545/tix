// Package assets holds the texts embedded in the binary.
package assets

import _ "embed"

// Rules - RULES.md: the rules for an agent working with the tracker.
//
//go:embed RULES.md
var Rules string
