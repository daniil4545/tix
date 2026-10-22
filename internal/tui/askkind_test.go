package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/task"
)

// askRow returns the board row of task id.
func askRow(t *testing.T, m *Model, id string) (live bool, ask string) {
	t.Helper()
	tk := m.byID(id)
	if tk == nil {
		t.Fatalf("task %s is not on the board", id)
	}
	rows := m.rows([]*task.Task{tk})
	return rows[0].Live, rows[0].Ask
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

	if isLive, ask := askRow(t, m, live); !isLive || ask != "review: look" {
		t.Errorf("live claim: live=%v ask=%q, want live=true and the raw value", isLive, ask)
	}
	if isLive, _ := askRow(t, m, none); isLive {
		t.Errorf("no claim: live=true, want false")
	}
	m.now = func() time.Time { return time.Now().Add(10000 * time.Hour) }
	if isLive, _ := askRow(t, m, live); isLive {
		t.Errorf("expired claim (model clock): live=true, want false")
	}
}

// spec: T-85 overdue: a due date before the model-clock day marks the due part of the meta
func TestBoardRow_Overdue_ByModelClock(t *testing.T) {
	e := mvNewEnv(t, "")
	id := e.create("job", "doing")
	e.set(id, "due", "2026-10-20")
	m := newMouseModel(t, e, 120, 30)
	caReload(m)
	overdue := func() string {
		return m.rows([]*task.Task{m.byID(id)})[0].Overdue
	}
	day := func(s string) func() time.Time {
		return func() time.Time {
			d, _ := time.ParseInLocation(time.DateOnly, s, time.Local)
			return d.Add(15 * time.Hour)
		}
	}
	cases := []struct{ today, want string }{
		{"2026-10-19", ""},
		{"2026-10-20", ""},
		{"2026-10-21", "due 20.10"},
	}
	for _, c := range cases {
		m.now = day(c.today)
		if got := overdue(); got != c.want {
			t.Errorf("today %s: Overdue %q, want %q", c.today, got, c.want)
		}
	}
	m.now = day("2026-10-21")
	if meta := m.rows([]*task.Task{m.byID(id)})[0].Meta; !strings.Contains(meta, "due 20.10") {
		t.Errorf("meta %q lost the due text", meta)
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
