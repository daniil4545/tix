package core

// SetAfterRead ставит шов между чтением файла и сверкой перед rename (шаг 4 из 2.1).
func SetAfterRead(s *Store, fn func(path string, attempt int)) { s.afterRead = fn }
