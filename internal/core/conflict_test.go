package core_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniil4545/tix/internal/core"
)

func TestStore_Edit_ConflictCarriesMergedAndRetryResolves(t *testing.T) {
	defer guard(t)
	e := newEnv(t)
	s := e.open(2 * time.Second)
	mustCreate(t, s, "Alpha")
	_, rev0, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", rev0, "line1\nline2\nline3\n", "a"); err != nil {
		t.Fatal(err)
	}
	_, base, _ := s.Get("T-1")
	if _, err := s.Edit("T-1", base, "line1\nAGENT\nline3\n", "agent"); err != nil {
		t.Fatal(err)
	}
	n := e.count()
	before, _ := os.ReadFile(e.file("T-1"))

	_, err := s.Edit("T-1", base, "line1\nUSER\nline3\n", "human")
	var ce *core.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("Edit по старой базе: %v, ждали *core.ConflictError", err)
	}
	if !errors.Is(err, core.ErrConflict) || !strings.Contains(err.Error(), "body changed after tix show") {
		t.Errorf("ошибка %q: ждали errors.Is ErrConflict и прежний текст для кода 3", err)
	}
	for _, want := range []string{"<<<<<<<", "=======", ">>>>>>>", "AGENT", "USER", "line1", "line3"} {
		if !strings.Contains(ce.Merged, want) {
			t.Errorf("Merged без %q:\n%s", want, ce.Merged)
		}
	}
	after, _ := os.ReadFile(e.file("T-1"))
	if string(before) != string(after) || e.count() != n {
		t.Error("конфликт изменил файл или историю")
	}

	// Повтор: пользователь снял маркеры в Merged, база - ce.Rev. Новый конфликт недопустим.
	if _, err := s.Edit("T-1", ce.Rev, "line1\nBOTH\nline3\n", "human"); err != nil {
		t.Fatalf("повтор Edit с ConflictError.Rev: %v", err)
	}
	got, _ := os.ReadFile(e.file("T-1"))
	if !strings.Contains(string(got), "BOTH") || strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("после повтора:\n%s", got)
	}
}
