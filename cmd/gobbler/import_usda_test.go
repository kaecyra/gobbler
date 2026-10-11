package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

const usdaFixtureDir = "../../internal/catalog/usda/testdata/fdc"

func TestImportUSDACommandFillsCatalogAndWritesOffers(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv(config.EnvDataDir, dataDir)
	t.Setenv(config.EnvDevAuthBypass, "true")
	if code := execute([]string{"migrate"}, []command{migrateCommand}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("migrate exit code = %d", code)
	}
	offers := filepath.Join(t.TempDir(), "offers.json")

	args := []string{"import-usda", "-dir", usdaFixtureDir, "-offers", offers}
	if code := execute(args, []command{importUSDACommand}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("import-usda exit code = %d, want 0", code)
	}

	raw, err := os.ReadFile(offers)
	if err != nil {
		t.Fatalf("offers file: %v", err)
	}
	var got []struct{ Name string }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("offers file is not JSON: %v", err)
	}
	if len(got) == 0 {
		t.Errorf("offers file is empty; the fixture has foods matching no seeded ingredient")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var linked int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ingredients WHERE usda_fdc_id = 170001`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != 1 {
		t.Errorf("ingredients linked to FDC 170001 = %d, want 1 (seeded butter)", linked)
	}
}

func TestImportUSDACommandRequiresDir(t *testing.T) {
	t.Setenv(config.EnvDataDir, t.TempDir())
	t.Setenv(config.EnvDevAuthBypass, "true")
	if code := execute([]string{"import-usda"}, []command{importUSDACommand}, &bytes.Buffer{}); code == 0 {
		t.Fatal("want non-zero exit without -dir")
	}
}

func TestImportUSDACommandFailsOnMissingFiles(t *testing.T) {
	t.Setenv(config.EnvDataDir, t.TempDir())
	t.Setenv(config.EnvDevAuthBypass, "true")
	if code := execute([]string{"migrate"}, []command{migrateCommand}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("migrate exit code = %d", code)
	}
	args := []string{"import-usda", "-dir", t.TempDir()}
	if code := execute(args, []command{importUSDACommand}, &bytes.Buffer{}); code == 0 {
		t.Fatal("want non-zero exit when the CSV files are absent")
	}
}
