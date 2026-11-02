# Project gate, stages 0-2 of the Go Makefile template.
# Needs go and golangci-lint. The project has no DB or network: integration tests run in the same go test.
.PHONY: build test test-fast vet fmt fmt-check lint check pr-check ci-check release-smoke bench e2e-agents

# VERSION=1.2.3 make build sets the version in tix --version; without it the version is dev.
VERSION ?= dev

build:
	go build ./...
	go build -ldflags "-X main.version=$(VERSION)" -o bin/tix ./cmd/tix

# Fast tier: no race detector and no kill -9.
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

# gosec and the length and nesting limits live in .golangci.yml.
lint:
	golangci-lint run --timeout 120s

# Acceptance: format, vet, linter, tests, build.
check: fmt-check vet lint test build

# PR into dev: the fast tier, minutes.
pr-check: fmt-check vet lint test-fast build

# PR into main: everything, and the release archive of this system unpacks, starts and keeps a task.
ci-check: check release-smoke

release-smoke:
	goreleaser release --snapshot --clean --skip=publish
	@set -e; d=$$(mktemp -d); trap 'rm -rf "$$d"' EXIT; \
	tar -xzf dist/tix_*_$$(go env GOOS)_$$(go env GOARCH).tar.gz -C "$$d"; \
	"$$d/tix" --version; \
	TIX_HOME="$$d/home" "$$d/tix" new "release smoke" >/dev/null; \
	TIX_HOME="$$d/home" "$$d/tix" ls | grep -q "release smoke"

# Speed benchmark: tixbench creates 1000 tasks in a temporary TIX_HOME itself.
bench: build
	go run ./cmd/tixbench bin/tix

# A live run of two agents (Claude Code, Codex) spends subscription limits: only with consent.
# Without E2E_ARGS=--yes the script prints a warning and exits with code 2. Subset: E2E_ARGS="--yes --agents claude".
e2e-agents:
	scripts/e2e-agents.sh $(E2E_ARGS)
