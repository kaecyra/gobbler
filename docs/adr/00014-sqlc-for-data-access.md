# ADR-00014: Data access through sqlc-generated code

**Status:** Accepted

**Date:** 2026-10-12

## Context

ADR-00002 fixes the storage stack: SQLite through `modernc.org/sqlite` and
`goose` migrations written in SQL. It said nothing about how Go code issues
queries, so the `catalog` and `store` packages grew hand-written
`database/sql` calls: query strings, manual `Scan` into structs, and each
package's own transaction helper.

That costs boilerplate on every query, gives no compile-time check that a query
matches the schema, and has already produced two copies of the same
transaction helper.

Options considered:

- **Hand-written `database/sql`.** No dependency and full control. Every query
  is a string checked only when a test runs it, and every result is a hand
  `Scan` that drifts when a column changes.
- **A full ORM (GORM, ent).** Least code for simple CRUD. It brings a second
  model of the schema beside the `goose` migrations, hides the SQL that runs,
  and works against the queries this app needs most: FTS5 search and walking
  the step graph. GORM resolves errors at run time through reflection; ent
  replaces SQL migrations with its own schema DSL, which contradicts ADR-00002.
- **sqlc.** Queries stay as SQL; sqlc reads the `goose` migrations as the
  schema and generates typed Go functions and row structs over `database/sql`.
  A query that does not match the schema fails at generation, not at run time.
  Chosen.

## Decision

- Packages that own SQL (`catalog`, `store`, and any later package with its own
  tables) write their queries as `.sql` files inside the package. sqlc
  generates a Go subpackage for each, beside the queries. The SQL stays in the
  package that owns it; nothing outside imports another package's generated
  code.
- sqlc reads the schema from the embedded `goose` migrations. There is no
  second schema file.
- Generated code is committed, so `go build` and `go test` need no sqlc
  install. sqlc is pinned as a Go `tool` dependency, and the gate fails when the
  generated code is stale.
- Transactions use the generated `Queries.WithTx` through one shared helper
  in `db`, not a helper per package.
- A query that sqlc cannot express (for example, one its SQLite parser
  rejects) may be hand-written in the owning package. A comment there names
  the sqlc limitation.

## Consequences

ADR-00002's storage choice stands; this record governs only how Go reaches the
database. Adding or changing a query now means editing SQL and regenerating,
and contributors run the generator rather than editing generated files.

**Obligations:**

1. `sqlc.yaml` exists at the repository root, with one entry per SQL-owning
   package, and its schema points at the `goose` migrations directory.
2. sqlc is listed in `go.mod`'s `tool` block. A `make generate` target runs it.
   `make check` runs `sqlc diff` and fails on any difference.
3. `catalog` and `store` hold no hand-written query strings, except those
   carrying a comment that names the sqlc limitation they work around.
4. One transaction helper, in the `db` package, serves both `catalog` and
   `store`.
