package task

// Criterion - критерий готовности из раздела "## Details": номер с 1, текст, отметка.
type Criterion struct {
	N    int
	Text string
	Done bool
}

// Criteria возвращает критерии раздела "## Details".
func (t *Task) Criteria() []Criterion {
	return nil
}

// Check отмечает критерии с номерами nums. changed=false: все уже отмечены.
func (t *Task) Check(nums ...int) (changed bool, err error) {
	return false, errNotImplemented
}
