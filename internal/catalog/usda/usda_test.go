package usda

import (
	"context"
	"database/sql"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

func newStore(t *testing.T) (*catalog.Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	// Start from an empty catalog so each test controls exactly which
	// ingredients exist; the migration seed would otherwise match fixture foods.
	if _, err := d.ExecContext(ctx, `DELETE FROM ingredients`); err != nil {
		t.Fatalf("clear seeded ingredients: %v", err)
	}
	return catalog.New(d), d
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

func create(t *testing.T, s *catalog.Store, in catalog.Ingredient) catalog.Ingredient {
	t.Helper()
	got, err := s.CreateIngredient(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateIngredient(%q): %v", in.Name, err)
	}
	return got
}

func get(t *testing.T, s *catalog.Store, id int64) catalog.Ingredient {
	t.Helper()
	got, err := s.GetIngredient(context.Background(), id)
	if err != nil {
		t.Fatalf("GetIngredient(%d): %v", id, err)
	}
	return got
}

func approx(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", what, want)
	}
	if math.Abs(*got-want) > 1e-4 {
		t.Errorf("%s = %v, want %v", what, *got, want)
	}
}

func run(t *testing.T, s *catalog.Store) Report {
	t.Helper()
	rep, err := Import(context.Background(), s, os.DirFS("testdata/fdc"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return rep
}

func TestImportFillsEmptyDensityAndID(t *testing.T) {
	s, _ := newStore(t)
	butter := create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy", Aliases: []string{"unsalted butter"}})
	flour := create(t, s, catalog.Ingredient{Name: "flour", Aisle: "baking", Reviewed: true})
	honey := create(t, s, catalog.Ingredient{Name: "honey", Aisle: "pantry"})

	rep := run(t, s)

	b := get(t, s, butter.ID)
	approx(t, "butter density", b.DensityGPerML, 227/236.5882365) // cup portion, seq 1, beats the tbsp one
	if b.USDAID == nil || *b.USDAID != 170001 {
		t.Errorf("butter USDAID = %v, want 170001", b.USDAID)
	}
	approx(t, "flour density", get(t, s, flour.ID).DensityGPerML, 125/236.5882365) // measure_unit name path
	approx(t, "honey density", get(t, s, honey.ID).DensityGPerML, 42/(2*14.78676478125))

	if rep.Filled != 3 || rep.Matched != 3 || rep.SkippedCurated != 0 {
		t.Errorf("report = %+v", rep)
	}
}

func TestImportNeverChangesAnythingButEmptyDensityAndID(t *testing.T) {
	cases := []struct {
		name string
		in   catalog.Ingredient
		// want applies the only changes the import may make.
		want func(catalog.Ingredient) catalog.Ingredient
	}{
		{
			name: "all empty fills density and id, keeps reviewed false",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", Aliases: []string{"unsalted butter"}, Reviewed: false},
			want: func(i catalog.Ingredient) catalog.Ingredient {
				i.DensityGPerML, i.USDAID = f64(227/236.5882365), i64(170001)
				return i
			},
		},
		{
			name: "all empty fills density and id, keeps reviewed true",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", Reviewed: true},
			want: func(i catalog.Ingredient) catalog.Ingredient {
				i.DensityGPerML, i.USDAID = f64(227/236.5882365), i64(170001)
				return i
			},
		},
		{
			name: "curated density kept, id still linked",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", DensityGPerML: f64(0.91)},
			want: func(i catalog.Ingredient) catalog.Ingredient { i.USDAID = i64(170001); return i },
		},
		{
			name: "curated id of another food kept and density not borrowed",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", USDAID: i64(999)},
			want: func(i catalog.Ingredient) catalog.Ingredient { return i },
		},
		{
			name: "curated id of the same food leaves density to fill",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", USDAID: i64(170001)},
			want: func(i catalog.Ingredient) catalog.Ingredient { i.DensityGPerML = f64(227 / 236.5882365); return i },
		},
		{
			name: "curated density and id both kept",
			in:   catalog.Ingredient{Name: "butter", Aisle: "dairy", DensityGPerML: f64(0.9), USDAID: i64(5)},
			want: func(i catalog.Ingredient) catalog.Ingredient { return i },
		},
		{
			name: "curated aisle and aliases kept",
			in:   catalog.Ingredient{Name: "butter", Aisle: "chilled aisle 7", Aliases: []string{"salted butter", "sweet butter"}},
			want: func(i catalog.Ingredient) catalog.Ingredient {
				i.DensityGPerML, i.USDAID = f64(227/236.5882365), i64(170001)
				return i
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			before := create(t, s, tc.in)
			run(t, s)
			got := get(t, s, before.ID)
			want := tc.want(before)
			if got.DensityGPerML != nil && want.DensityGPerML != nil {
				approx(t, "density", got.DensityGPerML, *want.DensityGPerML)
				got.DensityGPerML, want.DensityGPerML = nil, nil
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestImportIsIdempotent(t *testing.T) {
	s, d := newStore(t)
	create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy"})
	create(t, s, catalog.Ingredient{Name: "flour", Aisle: "baking", DensityGPerML: f64(0.5)})
	create(t, s, catalog.Ingredient{Name: "honey", Aisle: "pantry"})

	first := run(t, s)
	once, err := s.ListIngredients(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ingredients`).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	second := run(t, s)
	twice, err := s.ListIngredients(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("second run changed the catalog:\nonce  %+v\ntwice %+v", once, twice)
	}
	if first.Filled == 0 {
		t.Errorf("first run filled nothing: %+v", first)
	}
	if second.Filled != 0 {
		t.Errorf("second run Filled = %d, want 0", second.Filled)
	}
	if !reflect.DeepEqual(first.Offers, second.Offers) {
		t.Errorf("offers differ between runs")
	}
}

func TestImportOffersUnmatchedWithoutCreating(t *testing.T) {
	s, d := newStore(t)
	create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy"})
	var before int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ingredients`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	rep := run(t, s)

	var after int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ingredients`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("ingredient rows %d -> %d; the import must not create ingredients", before, after)
	}
	var quinoa *Offer
	for i := range rep.Offers {
		if rep.Offers[i].Name == "quinoa" {
			quinoa = &rep.Offers[i]
		}
	}
	if quinoa == nil {
		t.Fatalf("no offer for quinoa in %+v", rep.Offers)
	}
	if quinoa.FDCID != 170004 || quinoa.Description != "Quinoa, cooked" {
		t.Errorf("quinoa offer = %+v", *quinoa)
	}
	approx(t, "quinoa offer density", quinoa.DensityGPerML, 185/236.5882365)
	if rep.Unmatched != len(rep.Offers) {
		t.Errorf("Unmatched = %d, offers = %d", rep.Unmatched, len(rep.Offers))
	}
	for i := 1; i < len(rep.Offers); i++ {
		if rep.Offers[i-1].Name >= rep.Offers[i].Name {
			t.Errorf("offers not sorted by name: %q then %q", rep.Offers[i-1].Name, rep.Offers[i].Name)
		}
	}
}

func TestImportSkipsBrandedAndUnusablePortions(t *testing.T) {
	s, _ := newStore(t)
	cheese := create(t, s, catalog.Ingredient{Name: "cheese", Aisle: "dairy"})
	milk := create(t, s, catalog.Ingredient{Name: "milk", Aisle: "dairy"})

	rep := run(t, s)

	// "cup, shredded" and "slice" are not plain volumes; the link is still made.
	c := get(t, s, cheese.ID)
	if c.DensityGPerML != nil {
		t.Errorf("cheese density = %v, want nil: no plain volume portion", *c.DensityGPerML)
	}
	if c.USDAID == nil || *c.USDAID != 170006 {
		t.Errorf("cheese USDAID = %v, want 170006", c.USDAID)
	}
	// A survey food has no amount, so no density, but it links.
	m := get(t, s, milk.ID)
	if m.DensityGPerML != nil || m.USDAID == nil || *m.USDAID != 170007 {
		t.Errorf("milk = %+v, want link only", m)
	}
	// 6 non-branded foods; the branded BUTTER row is not counted or offered.
	if rep.Foods != 6 {
		t.Errorf("Foods = %d, want 6", rep.Foods)
	}
	for _, o := range rep.Offers {
		if o.FDCID == 170005 {
			t.Errorf("branded food was offered: %+v", o)
		}
	}
}

func TestImportPrefersFoodWithDensityThenLowestID(t *testing.T) {
	s, _ := newStore(t)
	flour := create(t, s, catalog.Ingredient{Name: "flour", Aisle: "baking"})
	fsys := fstest.MapFS{
		"measure_unit.csv": {Data: []byte("id,name\n1000,cup\n")},
		"food.csv": {Data: []byte("fdc_id,data_type,description\n" +
			"30,sr_legacy_food,\"Flour, rye\"\n" +
			"20,sr_legacy_food,\"Flour, corn\"\n" +
			"10,sr_legacy_food,\"Flour, oat\"\n")},
		"food_portion.csv": {Data: []byte("fdc_id,seq_num,amount,measure_unit_id,portion_description,modifier,gram_weight\n" +
			"30,1,1,1000,,,100\n20,1,1,1000,,,120\n")},
	}
	if _, err := Import(context.Background(), s, fsys); err != nil {
		t.Fatal(err)
	}
	got := get(t, s, flour.ID)
	if got.USDAID == nil || *got.USDAID != 20 {
		t.Errorf("USDAID = %v, want 20 (lowest id among foods with a density)", got.USDAID)
	}
}

func TestImportFailsLoudlyOnMalformedInput(t *testing.T) {
	const (
		units = "id,name\n1000,cup\n"
		foods = "fdc_id,data_type,description\n1,sr_legacy_food,Butter\n"
		ports = "fdc_id,seq_num,amount,measure_unit_id,portion_description,modifier,gram_weight\n"
	)
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string
	}{
		{"bad gram weight", map[string]string{"food_portion.csv": ports + "1,1,1,1000,,,heavy\n"}, "food_portion.csv line 2"},
		{"bad amount", map[string]string{"food_portion.csv": ports + "1,1,lots,1000,,,5\n"}, "food_portion.csv line 2"},
		{"bad seq", map[string]string{"food_portion.csv": ports + "1,x,1,1000,,,5\n"}, "seq_num"},
		{"bad portion fdc id", map[string]string{"food_portion.csv": ports + "abc,1,1,1000,,,5\n"}, "food_portion.csv line 2"},
		{"bad food id", map[string]string{"food.csv": "fdc_id,data_type,description\nxyz,sr_legacy_food,Butter\n"}, "food.csv line 2"},
		{"empty description", map[string]string{"food.csv": "fdc_id,data_type,description\n7,sr_legacy_food,\n"}, "has no description"},
		{"missing column", map[string]string{"food.csv": "fdc_id,description\n1,Butter\n"}, `missing required column "data_type"`},
		{"ragged row", map[string]string{"food.csv": "fdc_id,data_type,description\n1,sr_legacy_food\n"}, "food.csv"},
		{"missing file", map[string]string{"food.csv": ""}, "food.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newStore(t)
			butter := create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy"})
			fsys := fstest.MapFS{
				"measure_unit.csv": {Data: []byte(units)},
				"food.csv":         {Data: []byte(foods)},
				"food_portion.csv": {Data: []byte(ports)},
			}
			for name, data := range tc.files {
				fsys[name] = &fstest.MapFile{Data: []byte(data)}
			}
			if tc.name == "missing file" {
				delete(fsys, "food.csv")
			}
			_, err := Import(context.Background(), s, fsys)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
			if got := get(t, s, butter.ID); got.USDAID != nil || got.DensityGPerML != nil {
				t.Errorf("a failed import modified the catalog: %+v", got)
			}
		})
	}
}

func TestImportMissingMeasureUnitFile(t *testing.T) {
	s, _ := newStore(t)
	_, err := Import(context.Background(), s, fstest.MapFS{})
	if err == nil || !strings.Contains(err.Error(), "measure_unit.csv") {
		t.Fatalf("err = %v", err)
	}
}

type failingCatalog struct{ matchErr, updateErr error }

func (f failingCatalog) MatchIngredient(context.Context, string) (catalog.Ingredient, bool, error) {
	return catalog.Ingredient{}, false, f.matchErr
}

func (f failingCatalog) GetIngredient(context.Context, int64) (catalog.Ingredient, error) {
	return catalog.Ingredient{}, f.matchErr
}

func (f failingCatalog) UpdateIngredient(context.Context, catalog.Ingredient) error {
	return f.updateErr
}

func TestImportReportsCatalogFailure(t *testing.T) {
	_, err := Import(context.Background(), failingCatalog{matchErr: os.ErrInvalid}, os.DirFS("testdata/fdc"))
	if err == nil || !strings.Contains(err.Error(), "match food") {
		t.Fatalf("err = %v, want a wrapped match failure", err)
	}
}

type updateFailCatalog struct {
	*catalog.Store
}

func (updateFailCatalog) UpdateIngredient(context.Context, catalog.Ingredient) error {
	return os.ErrPermission
}

func TestImportReportsUpdateFailure(t *testing.T) {
	s, _ := newStore(t)
	create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy"})
	_, err := Import(context.Background(), updateFailCatalog{s}, os.DirFS("testdata/fdc"))
	if err == nil || !strings.Contains(err.Error(), `fill ingredient "butter"`) {
		t.Fatalf("err = %v", err)
	}
}

// editAfterMatch lets a test change the stored ingredient after the import has
// matched it but before it writes, as a human using the catalog page would.
type editAfterMatch struct {
	*catalog.Store
	edit func()
}

func (e *editAfterMatch) MatchIngredient(ctx context.Context, term string) (catalog.Ingredient, bool, error) {
	ing, ok, err := e.Store.MatchIngredient(ctx, term)
	if ok && e.edit != nil {
		e.edit()
		e.edit = nil
	}
	return ing, ok, err
}

func TestImportKeepsEditsMadeDuringTheRun(t *testing.T) {
	s, _ := newStore(t)
	butter := create(t, s, catalog.Ingredient{Name: "butter", Aisle: "dairy"})
	cat := &editAfterMatch{Store: s, edit: func() {
		edited := butter
		edited.Aisle = "chilled"
		edited.Aliases = []string{"sweet butter"}
		edited.Reviewed = true
		edited.DensityGPerML = f64(0.91) // set by hand mid-run
		if err := s.UpdateIngredient(context.Background(), edited); err != nil {
			t.Errorf("edit during run: %v", err)
		}
	}}

	if _, err := Import(context.Background(), cat, os.DirFS("testdata/fdc")); err != nil {
		t.Fatal(err)
	}

	got := get(t, s, butter.ID)
	if got.Aisle != "chilled" || !got.Reviewed || !reflect.DeepEqual(got.Aliases, []string{"sweet butter"}) {
		t.Errorf("edit made during the run was reverted: %+v", got)
	}
	approx(t, "hand-set density", got.DensityGPerML, 0.91)
	if got.USDAID == nil || *got.USDAID != 170001 {
		t.Errorf("USDAID = %v, want 170001", got.USDAID)
	}
}
