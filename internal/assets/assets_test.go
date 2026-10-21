package assets_test

import (
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
