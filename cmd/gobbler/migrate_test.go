package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/config"
)

func TestMigrateCommandSucceedsAgainstFreshDataDir(t *testing.T) {
	t.Setenv(config.EnvDataDir, t.TempDir())
	t.Setenv(config.EnvDevAuthBypass, "true")
	if code := execute([]string{"migrate"}, []command{migrateCommand}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestMigrateCommandRejectsInvalidConfig(t *testing.T) {
	t.Setenv(config.EnvDataDir, t.TempDir())
	t.Setenv(config.EnvDevAuthBypass, "")
	t.Setenv(config.EnvAccessTeamDomain, "")
	t.Setenv(config.EnvAccessAUD, "")
	var out bytes.Buffer
	if code := execute([]string{"migrate"}, []command{migrateCommand}, &out); code == 0 {
		t.Fatal("want non-zero exit on invalid config")
	}
}

func TestMigrateFailingMigrationReturnsNamedError(t *testing.T) {
	cfg := config.Config{Server: config.Server{DataDir: t.TempDir()}}
	fsys := fstest.MapFS{"00001_bad.sql": {Data: []byte("-- +goose Up\nCREATE TABLE (;\n-- +goose Down\nSELECT 1;\n")}}
	err := migrate(context.Background(), cfg, fsys)
	if err == nil || !strings.Contains(err.Error(), "00001_bad.sql") {
		t.Fatalf("err = %v, want error naming 00001_bad.sql", err)
	}
}
