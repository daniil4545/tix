package tui

import (
	"strings"
	"testing"
)

// New task in a project outside the catalog: the status line shows the refusal, no task, no panic.
func TestNewTask_UnknownProject_ShowsRefusal(t *testing.T) {
	acStubEditor(t, "")
	e := mvNewEnv(t, "[paths]\ntarn = \"\"\n")
	e.createIn("Alpha", "tarn", "idea")
	e.acStartIn("ghost", 140, 30)
	e.key("n")
	e.screenHas("title:")
	e.tm.Type("Nope")
	e.key("enter")
	e.until("refusal shown", func() bool { return e.acByTitle("Nope") == nil && strings.Contains(e.text(), "unknown project") })
	if e.acByTitle("Nope") != nil {
		t.Error("task created in an unknown project")
	}
}
