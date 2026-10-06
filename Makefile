# Gates for gobbler. goimports runs through the `tool` directive in go.mod, so
# its version is pinned there.

COVER_MIN  := 70
MODULE     := $(shell go list -m)
# Single source of truth for the linter version: `make tools` installs it,
# `make lint` refuses to run any other, and CI calls `make tools`.
GOLANGCI_LINT_VERSION := v2.14.0

.PHONY: tools fmt fmt-check lint test

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
