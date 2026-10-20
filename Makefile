# Гейт проекта по ~/.claude/templates/Makefile.go: ступени 0-2 из ~/.claude/rules/verification.md.
# Нужны go и golangci-lint. Проект без БД и сети: интеграционные тесты идут в том же go test.
.PHONY: build test test-fast vet fmt fmt-check lint check bench e2e-agents

build:
	go build ./...
	go build -o bin/tk ./cmd/tk

# Быстрый ярус: без гонки и kill -9.
test-fast:
	go test -short ./... $(if $(RUN),-run $(RUN) -count=1,)

test:
	go test -count=1 ./... $(if $(RUN),-run $(RUN),)

vet:
	go vet ./...

fmt:
	gofmt -l -w .

fmt-check:
	@out=$$(gofmt -l .); \
	test -z "$$out" || { echo "gofmt needed:"; echo "$$out"; exit 1; }

# gosec и лимиты длины и вложенности - в .golangci.yml.
lint:
	golangci-lint run --timeout 120s

# Приёмка: формат, vet, линтер, тесты, сборка.
check: fmt-check vet lint test build

# Замер скорости: tkbench сам создаёт 1000 задач во временном TRACKER_HOME.
bench: build
	go run ./cmd/tkbench bin/tk

# Проверка трёх агентов (Claude Code, Codex, OpenCode) на живых CLI делает З3.
e2e-agents:
	@echo "e2e-agents: цель появится в задаче З3"
