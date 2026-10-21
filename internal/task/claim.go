package task

import (
	"strings"
	"time"
)

// claimLayout - the claim deadline in the file: local time down to minutes.
const claimLayout = "2006-01-02T15:04"

// claimSep separates the actor from the deadline in the claim field.
const claimSep = " until "

// Claim - a task claim: who and until when.
type Claim struct {
	Actor string
	Until time.Time
}

// ClaimInfo parses the claim field "<actor> until 2006-01-02T15:04". Empty or broken: false.
func (t *Task) ClaimInfo() (Claim, bool) {
	i := strings.LastIndex(t.Claim, claimSep)
	if i <= 0 {
		return Claim{}, false
	}
	until, err := time.ParseInLocation(claimLayout, t.Claim[i+len(claimSep):], time.Local)
	if err != nil {
		return Claim{}, false
	}
	return Claim{Actor: t.Claim[:i], Until: until}, true
}

// SetClaim writes the claim field.
func (t *Task) SetClaim(c Claim) {
	t.Claim = c.Actor + claimSep + c.Until.In(time.Local).Format(claimLayout)
}

// ClearClaim clears the claim field.
func (t *Task) ClearClaim() {
	t.Claim = ""
}
