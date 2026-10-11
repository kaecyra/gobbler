package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/catalog/usda"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// importUSDACommand enriches the ingredient catalog from a FoodData Central CSV
// download. It is run by the operator, never at start-up (ADR-00008).
var importUSDACommand = command{
	name:    "import-usda",
	summary: "fill densities and FDC ids from a FoodData Central CSV download",
	run: func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("import-usda", flag.ContinueOnError)
		dir := fs.String("dir", "", "directory holding food.csv, food_portion.csv and measure_unit.csv (required)")
		offers := fs.String("offers", "", "write suggested new ingredients to this JSON file")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *dir == "" {
			return errors.New("-dir is required")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return importUSDA(ctx, cfg, *dir, *offers)
	},
}

// importUSDA opens the database and runs the import over the CSV files in dir.
func importUSDA(ctx context.Context, cfg config.Config, dir, offersPath string) error {
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			slog.WarnContext(ctx, "close database", "error", err.Error())
		}
	}()
	rep, err := usda.Import(ctx, catalog.New(d), os.DirFS(dir))
	if err != nil {
		return fmt.Errorf("import USDA data: %w", err)
	}
	if offersPath == "" {
		return nil
	}
	return writeOffers(offersPath, rep.Offers)
}

func writeOffers(path string, offers []usda.Offer) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create offers file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close offers file: %w", cerr))
		}
	}()
	return encodeOffers(f, offers)
}

func encodeOffers(w io.Writer, offers []usda.Offer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if offers == nil {
		offers = []usda.Offer{}
	}
	if err := enc.Encode(offers); err != nil {
		return fmt.Errorf("write offers: %w", err)
	}
	return nil
}
