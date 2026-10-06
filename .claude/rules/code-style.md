# Code style

> **Always on.** Applies to any line of code in this repository.
> `gofmt` and `goimports` own formatting and `golangci-lint` owns the rest;
> neither is argued with by hand.

## What the tools enforce, and what they don't

Run `make fmt` before committing; the pre-commit hook runs it too. Linter
configuration lives in `.golangci.yml`, created in the foundation work.

The tools cannot enforce layout and shape, so these are the rules:

- **Layout.** `cmd/gobbler/` holds the entry point. Everything else lives in
  `internal/<package>/` (ADR-00002). Name a package for what it provides —
  `ingest`, `quantity`, `catalog` — never `util`, `common` or `helpers`.
- **Interfaces belong to the consumer.** Accept interfaces, return concrete
  types. Define an interface in the package that calls it, sized to what that
  package calls. The LLM provider interface (ADR-00007) is the model.
- **`context.Context` first** on anything that does I/O.
- **SQL lives in its store package.** Handlers and templ components never build
  queries.
- **Pure logic stays pure.** Quantity arithmetic, unit conversion, the
  ingredient-line parser and shopping-list merging take values and return
  values, so they can be table-tested without a database.

## Logging

`log/slog` with the JSON handler. Key–value pairs with lowercase snake_case
keys. Never log an API key, the Cloudflare Access JWT or cookie, or S3
credentials.

## Never type a version or an identifier from memory

Version numbers, action tags, runtime pins, model identifiers, protocol
details: check the live source of truth — the registry, the release page, the
specification — before writing one into a file. Training data is stale by
construction and gives plausible wrong answers rather than obviously wrong
ones, which is the expensive kind.

Where a value genuinely cannot be verified — egress blocked, source offline —
say so at the point of use rather than committing a guess that reads as
checked.

Here that means: Go module versions from `go list -m -versions <module>` or
pkg.go.dev; Claude and Gemini model identifiers from the provider's current
model list, and only ever in configuration, never in code (ADR-00007).
