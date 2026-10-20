package core

// ConflictError - конфликт слияния тела при Edit. Rev - блоб, на который правка
// опиралась; Merged - слияние с маркерами конфликта. Заглушка зоны 0:
// mergeBody пока возвращает голый ErrConflict, перейдёт на этот тип зона H.
type ConflictError struct{ Rev, Merged string }

// Error совпадает с текстом, который mergeBody отдаёт сейчас (код 3 в CLI).
func (e *ConflictError) Error() string {
	return ErrConflict.Error() + ": body changed after tk show, reread and retry"
}

// Is делает errors.Is(err, ErrConflict) истинным.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }
