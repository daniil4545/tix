package core

// ConflictError - a body merge conflict in Edit. Rev - the blob of the current file: the new base
// for a repeated Edit; Merged - the merge of bodies with conflict markers, a human fixes it.
type ConflictError struct{ Rev, Merged string }

// Error gives the former refusal text (code 3 in the CLI).
func (e *ConflictError) Error() string {
	return ErrConflict.Error() + ": body changed after tix show, reread and retry"
}

// Is makes errors.Is(err, ErrConflict) true.
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }
