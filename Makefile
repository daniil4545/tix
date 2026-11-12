# Project gate, stages 0-2 of the Go Makefile template.
GITLEAKS := go run github.com/zricethezav/gitleaks/v8@v8.30.1
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.9.0
# Needs go and golangci-lint. The project has no DB or network: integration tests run in the same go test.
.PHONY: ci-test build test test-fast test-race vet fmt fmt-check lint tidy-check vuln secrets check pr-check ci-check release-smoke bench e2e-agents

# VERSION=1.2.3 make build sets the version in tix --version; without it the version is dev.
VERSION ?= dev

build:
	go build ./...
	go build -ldflags "-X main.version=$(VERSION)" -o bin/tix ./cmd/tix

# Tests run as on a CI runner: no machine git config and no author guessed from the host name, so a
# test that commits without naming its author fails here too.
GIT_BARE := GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
	GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=user.useConfigOnly GIT_CONFIG_VALUE_0=true

# Fast tier: no race detector and no kill -9.
test-fast:
	$(GIT_BARE) go test -short ./... $(if $(RUN),-run $(RUN) -count=1,)

test:
	$(GIT_BARE) go test -count=1 ./... $(if $(RUN),-run $(RUN),)

# The TUI, the watcher and the store run goroutines at once: CI runs every test with the race detector.
test-race:
	$(GIT_BARE) go test -race -count=1 ./...

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

SHELLCHECK := uvx --from shellcheck-py==0.11.0.1 shellcheck
ACTIONLINT := go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
TYPOS := uvx typos==1.52.0
YAMLLINT := uvx yamllint==1.38.0
MARKDOWNLINT := npx --yes markdownlint-cli2@0.24.0
CHECKMAKE := go run github.com/mrtazz/checkmake/cmd/checkmake@0.2.2

# All static checks, the same set locally and in CI: gofmt, go vet, golangci-lint v2.15.0, shellcheck,
# actionlint, typos, yamllint, markdownlint (root *.md), checkmake. No tests.
# gosec and the length and nesting limits live in .golangci.yml.
lint: fmt-check vet
	golangci-lint run --timeout 120s
	git ls-files -z '*.sh' | xargs -0 $(SHELLCHECK)
	$(ACTIONLINT)
	$(TYPOS)
	$(YAMLLINT) -c .yamllint.yml $$(git ls-files '*.yml' '*.yaml')
	$(MARKDOWNLINT) '*.md'
	$(CHECKMAKE) --config=checkmake.ini Makefile

# Everything but lint, for the CI job that runs next to the lint job.
ci-test: tidy-check secrets test-race vuln build release-smoke

# Acceptance: lint, tests, build.
check: lint test build

# Fast local check before a push: the fast tier, minutes.
pr-check: lint tidy-check test-fast build

# PR into main: everything with races, known vulnerabilities and secrets, and the release archive
# of this system unpacks, starts and keeps a task.
ci-check: lint ci-test

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
