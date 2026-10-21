package cli

import "time"

// SetHookReadTimeout сокращает срок чтения stdin хука; возвращает откат.
func SetHookReadTimeout(d time.Duration) func() {
	old := hookReadTimeout
	hookReadTimeout = d
	return func() { hookReadTimeout = old }
}
