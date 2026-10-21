package assets_test

import (
	"strings"
	"testing"

	"github.com/daniil4545/tix/internal/assets"
)

// RULES.md читает агент: в нём цикл работы, личность и шаблон тикета (план З2, раздел 6).
func TestRules_HasCycleIdentityAndTicketTemplate(t *testing.T) {
	for _, want := range []string{"tix take", "tix done", "--as", "## Details"} {
		if !strings.Contains(assets.Rules, want) {
			t.Errorf("в RULES.md нет %q", want)
		}
	}
}
