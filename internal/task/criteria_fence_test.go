package task_test

import (
	"reflect"
	"testing"

	"tracker/internal/task"
)

func bodyTask(body string) *task.Task { return &task.Task{Body: body} }

// Строки внутри блока кода не критерии: ни для разбора, ни для Check и Uncheck.
func TestCriteria_InsideCodeFence_Ignored(t *testing.T) {
	for _, fence := range []string{"```", "~~~"} {
		body := "## Details\n- [ ] real\n" + fence + "\n- [x] in block\n## Not heading\n" + fence + "\n- [ ] after\n"
		tk := bodyTask(body)
		got := tk.Criteria()
		want := []task.Criterion{{N: 1, Text: "real"}, {N: 2, Text: "after"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Criteria = %+v, ждали %+v", fence, got, want)
		}
		if _, err := tk.Uncheck(1, 2); err != nil || tk.Body != body {
			t.Errorf("%s: Uncheck изменил тело (err=%v): %q", fence, err, tk.Body)
		}
		if _, err := tk.Check(2); err != nil || tk.Body != "## Details\n- [ ] real\n"+fence+"\n- [x] in block\n## Not heading\n"+fence+"\n- [x] after\n" {
			t.Errorf("%s: Check(2) тронул блок кода (err=%v): %q", fence, err, tk.Body)
		}
		if rows := task.CriterionRows("- [ ] real\n" + fence + "\n- [x] in block\n" + fence + "\n- [ ] after"); !reflect.DeepEqual(rows, []int{0, 4}) {
			t.Errorf("%s: CriterionRows = %v, ждали [0 4]", fence, rows)
		}
	}
}

// Uncheck: CRLF сохраняется, одинаковые критерии различаются номером.
func TestUncheck_CRLFAndIdenticalCriteria(t *testing.T) {
	tk := bodyTask("## Details\r\n- [x] same\r\n- [x] same\r\n")
	changed, err := tk.Uncheck(2)
	if err != nil || !changed {
		t.Fatalf("Uncheck(2) = %v, %v", changed, err)
	}
	if want := "## Details\r\n- [x] same\r\n- [ ] same\r\n"; tk.Body != want {
		t.Errorf("тело = %q, ждали %q", tk.Body, want)
	}
	if changed, err := tk.Uncheck(2); err != nil || changed {
		t.Errorf("повторное Uncheck(2) = %v, %v; ждали false, nil", changed, err)
	}
	if _, err := tk.Uncheck(3); err == nil {
		t.Error("Uncheck(3) без третьего критерия не вернул ошибку")
	}
}
