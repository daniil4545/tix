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
const RulesHash = "881246329b9b8860a18ba70f4b8f9877ea4f3e7a7ea9d1205e6eebd6ffbc26bc"

// previousRules - sha256 of RULES.md texts shipped by earlier versions. An empty file is not here:
// the owner may have emptied it on purpose.
var previousRules = map[string]bool{
	"9d2f6458ab389c8f807f879e47064bad238c69452b189ef546b1638c9d3dafb9": true,
	"9a5ce4f9e4fc1771215cf99ba320a7e3e59ae8bfbc4b6e8a3dec18228b595fa7": true,
	"cb7e389dc503145e61ac25498d04bca352144c26ddc9fd44b9c60393412f659e": true,
	"3f710fc22c5bdb883e9871a7769452405910cc4748ed1899ff10632e02202830": true,
	"00db067ef71c77de3f648f26133c99288670af80c94fabca5f229153e14dae6e": true,
	"2208f3ba49377f35e6bcce8bbc45e85e8aa2880d472dd1010232013ccea5a9fc": true,
	"36471ac63225f418df33f54aee88f3bee64a0a9fdd5d5e7ddb39be622d1c1f3f": true,
	"8de482088d3042c4c2c82aebf257f97d7c451d3b653c6a2e1788a3a057eb2930": true,
	"72d8940a0c3e61d913e0de4b907282780335e81a88131151a111bbf6b2e74a26": true,
	"0d3303412cf80eb27db48634e211bc744ea52528600fb198ddc5088b748db05a": true,
	"f1e0f5e1aaef7404cead2d39c926c422a71a3e0d1e75396a92a34f8b36bd5f34": true,
	"b3556ac040a632d6a9a1e2e2e55c41bc7b8781dd1ddf320843a8f7feae85e29a": true,
	"2c1238b48f9fa4ea82de5d32c5237eeb08eaa773000baec6aec2fd8583508900": true,
	"5f96e07d234feeeaf200831331b3d2cb2f18f01bc88c3b7d7f7b0cdb200228e8": true,
	"c56259fbb08875c39ee29e4d93cfa3bed99a0e9427247f0e0e10670e763e7cba": true,
	"5d390bc8cb3b7642b757e75f82c5efe6de935d45db3ad3e7a1db73a6184e676f": true,
	"5086330dcf376195052547a558e048d28ac25f322e77f9fc26edb0387a3bbaee": true,
	"746f63b8c9224912cb57bf64bdc7b9552e9a27d8e06fa5927eac213eaf5c1363": true,
	"80665ac8179d1012e30ea1328750c34f36bdaeacbc7d77aa2cfcf6b4f3fb1e6c": true,
	"d2aa24fdb03d5f06044407753702400c9a499733cdf8b83ea8d85999e013e0da": true,
	"74216bc959fefe9656b5c443232418d0abe4db6eb386a32e2401068398be8160": true,
	"da3782a3c15c4b9af356f0885f693c6b2fc95ba50a4ef8313d70cd8d4c5125a3": true,
	"f2eacf1430eb91434e062aab2aca854b44837b5859be5007d610330ec7be4fe5": true,
	"39c1d03b2c59e2252fd06cf35eef614695d327b3a38194f289f6d2e0e2c1f2b6": true,
	"3485ae0aec3c6e9354614ee7eec1359ff46b48e136ed83df836ae0f17ec06172": true,
	"da5d3b95d377ea677c5894b54357d984d082339bb3f6407a3df0541022277134": true,
	"742f6f37750602970f8ac8eb6b3b064d0dbc74616da808c5669fdd56bd23799b": true,
}

// OutdatedRules reports whether text is an untouched RULES.md of an earlier version.
func OutdatedRules(text []byte) bool {
	sum := sha256.Sum256(text)
	return previousRules[hex.EncodeToString(sum[:])]
}
