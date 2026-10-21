# Гейт проекта по ~/.claude/templates/Makefile.go: ступени 0-2 из ~/.claude/rules/verification.md.
# Нужны go и golangci-lint. Проект без БД и сети: интеграционные тесты идут в том же go test.
.PHONY: build test test-fast vet fmt fmt-check lint check bench e2e-agents

# VERSION=1.2.3 make build подставляет версию в tix --version; без неё dev.
VERSION ?= dev

build:
	go build ./...
	go build -ldflags "-X main.version=$(VERSION)" -o bin/tix ./cmd/tix

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

# Замер скорости: tixbench сам создаёт 1000 задач во временном TIX_HOME.
bench: build
	go run ./cmd/tixbench bin/tix

# Живой прогон двух агентов (Claude Code, Codex) тратит лимиты подписок: только с согласием.
# Без E2E_ARGS=--yes скрипт печатает предупреждение и выходит с кодом 2. Подмножество: E2E_ARGS="--yes --agents claude".
e2e-agents:
	scripts/e2e-agents.sh $(E2E_ARGS)
