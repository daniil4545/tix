// Package assets holds the texts embedded in the binary.
package assets

import (
	"crypto/sha256"
	"encoding/hex"

	_ "embed"
)

// Rules - RULES.md: the rules for an agent working with the tracker.
//
//go:embed RULES.md
var Rules string

// RulesHash - sha256 of Rules. A test keeps it equal to RULES.md: when the text changes, the old
// hash moves into previousRules, so a store with an untouched old copy gets the new text.
const RulesHash = "f2eacf1430eb91434e062aab2aca854b44837b5859be5007d610330ec7be4fe5"

// previousRules - sha256 of RULES.md texts shipped by earlier versions. An empty file is not here:
// the owner may have emptied it on purpose.
var previousRules = map[string]bool{
	"c56259fbb08875c39ee29e4d93cfa3bed99a0e9427247f0e0e10670e763e7cba": true,
	"5d390bc8cb3b7642b757e75f82c5efe6de935d45db3ad3e7a1db73a6184e676f": true,
	"5086330dcf376195052547a558e048d28ac25f322e77f9fc26edb0387a3bbaee": true,
	"746f63b8c9224912cb57bf64bdc7b9552e9a27d8e06fa5927eac213eaf5c1363": true,
	"80665ac8179d1012e30ea1328750c34f36bdaeacbc7d77aa2cfcf6b4f3fb1e6c": true,
	"d2aa24fdb03d5f06044407753702400c9a499733cdf8b83ea8d85999e013e0da": true,
	"74216bc959fefe9656b5c443232418d0abe4db6eb386a32e2401068398be8160": true,
	"da3782a3c15c4b9af356f0885f693c6b2fc95ba50a4ef8313d70cd8d4c5125a3": true,
}

// OutdatedRules reports whether text is an untouched RULES.md of an earlier version.
func OutdatedRules(text []byte) bool {
	sum := sha256.Sum256(text)
	return previousRules[hex.EncodeToString(sum[:])]
}
