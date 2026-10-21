package assets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/assets"
)

// RULES.md is read by the agent: it holds the work cycle, identity and the task template (agent cycle, section 6).
func TestRules_HasCycleIdentityAndTicketTemplate(t *testing.T) {
	for _, want := range []string{"tix take", "tix done", "--as", "## Details"} {
		if !strings.Contains(assets.Rules, want) {
			t.Errorf("RULES.md lacks %q", want)
		}
	}
}

// RulesHash follows RULES.md. A failure means the text changed: move the old RulesHash into
// previousRules, then set RulesHash to the new sum.
func TestRulesHash_MatchesRulesText(t *testing.T) {
	sum := sha256.Sum256([]byte(assets.Rules))
	if got := hex.EncodeToString(sum[:]); got != assets.RulesHash {
		t.Fatalf("RULES.md sha256 is %s, RulesHash is %s: move the old hash into previousRules and update RulesHash", got, assets.RulesHash)
	}
	if assets.OutdatedRules([]byte(assets.Rules)) {
		t.Error("the current RULES.md is listed as outdated")
	}
	if assets.OutdatedRules([]byte(assets.Rules+"\nmy own line\n")) || assets.OutdatedRules(nil) {
		t.Error("an edited or empty RULES.md is listed as outdated")
	}
}
