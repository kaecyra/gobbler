# Migrations

goose SQL migrations, embedded into the binary by `internal/db`. Each file is
`NNNNN_name.sql`; numbers are assigned by the plan graph, never chosen by hand.
This README also keeps the directory non-empty so `go:embed` accepts it.
