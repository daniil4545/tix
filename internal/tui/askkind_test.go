package tui

import (
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

// askRow returns the board row of task id.
func askRow(t *testing.T, m *Model, id string) (ended bool, ask string) {
	t.Helper()
	tk := m.byID(id)
	if tk == nil {
		t.Fatalf("task %s is not on the board", id)
	}
	rows := m.rows([]*task.Task{tk})
	return rows[0].AskEnded, rows[0].Ask
}

// spec: T-44 scenario 3 (live claim decides the star state, by the model clock)
func TestBoardRow_AskSession_LiveOrEnded(t *testing.T) {
	e := mvNewEnv(t, "")
	live := e.create("live", "doing")
	e.set(live, "ask", "review: look")
	if _, err := e.store.Take(live, core.Take, "claude:abcd1234"); err != nil {
		t.Fatal(err)
	}
	none := e.create("none", "doing")
	e.set(none, "ask", "access: key")
	m := newMouseModel(t, e, 120, 30)
	caReload(m)

	if ended, ask := askRow(t, m, live); ended || ask != "review: look" {
		t.Errorf("live claim: ended=%v ask=%q, want ended=false and the raw value", ended, ask)
	}
	if ended, _ := askRow(t, m, none); !ended {
		t.Errorf("no claim: ended=false, want true")
	}
	m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
	if ended, _ := askRow(t, m, live); !ended {
		t.Errorf("expired claim (model clock): ended=false, want true")
	}
}

// spec: T-44 scenario 4
func TestFocus_G_AskWithoutLiveClaim_SessionEnded(t *testing.T) {
	cases := []struct {
		name   string
		take   bool
		expire bool
	}{
		{"no claim", false, false},
		{"expired claim", true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := mvNewEnv(t, "")
			id := e.create("job", "doing")
			e.set(id, "ask", "which db?")
			if c.take {
				if _, err := e.store.Take(id, core.Take, "claude:abcd1234"); err != nil {
					t.Fatal(err)
				}
			}
			m := newMouseModel(t, e, 120, 30)
			caReload(m)
			m.selectTask(id)
			var opened []string
			m.acts.open = func(u string) error { opened = append(opened, u); return nil }
			if c.expire {
				m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
			}
			typeKeys(m, "g")
			want := id + ": session ended; the answer stays in the task"
			if got := caStatus(m); got != want || len(opened) != 0 {
				t.Errorf("status %q, open %v, want %q and no open", got, opened, want)
			}
		})
	}
}

func TestFocus_G_NoAskNoClaim_KeepsNoSessionStatus(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "doing")
	m := newMouseModel(t, e, 120, 30)
	m.selectTask(id)
	typeKeys(m, "g")
	if got := caStatus(m); got != id+": no session to open" {
		t.Errorf("status %q, want the old no-session text", got)
	}
}
