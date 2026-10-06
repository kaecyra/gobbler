# Gates for gobbler. goimports runs through the `tool` directive in go.mod, so
# its version is pinned there.

COVER_MIN  := 70

.PHONY: fmt fmt-check lint test

## fmt: rewrite Go files with gofmt and goimports
fmt:
	gofmt -w .
	go tool goimports -local github.com/kaecyra/gobbler -w .

## fmt-check: fail if any Go file is not formatted (CI)
fmt-check:
	@out="$$(gofmt -l .; go tool goimports -local github.com/kaecyra/gobbler -l .)"; \
	if [ -n "$$out" ]; then echo "unformatted files (run make fmt):"; echo "$$out"; exit 1; fi

## lint: golangci-lint and go vet
lint:
	golangci-lint run ./...
	go vet ./...

## test: run tests, failing below $(COVER_MIN)% line coverage
test:
	@f="$$(mktemp)"; trap 'rm -f "$$f"' EXIT; \
	go test -coverprofile="$$f" ./... || exit 1; \
	total="$$(go tool cover -func="$$f" | awk '/^total:/ {sub("%","",$$3); print $$3}')"; \
	echo "total coverage: $$total% (minimum $(COVER_MIN)%)"; \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { exit !(t+0 >= m+0) }' || { echo "coverage below $(COVER_MIN)%"; exit 1; }
