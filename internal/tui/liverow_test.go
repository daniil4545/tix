package tui

import (
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

// liveRow returns the board row of task id.
func liveRow(t *testing.T, m *Model, id string) bool {
	t.Helper()
	tk := m.byID(id)
	if tk == nil {
		t.Fatalf("task %s is not on the board", id)
	}
	rows := m.rows([]*task.Task{tk})
	return rows[0].Live
}

// A live claim decides the card state, by the model clock.
func TestBoardRow_Live_ByClaimAndModelClock(t *testing.T) {
	e := mvNewEnv(t, "")
	live := e.create("live", "in_progress")
	if _, err := e.store.Take(live, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	none := e.create("none", "in_progress")
	m := newMouseModel(t, e, 120, 30)
	caReload(m)

	if isLive := liveRow(t, m, live); !isLive {
		t.Errorf("live claim: live=%v, want true", isLive)
	}
	if isLive := liveRow(t, m, none); isLive {
		t.Errorf("no claim: live=true, want false")
	}
	m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
	if isLive := liveRow(t, m, live); isLive {
		t.Errorf("expired claim (model clock): live=true, want false")
	}
}
