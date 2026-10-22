package task

import "regexp"

// AskKinds - the kinds of an agent ask; the first is the default for a value without a prefix.
var AskKinds = []string{"decide", "review", "access"}

var (
	askKindRe   = regexp.MustCompile(`^(decide|review|access):\s*`)
	askPrefixRe = regexp.MustCompile(`^[a-z]+:`)
)

// ParseAsk splits an ask value into its kind and text. No prefix means decide. known is false
// for an unknown lowercase prefix (foo: x): the kind is decide and the text is the whole value.
func ParseAsk(value string) (kind, text string, known bool) {
	if m := askKindRe.FindStringSubmatch(value); m != nil {
		return m[1], value[len(m[0]):], true
	}
	return AskKinds[0], value, !askPrefixRe.MatchString(value)
}
