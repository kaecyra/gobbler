package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{Server: config.Server{DataDir: t.TempDir()}}
}

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(context.Background(), testConfig(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func pragma(t *testing.T, c *sql.Conn, name string) string {
	t.Helper()
	var v string
	if err := c.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestOpenSetsPragmasOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	d, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = d.Close() }()

	if _, err := os.Stat(filepath.Join(cfg.Server.DataDir, FileName)); err != nil {
		t.Fatalf("database file not created in data dir: %v", err)
	}
	// Two simultaneously held connections prove the pragmas are per connection.
	c1, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := d.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()

	for _, c := range []*sql.Conn{c1, c2} {
		if got := pragma(t, c, "journal_mode"); got != "wal" {
			t.Errorf("journal_mode = %q, want wal", got)
		}
		if got := pragma(t, c, "foreign_keys"); got != "1" {
			t.Errorf("foreign_keys = %q, want 1", got)
		}
		if got := pragma(t, c, "busy_timeout"); got != "5000" {
			t.Errorf("busy_timeout = %q, want 5000", got)
		}
	}
}

func TestOpenCreatesMissingDataDir(t *testing.T) {
	cfg := config.Config{Server: config.Server{DataDir: filepath.Join(t.TempDir(), "nested", "data")}}
	d, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = d.Close()
}

func TestOpenFailsWhenDataDirIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), config.Config{Server: config.Server{DataDir: f}})
	if err == nil {
		t.Fatal("want error when data dir is a file")
	}
}

func fixture(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

const createWidgets = `-- +goose Up
CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
-- +goose Down
DROP TABLE widgets;
`

const addColor = `-- +goose Up
ALTER TABLE widgets ADD COLUMN color TEXT;
-- +goose Down
ALTER TABLE widgets DROP COLUMN color;
`

func TestMigrateAppliesToEmptyDatabaseAndRecordsVersion(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	fsys := fixture(map[string]string{"00001_widgets.sql": createWidgets, "00002_color.sql": addColor})

	if err := Migrate(ctx, d, fsys); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO widgets (name, color) VALUES ('a', 'red')"); err != nil {
		t.Fatalf("schema not applied: %v", err)
	}
	v, err := Version(ctx, d, fsys)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
}

func TestMigrateRerunIsNoOp(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	fsys := fixture(map[string]string{"00001_widgets.sql": createWidgets})

	if err := Migrate(ctx, d, fsys); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO widgets (name) VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, d, fsys); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM widgets").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v; rerun must not touch data", n, err)
	}
	var applied int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id > 0").Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("recorded migrations = %d, err = %v, want 1", applied, err)
	}
}

func TestMigrateFailureNamesMigrationAndPropagates(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	fsys := fixture(map[string]string{
		"00001_widgets.sql": createWidgets,
		"00002_broken.sql":  "-- +goose Up\nCREATE TABLE nope (;\n-- +goose Down\nSELECT 1;\n",
	})

	err := Migrate(ctx, d, fsys)
	if err == nil {
		t.Fatal("Migrate swallowed a failing migration")
	}
	if !strings.Contains(err.Error(), "00002_broken.sql") {
		t.Fatalf("error %q does not name the failing migration", err)
	}
	v, verr := Version(ctx, d, fsys)
	if verr != nil || v != 1 {
		t.Fatalf("version = %d, err = %v; earlier migration should stay applied", v, verr)
	}
}

func TestMigrateHonoursCancelledContext(t *testing.T) {
	d := openTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Migrate(ctx, d, fixture(map[string]string{"00001_widgets.sql": createWidgets}))
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestMigrateEmptyFilesystemIsNoOp(t *testing.T) {
	d := openTest(t)
	if err := Migrate(context.Background(), d, fstest.MapFS{}); err != nil {
		t.Fatalf("Migrate with no migrations: %v", err)
	}
}

func TestMigrateShippedMigrationsWithNoMigrationsYet(t *testing.T) {
	d := openTest(t)
	if err := Migrate(context.Background(), d, Migrations()); err != nil {
		t.Fatalf("Migrate shipped: %v", err)
	}
}

func TestOpenRefusesDatabaseThatCannotEnterWAL(t *testing.T) {
	// An in-memory database ignores the WAL request and reports "memory".
	d, err := connect(context.Background(), "file::memory:?_pragma=journal_mode(wal)", "memory")
	if err == nil {
		_ = d.Close()
		t.Fatal("want error when journal mode is not wal")
	}
	if !strings.Contains(err.Error(), `"memory"`) {
		t.Fatalf("error %q does not name the actual journal mode", err)
	}
}

func TestVersionOnFreshDatabaseIsZero(t *testing.T) {
	d := openTest(t)
	v, err := Version(context.Background(), d, fixture(map[string]string{"00001_widgets.sql": createWidgets}))
	if err != nil || v != 0 {
		t.Fatalf("version = %d, err = %v, want 0", v, err)
	}
}

func TestVersionWithNoMigrationsIsZero(t *testing.T) {
	d := openTest(t)
	v, err := Version(context.Background(), d, fstest.MapFS{})
	if err != nil || v != 0 {
		t.Fatalf("version = %d, err = %v, want 0", v, err)
	}
}
