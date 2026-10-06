# gobbler

A self-hosted, single-user recipe manager. It ingests recipes from URLs, pasted
text and photos, stores them as structured data, and builds merged,
aisle-grouped shopping lists from them. One Go binary over a SQLite file.

The design is in [docs/design.md](./docs/design.md) and the decisions in
[docs/adr/](./docs/adr/README.md). Contributor and agent rules are in
[AGENTS.md](./AGENTS.md).

## Development

Requires Go (the version in `go.mod`) and [golangci-lint](https://golangci-lint.run/) v2.

```sh
make fmt    # gofmt and goimports, rewrites files
make lint   # golangci-lint and go vet
make test   # go test; fails below 70% line coverage
```

`make e2e` and `make check` do not exist yet.

Install the git hooks once per clone with [lefthook](https://lefthook.dev/):

```sh
lefthook install
```

Pre-commit runs `make fmt`; pre-push runs `make lint` and `make test`.
