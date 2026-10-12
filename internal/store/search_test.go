package store

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func names(ss []Summary) []string {
	out := []string{}
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

func mustSearch(t *testing.T, s *Store, text string, tags ...string) []Summary {
	t.Helper()
	got, err := s.Search(context.Background(), SearchQuery{Text: text, Tags: tags})
	if err != nil {
		t.Fatalf("Search(%q, %v): %v", text, tags, err)
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

func TestTagFilter(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	mk := func(name string, tags ...string) {
		r := simpleRecipe(name)
		r.Tags = tags
		if _, err := s.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	mk("A", "dinner", "quick")
	mk("B", "dinner", "vegan")
	mk("C", "dessert", "quick")
	mk("D")

	wantNames(t, mustSearch(t, s, "", "dinner"), "A", "B")
	wantNames(t, mustSearch(t, s, "", "dinner", "quick"), "A")
	wantNames(t, mustSearch(t, s, "", "dinner", "dinner"), "A", "B")
	wantNames(t, mustSearch(t, s, "", "nothing"))
	wantNames(t, mustSearch(t, s, "a", "quick"), "A")
	wantNames(t, mustSearch(t, s, "c", "dinner"))

	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"dessert", 1}, {"dinner", 2}, {"quick", 2}, {"vegan", 1}}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags = %v, want %v", tags, want)
		}
	}
}

func TestTagsEmptyLibrary(t *testing.T) {
	s, _ := newTestStore(t)
	tags, err := s.Tags(context.Background())
	if err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("Tags = %#v, %v", tags, err)
	}
}

// TestMigrationIndexesExistingRecipes applies the schema without the search
// migration, writes recipes, then applies it: rows that predate it are found.
func TestMigrationIndexesExistingRecipes(t *testing.T) {
	ctx := authedCtx(t)
	d := openRaw(t)
	all := db.Migrations()
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		// Leave out 00004 and everything above it: a later migration applied
		// first would put 00004 out of order, and goose refuses that. The
		// README sorts after the numbered files, so it is left out too.
		if e.Name() >= "00004" {
			continue
		}
		b, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		before[e.Name()] = &fstest.MapFile{Data: b}
	}
	if err := db.Migrate(ctx, d, before); err != nil {
		t.Fatal(err)
	}
	s := New(d)
	r := simpleRecipe("Old Stew")
	r.Tags = []string{"winter"}
	id, err := s.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "needs thyme"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d, all); err != nil {
		t.Fatalf("apply search migration over existing data: %v", err)
	}
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
