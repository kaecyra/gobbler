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
		offers := fs.String("offers", "", "write suggested new ingredients and partial matches to this JSON file")
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
	return writeOffers(offersPath, rep)
}

func writeOffers(path string, rep usda.Report) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create offers file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close offers file: %w", cerr))
		}
	}()
	return encodeOffers(f, rep)
}

// offersFile is the JSON the operator reviews: foods that match nothing
// (candidate new ingredients) and foods that match only the start of an
// ingredient name (add the alias to opt in, then re-run).
type offersFile struct {
	Offers         []usda.Offer        `json:"offers"`
	PartialMatches []usda.PartialMatch `json:"partial_matches"`
}

func encodeOffers(w io.Writer, rep usda.Report) error {
	out := offersFile{Offers: rep.Offers, PartialMatches: rep.PartialMatches}
	if out.Offers == nil {
		out.Offers = []usda.Offer{}
	}
	if out.PartialMatches == nil {
		out.PartialMatches = []usda.PartialMatch{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("write offers: %w", err)
	}
	return nil
}
