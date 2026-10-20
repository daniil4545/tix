package task

import (
	"errors"
	"time"
)

var errNotImplemented = errors.New("not implemented")

// Claim - захват задачи: кто и до какого времени.
type Claim struct {
	Actor string
	Until time.Time
}

// ClaimInfo разбирает поле claim. Пусто или битое: false.
func (t *Task) ClaimInfo() (Claim, bool) {
	return Claim{}, false
}

// SetClaim пишет поле claim.
func (t *Task) SetClaim(c Claim) {}

// ClearClaim очищает поле claim.
func (t *Task) ClearClaim() {}
