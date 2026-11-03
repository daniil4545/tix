package task_test

import "testing"

func TestMatches_TitleAbstractCriteriaOnly(t *testing.T) {
	tk := bodyTask("## Abstract\nFind the Needle here\n\n## Details\n- [ ] crit Beacon\n\n## Notes\nsecret\n")
	tk.Title = "Some Title"
	for needle, want := range map[string]bool{"title": true, "needle": true, "beacon": true, "secret": false, "zzz": false} {
		if got := tk.Matches(needle); got != want {
			t.Errorf("Matches(%q) = %v, want %v", needle, got, want)
		}
	}
	if line, ok := tk.MatchLine("needle"); !ok || line != "Find the Needle here" {
		t.Errorf("MatchLine = %q, %v", line, ok)
	}
}
