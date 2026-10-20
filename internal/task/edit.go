package task

// SetField меняет поле frontmatter: title, waiting, due, tags, ask, external.
func (t *Task) SetField(name, value string) error {
	return errNotImplemented
}

// SetSection заменяет текст раздела (Abstract, Summary). "## " внутри текста - ошибка.
func (t *Task) SetSection(name, text string) error {
	return errNotImplemented
}
