# Project gate, stages 0-2 of the Go Makefile template.
GITLEAKS := go run github.com/zricethezav/gitleaks/v8@v8.30.1
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.9.0
# Needs go and golangci-lint. The project has no DB or network: integration tests run in the same go test.
.PHONY: build test test-fast test-race vet fmt fmt-check lint tidy-check vuln secrets check pr-check ci-check release-smoke bench e2e-agents

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

# The TUI, the watcher and the store run goroutines at once: CI runs every test with the race detector.
test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

fmt-check:
	@out=$$(gofmt -l .); \
	test -z "$$out" || { echo "gofmt needed:"; echo "$$out"; exit 1; }

# go.mod and go.sum match the imports.
tidy-check:
	go mod tidy -diff

vuln:
	$(GOVULNCHECK) ./...

# gitleaks ignores .gitignore, so it scans what git would commit instead of local files.
secrets:
	@set -e; tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	cp "$$(git rev-parse --git-path index)" "$$tmp/index"; \
	GIT_INDEX_FILE="$$tmp/index" git add -A; \
	tree=$$(GIT_INDEX_FILE="$$tmp/index" git write-tree); \
	mkdir "$$tmp/tree"; git archive "$$tree" | tar -x -C "$$tmp/tree"; \
	cd "$$tmp/tree" && $(GITLEAKS) dir --no-banner --redact .

# gosec and the length and nesting limits live in .golangci.yml.
lint:
	golangci-lint run --timeout 120s

# Acceptance: format, vet, linter, tests, build.
check: fmt-check vet lint test build

# Fast local check before a push: the fast tier, minutes.
pr-check: fmt-check vet lint tidy-check test-fast build

# PR into main: everything with races, known vulnerabilities and secrets, and the release archive
# of this system unpacks, starts and keeps a task.
ci-check: fmt-check vet lint tidy-check secrets test-race vuln build release-smoke

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
