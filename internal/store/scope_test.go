package store

import (
	"context"
	"errors"
	"testing"
)

// Household A owns one recipe with a note, a made-it entry, a snapshot and a
// revision. Every test below acts as household B (otherCtx) against it and
// asserts that nothing of A's is found, changed or leaked (ADR-00018).
type scopeFixture struct {
	s       *Store
	a, b    context.Context
	id      int64 // A's recipe, at version 2
	snapKnd SnapshotKind
}

func newScopeFixture(t *testing.T) scopeFixture {
	t.Helper()
	s, _ := newTestStore(t)
	f := scopeFixture{s: s, a: authedCtx(t), b: otherCtx(t), snapKnd: SnapshotText}
	id, err := s.CreateWithSnapshots(f.a, simpleRecipe("Secret Stew"), SnapshotInput{Kind: f.snapKnd, Content: []byte("source")})
	if err != nil {
		t.Fatal(err)
	}
	r := simpleRecipe("Secret Stew")
	r.Tags = []string{"private"}
	if err := s.Update(f.a, id, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(f.a, id, "grandma's secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(f.a, id, MadeIt{Comment: "great"}); err != nil {
		t.Fatal(err)
	}
	f.id = id
	return f
}

// unchanged fails if household A's recipe is not exactly as the fixture left it.
func (f scopeFixture) unchanged(t *testing.T) {
	t.Helper()
	info, err := f.s.Info(f.a, f.id)
	if err != nil {
		t.Fatalf("A's recipe: %v", err)
	}
	if info.Version != 2 {
		t.Errorf("A's recipe is at version %d, want 2", info.Version)
	}
	notes, err := f.s.ListNotes(f.a, f.id)
	if err != nil || len(notes) != 1 {
		t.Errorf("A's notes = %d (err %v), want 1", len(notes), err)
	}
	madeIt, err := f.s.ListMadeIt(f.a, f.id)
	if err != nil || len(madeIt) != 1 {
		t.Errorf("A's made-it log = %d (err %v), want 1", len(madeIt), err)
	}
	kinds, err := f.s.ListSnapshotKinds(f.a, f.id)
	if err != nil || len(kinds) != 1 {
		t.Errorf("A's snapshots = %d (err %v), want 1", len(kinds), err)
	}
	revs, err := f.s.ListRevisions(f.a, f.id)
	if err != nil || len(revs) != 1 {
		t.Errorf("A's revisions = %d (err %v), want 1", len(revs), err)
	}
	all, err := f.s.Search(f.a, SearchQuery{})
	if err != nil || len(all) != 1 {
		t.Errorf("A's recipes = %d (err %v), want 1", len(all), err)
	}
}

func TestOtherHouseholdCannotReadARecipe(t *testing.T) {
	f := newScopeFixture(t)
	reads := map[string]func() error{
		"Get":               func() error { _, err := f.s.Get(f.b, f.id); return err },
		"Info":              func() error { _, err := f.s.Info(f.b, f.id); return err },
		"GetVersion live":   func() error { _, err := f.s.GetVersion(f.b, f.id, 2); return err },
		"GetVersion old":    func() error { _, err := f.s.GetVersion(f.b, f.id, 1); return err },
		"ListRevisions":     func() error { _, err := f.s.ListRevisions(f.b, f.id); return err },
		"ListNotes":         func() error { _, err := f.s.ListNotes(f.b, f.id); return err },
		"ListMadeIt":        func() error { _, err := f.s.ListMadeIt(f.b, f.id); return err },
		"GetSnapshot":       func() error { _, err := f.s.GetSnapshot(f.b, f.id, f.snapKnd); return err },
		"ListSnapshotKinds": func() error { _, err := f.s.ListSnapshotKinds(f.b, f.id); return err },
	}
	for name, read := range reads {
		if err := read(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s as the other household = %v, want ErrNotFound", name, err)
		}
	}
	f.unchanged(t)
}

func TestOtherHouseholdCannotChangeARecipe(t *testing.T) {
	f := newScopeFixture(t)
	writes := map[string]func() error{
		"Update":          func() error { return f.s.Update(f.b, f.id, simpleRecipe("Hijacked")) },
		"UpdateIfVersion": func() error { return f.s.UpdateIfVersion(f.b, f.id, 2, simpleRecipe("Hijacked")) },
		"Delete":          func() error { return f.s.Delete(f.b, f.id) },
		"Restore":         func() error { return f.s.Restore(f.b, f.id, 1) },
		"Clone":           func() error { _, err := f.s.Clone(f.b, f.id); return err },
		"AddNote":         func() error { _, err := f.s.AddNote(f.b, f.id, "injected"); return err },
		"AddMadeIt":       func() error { _, err := f.s.AddMadeIt(f.b, f.id, MadeIt{Comment: "injected"}); return err },
		"AddSnapshot":     func() error { return f.s.AddSnapshot(f.b, f.id, SnapshotHTML, []byte("injected")) },
	}
	for name, write := range writes {
		if err := write(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s as the other household = %v, want ErrNotFound", name, err)
		}
	}
	f.unchanged(t)
	got, err := f.s.Get(f.a, f.id)
	if err != nil || got.Name != "Secret Stew" {
		t.Errorf("A's recipe = %q (err %v), want it untouched", got.Name, err)
	}
	if mine, err := f.s.Search(f.b, SearchQuery{}); err != nil || len(mine) != 0 {
		t.Errorf("B's recipes after refused writes = %d (err %v), want 0 (a clone must not land in B)", len(mine), err)
	}
}

func TestRecipeStaysWithItsHouseholdOnCloneAndRestore(t *testing.T) {
	f := newScopeFixture(t)
	cloneID, err := f.s.Clone(f.a, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Get(f.b, cloneID); !errors.Is(err, ErrNotFound) {
		t.Errorf("B reads A's clone = %v, want ErrNotFound", err)
	}
	info, err := f.s.Info(f.a, cloneID)
	if err != nil || info.ClonedFrom == nil || info.ClonedFrom.RecipeID != f.id {
		t.Errorf("A's clone = %+v (err %v), want it to point at the original", info, err)
	}
	if err := f.s.Restore(f.a, f.id, 1); err != nil {
		t.Fatalf("A restores its own recipe: %v", err)
	}
	if _, err := f.s.Get(f.b, f.id); !errors.Is(err, ErrNotFound) {
		t.Errorf("B reads A's restored recipe = %v, want ErrNotFound", err)
	}
}

func TestSearchNeverCrossesHouseholds(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	mk := func(ctx context.Context, name, tag, note string) {
		t.Helper()
		r := simpleRecipe(name)
		r.Tags = []string{tag}
		id, err := s.Create(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddNote(ctx, id, note); err != nil {
			t.Fatal(err)
		}
	}
	mk(a, "Lemon Cake of A", "shared-tag", "zesty")
	mk(b, "Lemon Pie of B", "shared-tag", "zesty")

	search := func(ctx context.Context, text string, tags ...string) []string {
		t.Helper()
		got, err := s.Search(ctx, SearchQuery{Text: text, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
		return names(got)
	}
	for _, c := range []struct {
		name string
		text string
		tags []string
	}{
		{"by name", "lemon", nil},
		{"by note", "zesty", nil},
		{"by tag", "", []string{"shared-tag"}},
		{"by text and tag", "lemon", []string{"shared-tag"}},
		{"everything", "", nil},
	} {
		if got := search(a, c.text, c.tags...); len(got) != 1 || got[0] != "Lemon Cake of A" {
			t.Errorf("A searching %s = %v, want only its own", c.name, got)
		}
		if got := search(b, c.text, c.tags...); len(got) != 1 || got[0] != "Lemon Pie of B" {
			t.Errorf("B searching %s = %v, want only its own", c.name, got)
		}
	}
}

func TestNoHouseholdOnContextFailsEveryCall(t *testing.T) {
	s, _ := newTestStore(t)
	id, err := s.Create(authedCtx(t), simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	bare := noHouseholdCtx(t) // a verified identity, no household
	calls := map[string]func() error{
		"Create":            func() error { _, err := s.Create(bare, simpleRecipe("y")); return err },
		"CreateWithSnaps":   func() error { _, err := s.CreateWithSnapshots(bare, simpleRecipe("y")); return err },
		"Get":               func() error { _, err := s.Get(bare, id); return err },
		"Info":              func() error { _, err := s.Info(bare, id); return err },
		"Update":            func() error { return s.Update(bare, id, simpleRecipe("y")) },
		"UpdateIfVersion":   func() error { return s.UpdateIfVersion(bare, id, 1, simpleRecipe("y")) },
		"Delete":            func() error { return s.Delete(bare, id) },
		"Clone":             func() error { _, err := s.Clone(bare, id); return err },
		"Restore":           func() error { return s.Restore(bare, id, 1) },
		"GetVersion":        func() error { _, err := s.GetVersion(bare, id, 1); return err },
		"ListRevisions":     func() error { _, err := s.ListRevisions(bare, id); return err },
		"AddNote":           func() error { _, err := s.AddNote(bare, id, "n"); return err },
		"ListNotes":         func() error { _, err := s.ListNotes(bare, id); return err },
		"AddMadeIt":         func() error { _, err := s.AddMadeIt(bare, id, MadeIt{}); return err },
		"ListMadeIt":        func() error { _, err := s.ListMadeIt(bare, id); return err },
		"AddSnapshot":       func() error { return s.AddSnapshot(bare, id, SnapshotText, []byte("t")) },
		"GetSnapshot":       func() error { _, err := s.GetSnapshot(bare, id, SnapshotText); return err },
		"ListSnapshotKinds": func() error { _, err := s.ListSnapshotKinds(bare, id); return err },
		"Search":            func() error { _, err := s.Search(bare, SearchQuery{}); return err },
		"Search text":       func() error { _, err := s.Search(bare, SearchQuery{Text: "x"}); return err },
		"Search punct only": func() error { _, err := s.Search(bare, SearchQuery{Text: "!!!"}); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrNoHousehold) {
			t.Errorf("%s with no household = %v, want ErrNoHousehold", name, err)
		}
	}
}

func TestDeletingAHouseholdRemovesItsRecipesAndSearchRows(t *testing.T) {
	s, d := newTestStore(t)
	if _, err := s.Create(otherCtx(t), simpleRecipe("B's")); err != nil {
		t.Fatal(err)
	}
	idA, err := s.CreateWithSnapshots(authedCtx(t), simpleRecipe("A's"), SnapshotInput{Kind: SnapshotText, Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(authedCtx(t), idA, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM households WHERE id = ?`, testHouseholdID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"recipes", "components", "ingredient_lines", "steps", "notes", "source_snapshots", "recipe_search"} {
		if n := count(t, d, `SELECT COUNT(*) FROM `+table+` WHERE household_id = ?`, testHouseholdID); n != 0 {
			t.Errorf("%s still holds %d rows of the deleted household", table, n)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes WHERE household_id = ?`, otherHouseholdID); n != 1 {
		t.Errorf("the other household has %d recipes, want 1", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipe_search`); n != 1 {
		t.Errorf("recipe_search holds %d rows, want only the other household's", n)
	}
}
