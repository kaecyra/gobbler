# Gates for gobbler. goimports runs through the `tool` directive in go.mod, so
# its version is pinned there.

COVER_MIN  := 70
MODULE     := $(shell go list -m)
# Single source of truth for the linter version: `make tools` installs it,
# `make lint` refuses to run any other, and CI calls `make tools`.
GOLANGCI_LINT_VERSION := v2.14.0

E2E_SETUP := cd e2e && npm ci && npx playwright install chromium

.PHONY: tools fmt fmt-check lint test test-scripts e2e e2e-routes version-check check

## tools: install the pinned golangci-lint
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

## fmt: rewrite Go files with gofmt and goimports
fmt:
	gofmt -w .
	go tool goimports -local $(MODULE) -w .

## fmt-check: fail if any Go file is not formatted (CI)
fmt-check:
	@out="$$(gofmt -l .; go tool goimports -local $(MODULE) -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files (run make fmt):"; echo "$$out"; exit 1; fi

## lint: golangci-lint and go vet
lint:
	@golangci-lint version 2>&1 | grep -q "version $(GOLANGCI_LINT_VERSION:v%=%) " || { echo "golangci-lint $(GOLANGCI_LINT_VERSION) required; run make tools"; exit 1; }
	golangci-lint run ./...
	go vet ./...

## test: run tests, failing below $(COVER_MIN)% line coverage
test:
	@f="$$(mktemp)"; trap 'rm -f "$$f"' EXIT; \
	go test -coverprofile="$$f" ./... || exit 1; \
	total="$$(go tool cover -func="$$f" | awk '/^total:/ {sub("%","",$$3); print $$3}')"; \
	echo "total coverage: $$total% (minimum $(COVER_MIN)%)"; \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { exit !(t+0 >= m+0) }' || { echo "coverage below $(COVER_MIN)%"; exit 1; }

## test-scripts: test the shell and Node helpers behind the gates
test-scripts:
	bash .github/scripts/check-version-bump_test.sh
	@command -v node >/dev/null || { echo "node is required; install Node"; exit 1; }
	node --test e2e/fixtures/*.test.mjs

## e2e-routes: fail if a route registered in internal/web has no e2e test
e2e-routes:
	@command -v node >/dev/null || { echo "node is required for e2e; install Node, then: $(E2E_SETUP)"; exit 1; }
	node e2e/fixtures/route-coverage.mjs

## e2e: build gobbler and run the Playwright suite (installs nothing)
e2e: e2e-routes
	@test -d e2e/node_modules/@playwright/test || { echo "e2e dependencies missing; setup: $(E2E_SETUP)"; exit 1; }
	@d="$$(mktemp -d)"; trap 'rm -rf "$$d"' EXIT; \
	go build -o "$$d/gobbler" ./cmd/gobbler || exit 1; \
	cd e2e && GOBBLER_E2E_BIN="$$d/gobbler" npx --no-install playwright test

## version-check: fail if source changed against origin/main without a VERSION bump
version-check:
	bash .github/scripts/check-version-bump.sh origin/main

## check: every gate; fails if any one fails
check: fmt-check lint test test-scripts e2e version-check
