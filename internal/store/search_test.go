package store

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func names(ss []Summary) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

func mustSearch(t *testing.T, s *Store, text string) []Summary {
	t.Helper()
	got, err := s.Search(authedCtx(t), SearchQuery{Text: text})
	if err != nil {
		t.Fatalf("Search(%q): %v", text, err)
	}
	return got
}

func wantNames(t *testing.T, got []Summary, want ...string) {
	t.Helper()
	g := names(got)
	if len(g) != len(want) {
		t.Fatalf("got %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("got %v, want %v", g, want)
		}
	}
}

func TestSearchMatchesEachIndexedField(t *testing.T) {
	ctx := authedCtx(t)
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)

	r := fullRecipe(t, ids) // name Pancakes, description Fluffy., tags breakfast, quick
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	other := simpleRecipe("Soup")
	other.Description = "Warming broth"
	if _, err := s.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	id, err := s.Create(ctx, simpleRecipe("Toast"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "Burns easily under the grill"); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, text, want string }{
		{"name", "pancakes", "Pancakes"},
		{"description", "broth", "Soup"},
		{"ingredient catalog name", "zz test butter", "Pancakes"},
		{"ingredient line as written", "sifted", "Pancakes"},
		{"tag", "breakfast", "Pancakes"},
		{"note", "grill", "Toast"},
		{"prefix while typing", "panc", "Pancakes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantNames(t, mustSearch(t, s, c.text), c.want)
		})
	}
}

