package cli_test

import (
	"strings"
	"testing"
)

// spec: T-44 scenario 2
func TestCLI_SetAsk_UnknownKindRefused(t *testing.T) {
	defer guard(t)
	for _, ask := range []string{"foo: x", "note:", " foo: x"} {
		t.Run(ask, func(t *testing.T) {
			b := newBox(t)
			b.ok("", "new", "Task")
			before := b.snapshot()
			msg := b.rule("set", "T-1", "ask="+ask)
			for _, kind := range []string{"decide", "review", "access"} {
				ensure(t, strings.Contains(msg, kind), "refusal %q does not list kind %q", msg, kind)
			}
			ensure(t, b.snapshot() == before, "the refused set changed the store")
		})
	}
}

// spec: T-44 scenario 2
func TestCLI_SetAsk_KnownKindOrPlainText_StoredAsIs(t *testing.T) {
	defer guard(t)
	for _, ask := range []string{"review: look", "access: need a key", "decide: which db?", "plain question", "Note: x", "see foo: x", "http://host/path down?", "a:b"} {
		t.Run(ask, func(t *testing.T) {
			b := newBox(t)
			b.ok("", "new", "Task")
			b.ok("", "set", "T-1", "ask="+ask)
			r := b.ok("", "show", "T-1", "--json")
			ensure(t, strings.Contains(r.out, `"ask": "`+ask+`"`) || strings.Contains(r.out, `"ask":"`+ask+`"`),
				"stored ask differs from %q:\n%s", ask, r.out)
		})
	}
}
