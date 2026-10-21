package core

// ConflictError - конфликт слияния тела при Edit. Rev - блоб текущего файла: новая база
// для повторного Edit; Merged - слияние тел с маркерами конфликта, его правит человек.
type ConflictError struct{ Rev, Merged string }

// Error даёт прежний текст отказа (код 3 в CLI).
func (e *ConflictError) Error() string {
	return ErrConflict.Error() + ": body changed after tix show, reread and retry"
}

// Is делает errors.Is(err, ErrConflict) истинным.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }
