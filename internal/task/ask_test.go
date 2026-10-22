package task_test

import (
	"testing"

	"github.com/daniil4545/tix/internal/task"
)

// spec: T-44 scenario 1
func TestParseAsk_KindPrefix(t *testing.T) {
	cases := []struct {
		name, value, kind, text string
		known                   bool
	}{
		{"no prefix is decide", "ship it?", "decide", "ship it?", true},
		{"decide", "decide: ship it?", "decide", "ship it?", true},
		{"review", "review: look at this", "review", "look at this", true},
		{"access", "access:   need a key", "access", "need a key", true},
		{"prefix only", "review:", "review", "", true},
		{"unknown lowercase word", "foo: x", "decide", "foo: x", false},
		{"unknown prefix only", "foo:", "decide", "foo:", false},
		{"link is text", "http://host is down?", "decide", "http://host is down?", true},
		{"capitalized word is text", "Note: x", "decide", "Note: x", true},
		{"word after a space is text", "see foo: x", "decide", "see foo: x", true},
		{"prefix after a space is text", " review: x", "decide", " review: x", true},
		{"empty", "", "decide", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, text, known := task.ParseAsk(c.value)
			if kind != c.kind || text != c.text || known != c.known {
				t.Errorf("ParseAsk(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.value, kind, text, known, c.kind, c.text, c.known)
			}
		})
	}
}

func FuzzParseAsk(f *testing.F) {
	for _, s := range []string{"", "review: x", "foo:", "a:b", ":"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, value string) {
		kind, _, _ := task.ParseAsk(value)
		switch kind {
		case "decide", "review", "access":
		default:
			t.Fatalf("ParseAsk(%q): kind %q is not one of the three", value, kind)
		}
	})
}
