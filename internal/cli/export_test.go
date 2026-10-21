package cli

import "time"

// SetHookReadTimeout shortens the hook stdin read timeout; returns a restore func.
func SetHookReadTimeout(d time.Duration) func() {
	old := hookReadTimeout
	hookReadTimeout = d
	return func() { hookReadTimeout = old }
}
