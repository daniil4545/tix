package cli_test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// keysOf returns the sorted JSON keys of an object.
func keysOf(t *testing.T, raw []byte) (map[string]json.RawMessage, string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return m, strings.Join(keys, " ")
}

// The JSON shape of show and list is a contract of the web UI: renaming a field must fail here.
func TestCLI_JSONShape_ShowAndList(t *testing.T) {
	defer guard(t)
	b := newBox(t)
	b.ok("", "new", "Epic")
	b.ok("", "new", "Child", "--parent", "T-1")
	for i := 1; i <= 7; i++ {
		b.ok("", "note", "T-1", fmt.Sprintf("step %d", i), "--as", "cli:1")
	}
	b.ok("", "note", "T-2", "child note\nsecond line", "--as", "cli:2")

	// Expected key sets, written sorted.
	const parentKeys = "body children children_closed claim comments created external id kind notes path project rank rev status tags title waiting"
	const childKeys = "body claim comments created external id kind notes parent path project rank rev status tags title waiting"
	m, keys := keysOf(t, []byte(b.ok("", "show", "T-1", "--json").out))
	ensure(t, keys == parentKeys, "show parent keys:\n%s", keys)
	var notes []map[string]string
	ensure(t, json.Unmarshal(m["notes"], &notes) == nil, "notes: %s", m["notes"])
	ensure(t, len(notes) == 5 && notes[4]["text"] == "step 7" && notes[4]["actor"] == "cli:1" && notes[4]["at"] != "",
		"default notes are the last 5, old to new: %s", m["notes"])
	ensure(t, string(m["kind"]) == `"idea"` && string(m["children"]) == "1" && string(m["children_closed"]) == "0",
		"kind or counts: %s %s %s", m["kind"], m["children"], m["children_closed"])
	_ = json.Unmarshal([]byte(b.ok("", "show", "T-1", "--json", "-v").out), &m)
	_ = json.Unmarshal(m["notes"], &notes)
	ensure(t, len(notes) == 7, "-v gives all notes, got %d", len(notes))

	m, keys = keysOf(t, []byte(b.ok("", "show", "T-2", "--json").out))
	ensure(t, keys == childKeys, "show child keys:\n%s", keys)
	ensure(t, string(m["parent"]) == `"T-1"`, "parent: %s", m["parent"])

	b.ok("", "new", "Plain")
	var items []json.RawMessage
	ensure(t, json.Unmarshal([]byte(b.ok("", "list", "--json").out), &items) == nil && len(items) == 3, "list items")
	want := map[string]string{
		"T-1": "children children_closed claim created external id kind last_note path project rank rev status tags title waiting",
		"T-2": "claim created external id kind last_note parent path project rank rev status tags title waiting",
		"T-3": "claim created external id kind path project rank rev status tags title waiting",
	}
	for _, raw := range items {
		m, keys := keysOf(t, raw)
		var id string
		_ = json.Unmarshal(m["id"], &id)
		ensure(t, keys == want[id], "list keys of %s:\n%s", id, keys)
		if id == "T-2" {
			var ln map[string]string
			_ = json.Unmarshal(m["last_note"], &ln)
			ensure(t, len(ln) == 2 && ln["at"] != "" && strings.HasPrefix(ln["text"], "child note"), "last_note: %s", m["last_note"])
		}
		if id == "T-1" {
			ensure(t, strings.Contains(string(m["last_note"]), "step 7"), "last_note of T-1: %s", m["last_note"])
		}
	}
}
