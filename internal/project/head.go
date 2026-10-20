package project

// Head возвращает полный sha HEAD репозитория в dir без вызова git. false: нет HEAD
// (нет коммитов, reftable, вне git).
func Head(dir string) (string, bool) {
	return "", false
}
