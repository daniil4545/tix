package core

// SetAfterRead sets the seam between the file read and the check before rename (step 4 of 2.1).
func SetAfterRead(s *Store, fn func(path string, attempt int)) { s.afterRead = fn }

// RankTmpPrefix is the name prefix of the rank cache temp file.
const RankTmpPrefix = rankTmpPrefix