func TestSearchNoMatchIsEmptyNotAnError(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	if _, err := s.Create(ctx, simpleRecipe("Soup")); err != nil {
		t.Fatal(err)
	}
	got := mustSearch(t, s, "zebra")
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

func TestSearchMultiWordRequiresEveryWord(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	a := simpleRecipe("Lemon Cake")
	b := simpleRecipe("Lemon Soup")
	c := simpleRecipe("Carrot Cake")
	for _, r := range []recipe.Recipe{a, b, c} {
		if _, err := s.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	wantNames(t, mustSearch(t, s, "lemon cake"), "Lemon Cake")
	wantNames(t, mustSearch(t, s, "cake lemon"), "Lemon Cake")
}

func TestSearchRanksNameAboveNotes(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	noted, err := s.Create(ctx, simpleRecipe("Aaa Plain"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, noted, "tastes like pie"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, simpleRecipe("Zzz Pie")); err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "pie"), "Zzz Pie", "Aaa Plain")
}

func TestSearchSyntaxCharactersAreNotErrors(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	if _, err := s.Create(ctx, simpleRecipe("Mac and Cheese")); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		`"`, `""`, `"cheese`, `cheese"`, `*`, `cheese*`, `AND`, `OR`, `NOT`, `NEAR(a b)`, `name:cheese`,
		`mac AND`, `(`, `)`, `-cheese`, `^mac`, `a+b`, `don't`, `;DROP TABLE recipes;--`, `\`, `%`, `é`, `{`, `:`,
	} {
		if _, err := s.Search(ctx, SearchQuery{Text: text}); err != nil {
			t.Errorf("Search(%q): %v", text, err)
		}
	}
	wantNames(t, mustSearch(t, s, `"cheese"`), "Mac and Cheese")
	wantNames(t, mustSearch(t, s, `mac AND cheese`), "Mac and Cheese")
	// Punctuation alone has no word to find.
	wantNames(t, mustSearch(t, s, `!!!`))
}

func TestSearchBlankListsEverythingByName(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	for _, n := range []string{"banana", "Apple", "cherry"} {
		if _, err := s.Create(ctx, simpleRecipe(n)); err != nil {
			t.Fatal(err)
		}
	}
	wantNames(t, mustSearch(t, s, "  "), "Apple", "banana", "cherry")
}

func TestSearchSummaryCarriesListFields(t *testing.T) {
	ctx := authedCtx(t)
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	if _, err := s.Create(ctx, fullRecipe(t, ids)); err != nil {
		t.Fatal(err)
	}
	got := mustSearch(t, s, "")
	if len(got) != 1 {
		t.Fatalf("got %d summaries", len(got))
	}
	g := got[0]
	if g.Name != "Pancakes" || g.Rating == nil || *g.Rating != 4 {
		t.Fatalf("name/rating: %+v", g)
	}
	if len(g.Tags) != 2 || g.Tags[0] != "breakfast" || g.Tags[1] != "quick" {
		t.Fatalf("tags: %v", g.Tags)
	}
	if g.Prep.Minutes() != 10 || g.Cook.Minutes() != 20 || g.Total.Minutes() != 45 {
		t.Fatalf("times: prep %v cook %v total %v", g.Prep, g.Cook, g.Total)
	}
}

func TestSearchTotalFallsBackToPrepPlusCook(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	r := simpleRecipe("Quick")
	r.Times.Prep, r.Times.Cook = 5*time.Minute, 10*time.Minute
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	if got := mustSearch(t, s, "")[0].Total.Minutes(); got != 15 {
		t.Fatalf("total = %v minutes, want 15", got)
	}
}

func TestIndexFollowsCreateEditNoteRestoreCloneDelete(t *testing.T) {
	ctx := authedCtx(t)
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)

	r := fullRecipe(t, ids)
	id, err := s.Create(ctx, r) // version 1: Pancakes, flour, tags breakfast quick
	if err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "pancakes"), "Pancakes")

	// Edit: new name, no tags, one line with a different ingredient.
	edited := simpleRecipe("Waffles")
	edited.Components[0].Lines[0].Raw = "batter mix"
	if err := s.Update(ctx, id, edited); err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "pancakes"))
	wantNames(t, mustSearch(t, s, "breakfast"))
	wantNames(t, mustSearch(t, s, "sifted"))
	wantNames(t, mustSearch(t, s, "waffles"), "Waffles")
	wantNames(t, mustSearch(t, s, "batter"), "Waffles")

	// Note.
	if _, err := s.AddNote(ctx, id, "serve with maple syrup"); err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "maple"), "Waffles")

	// Restore version 1 brings the old fields back and keeps the note.
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "waffles"))
	wantNames(t, mustSearch(t, s, "pancakes"), "Pancakes")
	wantNames(t, mustSearch(t, s, "sifted"), "Pancakes")
	wantNames(t, mustSearch(t, s, "maple"), "Pancakes")

	// Clone is indexed as its own recipe.
	cid, err := s.Clone(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustSearch(t, s, "pancakes"); len(got) != 2 {
		t.Fatalf("after clone: %v", names(got))
	}

	// Delete removes the row; the clone stays.
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	got := mustSearch(t, s, "pancakes")
	if len(got) != 1 || got[0].ID != cid {
		t.Fatalf("after delete: %v", got)
	}
	if n := count(t, d, `SELECT count(*) FROM recipe_search`); n != 1 {
		t.Fatalf("index rows after delete = %d, want 1", n)
	}
}

func TestIndexFollowsCatalogRename(t *testing.T) {
	ctx := authedCtx(t)
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	if _, err := s.Create(ctx, fullRecipe(t, ids)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE ingredients SET name = 'zz test spelt' WHERE id = ?`, ids.flour); err != nil {
		t.Fatal(err)
	}
	wantNames(t, mustSearch(t, s, "spelt"), "Pancakes")
	if n := count(t, d, `SELECT count(*) FROM recipe_search WHERE ingredients LIKE '%zz test flour%'`); n != 0 {
		t.Fatalf("old ingredient name still indexed")
	}
}

// migrationsBelow returns the embedded migrations numbered below prefix, as
// the database stood before that migration. Migrations above it are left out
// too: applying a later one first would put prefix out of order, and goose
// refuses that. The README sorts after the numbered files, so it is left out.
func migrationsBelow(t *testing.T, prefix string) fstest.MapFS {
	t.Helper()
	all := db.Migrations()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() >= prefix || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

// TestMigrationIndexesExistingRecipes applies the schema without the search
// migration, writes recipes, then applies it: rows that predate it are found.
// The recipes are written with SQL because the store writes the current shape,
// and the household they end up in is created before the migration that needs
// one (00008).
func TestMigrationIndexesExistingRecipes(t *testing.T) {
	ctx := authedCtx(t)
	d := openRaw(t)
	if err := db.Migrate(ctx, d, migrationsBelow(t, "00004")); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO recipes (id, name, created_at, created_by, updated_at, updated_by) VALUES (1, 'Old Stew', 't', 'a', 't', 'a')`,
		`INSERT INTO recipe_tags (recipe_id, position, tag) VALUES (1, 0, 'winter')`,
		`INSERT INTO notes (recipe_id, body, created_at, created_by) VALUES (1, 'needs thyme', 't', 'a')`,
	} {
		if _, err := d.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx, d, migrationsBelow(t, "00008")); err != nil {
		t.Fatalf("apply search migration over existing data: %v", err)
	}
	if _, err := household.New(d).Create(ctx, "Home", auth.DevEmail); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("apply household migration over existing data: %v", err)
	}
	s := New(d)
	for _, text := range []string{"stew", "winter", "thyme"} {
		wantNames(t, mustSearch(t, s, text), "Old Stew")
	}
}

// openRaw opens a database file with no migrations applied.
func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(context.Background(), config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}
