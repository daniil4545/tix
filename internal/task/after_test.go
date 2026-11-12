package task_test

import (
	"reflect"
	"testing"

	"github.com/daniil4545/tix/internal/config"
	"github.com/daniil4545/tix/internal/task"
)

func TestAfter_Lines(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string
	}{
		"list":      {"text\nAfter: T-1, T-2\n", []string{"T-1", "T-2"}},
		"case":      {"AFTER: T-3\nafter:T-4 T-5", []string{"T-3", "T-4", "T-5"}},
		"repeat":    {"After: T-1, T-1", []string{"T-1"}},
		"no ids":    {"After:\nAfter: soon, T-\nAfter: T-x", nil},
		"not start": {"see After: T-1\n- After: T-2", nil},
		"fenced":    {"```\nAfter: T-1\n```\nAfter: T-2", []string{"T-2"}},
		"empty":     {"", nil},
	}
	for name, c := range cases {
		got := (&task.Task{Body: c.body}).After()
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestWaitingOn_UnknownAndClosedAreDone(t *testing.T) {
	all := []*task.Task{{ID: "T-1", Status: "open"}, {ID: "T-2", Status: "closed"}}
	w := &task.Task{ID: "T-9", Body: "After: T-1, T-2, T-77"}
	if got := task.WaitingOn(w, task.NotClosed(all, config.Default())); !reflect.DeepEqual(got, []string{"T-1"}) {
		t.Errorf("got %v, want [T-1]", got)
	}
}
