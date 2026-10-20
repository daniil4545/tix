package task

import (
	"strings"
	"time"
)

// claimLayout - срок захвата в файле: локальное время до минут.
const claimLayout = "2006-01-02T15:04"

// claimSep отделяет актора от срока в поле claim.
const claimSep = " until "

// Claim - захват задачи: кто и до какого времени.
type Claim struct {
	Actor string
	Until time.Time
}

// ClaimInfo разбирает поле claim "<actor> until 2006-01-02T15:04". Пусто или битое: false.
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

// SetClaim пишет поле claim.
func (t *Task) SetClaim(c Claim) {
	t.Claim = c.Actor + claimSep + c.Until.In(time.Local).Format(claimLayout)
}

// ClearClaim очищает поле claim.
func (t *Task) ClearClaim() {
	t.Claim = ""
}
