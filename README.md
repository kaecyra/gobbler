# gobbler

A self-hosted, single-user recipe manager. It ingests recipes from URLs, pasted
text and photos, stores them as structured data, and builds merged,
aisle-grouped shopping lists from them. One Go binary over a SQLite file.

The design is in [docs/design.md](./docs/design.md) and the decisions in
[docs/adr/](./docs/adr/README.md). Contributor and agent rules are in
[AGENTS.md](./AGENTS.md).

## Running with Docker Compose

```sh
cp .env.example .env   # set the tunnel token and the Access team domain and AUD
docker compose up -d --build
```

Compose runs `gobbler` and `cloudflared`. The database lives on the named
volume `gobbler-data`, migrations apply at start-up, and `gobbler` publishes no
host port: the only way in is the Cloudflare tunnel, whose public hostname
should point at `http://gobbler:8080`. The image has no Node runtime and
nothing from `e2e/`.

## Development

Requires Go (the version in `go.mod`) and [golangci-lint](https://golangci-lint.run/) v2.14.0.
Run `make tools` to install the pinned version (set by `GOLANGCI_LINT_VERSION` in the `Makefile`);
`make lint` fails if a different version is on `PATH`.

```sh
make fmt    # gofmt and goimports, rewrites files
make lint   # golangci-lint and go vet
make test   # go test; fails below 70% line coverage
make e2e    # builds gobbler, runs the Playwright suite in e2e/ (installs nothing)
make check  # fmt-check, lint, test, test-scripts, e2e and version-check
```

The end-to-end suite needs Node (ADR-00013). Provision a checkout once; `make e2e`
fails with a pointer here if it was skipped:

```sh
cd e2e && npm ci && npx playwright install chromium
```

`make version-check` fails when source changed against `origin/main` without a
`VERSION` bump; docs, ADRs, Markdown, `.memory/` and `.gitignore` never need one.

Install the git hooks once per clone with [lefthook](https://lefthook.dev/):

```sh
lefthook install
```

Pre-commit runs `make fmt`; pre-push runs `make lint`, `make test`, `make e2e` and `make version-check`.
