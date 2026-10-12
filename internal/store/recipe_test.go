package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestCreateGetRoundTrip(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)

	want := fullRecipe(t, ids)
	id, err := s.Create(ctx, want)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	sameRecipe(t, want, got)

	// Spot-check what the JSON comparison could hide: exact values, ranges
	// and raw text survive as written.
	l := got.Components[0].Lines
	if got := l[0].Amount.Qty.String(); got != "1 1/3" {
		t.Errorf("fractional quantity = %q, want 1 1/3", got)
	}
	if !l[2].Amount.Qty.IsRange() || l[2].Amount.Qty.Min().String() != "50" || l[2].Amount.Qty.Max().String() != "60" {
		t.Errorf("range quantity = %v", l[2].Amount.Qty)
	}
	if l[0].Raw != "1 1/3 cups flour, sifted" {
		t.Errorf("raw = %q", l[0].Raw)
	}
	if !l[3].Amount.Qty.IsAbsent() || !l[3].Optional || l[3].Rule != quantity.RuleToTaste {
		t.Errorf("to-taste line = %+v", l[3])
	}
	if !got.Components[1].Lines[0].Amount.HasPackage || got.Components[1].Lines[0].Ingredient != 0 {
		t.Errorf("packaged unresolved line = %+v", got.Components[1].Lines[0])
	}

	info, err := s.Info(ctx, id)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version != 1 || info.CreatedBy != auth.DevEmail || info.UpdatedBy != auth.DevEmail || info.ClonedFrom != nil {
		t.Errorf("Info = %+v", info)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions`); n != 0 {
		t.Errorf("create wrote %d revisions, want 0", n)
	}
}

func TestUpdateReplacesDocument(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}

	next := simpleRecipe("Plain")
	next.Tags = []string{"x"}
	if err := s.Update(ctx, id, next); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, next, got)
	for _, table := range []string{"recipe_tags", "recipe_special_times", "recipe_equipment", "components", "ingredient_lines", "steps"} {
		var want int
		switch table {
		case "recipe_tags", "components", "ingredient_lines", "steps":
			want = 1
		}
		if n := count(t, d, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s has %d rows after update, want %d (old children must be replaced)", table, n, want)
		}
	}
}

func TestStepCycleIsRejectedAndWritesNothing(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)

	cyclic := simpleRecipe("Loop")
	cyclic.Components[0].Steps = []recipe.Step{
		{ID: "a", Text: "A", DependsOn: []recipe.StepID{"b"}},
		{ID: "b", Text: "B", DependsOn: []recipe.StepID{"a"}},
	}

	_, err := s.Create(ctx, cyclic)
	var ce *recipe.CycleError
	if !errors.As(err, &ce) || !errors.Is(err, recipe.ErrCycle) {
		t.Fatalf("Create error = %v, want a cycle error", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "(a)") || !strings.Contains(msg, "(b)") {
		t.Errorf("message %q does not name both steps", msg)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("failed create left %d recipes", n)
	}

	id, err := s.Create(ctx, simpleRecipe("Fine"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, cyclic); !errors.Is(err, recipe.ErrCycle) {
		t.Fatalf("Update error = %v, want a cycle error", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions`); n != 0 {
		t.Errorf("rejected update wrote %d revisions", n)
	}
	got, err := s.Get(ctx, id)
	if err != nil || got.Name != "Fine" {
		t.Errorf("recipe after rejected update = %q, %v", got.Name, err)
	}
}

func TestUpdateWritesOneRevisionPerEdit(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	v1 := simpleRecipe("v1")
	id, err := s.Create(ctx, v1)
	if err != nil {
		t.Fatal(err)
	}

	const edits = 4
	docs := []recipe.Recipe{v1}
	for i := 2; i <= edits+1; i++ {
		next := simpleRecipe("v" + string(rune('0'+i)))
		if err := s.Update(ctx, id, next); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
		docs = append(docs, next)
	}

	revs, err := s.ListRevisions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != edits {
		t.Fatalf("revisions = %d, want %d", len(revs), edits)
	}
	var prev time.Time
	for i, r := range revs {
		if r.Version != int64(i+1) || r.Author != auth.DevEmail || r.Restored != nil {
			t.Errorf("revision %d = %+v", i, r)
		}
		if !r.CreatedAt.After(prev) {
			t.Errorf("revision %d time %v not after %v", i, r.CreatedAt, prev)
		}
		prev = r.CreatedAt
		// Each revision holds the document as it was before the edit.
		got, err := s.GetVersion(ctx, id, r.Version)
		if err != nil {
			t.Fatalf("GetVersion(%d): %v", r.Version, err)
		}
		sameRecipe(t, docs[i], got)
	}
	info, err := s.Info(ctx, id)
	if err != nil || info.Version != edits+1 {
		t.Errorf("Info = %+v, %v; want version %d", info, err, edits+1)
	}
	cur, err := s.GetVersion(ctx, id, edits+1)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, docs[edits], cur)
	if n := count(t, d, `SELECT COUNT(*) FROM revisions`); n != edits {
		t.Errorf("revision rows = %d", n)
	}
}

func TestRevisionAndUpdateShareATransaction(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("keep"))
	if err != nil {
		t.Fatal(err)
	}

	// The new document names an ingredient that does not exist, so the write
	// fails after the revision has been inserted.
	bad := fullRecipe(t, ids)
	bad.Components[0].Lines[0].Ingredient = 987654
	err = s.Update(ctx, id, bad)
	if !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("Update error = %v, want ErrUnknownReference", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions`); n != 0 {
		t.Errorf("failed update left %d revisions", n)
	}
	info, _ := s.Info(ctx, id)
	if info.Version != 1 {
		t.Errorf("version = %d after failed update", info.Version)
	}
	if got, _ := s.Get(ctx, id); got.Name != "keep" {
		t.Errorf("name = %q after failed update", got.Name)
	}
}

func TestRestoreIsANewRevisionAndRoundTrips(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)

	original := fullRecipe(t, ids)
	id, err := s.Create(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, simpleRecipe("Edited")); err != nil {
		t.Fatal(err)
	}

	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, original, got)

	revs, err := s.ListRevisions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Fatalf("revisions after restore = %d, want 2", len(revs))
	}
	if revs[0].Restored != nil || revs[1].Restored == nil || *revs[1].Restored != 1 {
		t.Errorf("revisions = %+v; want the second marked as a restore of version 1", revs)
	}
	// The history is intact: the edit's document is still reachable.
	edited, err := s.GetVersion(ctx, id, 2)
	if err != nil || edited.Name != "Edited" {
		t.Errorf("version 2 = %q, %v", edited.Name, err)
	}
	if info, _ := s.Info(ctx, id); info.Version != 3 {
		t.Errorf("version = %d, want 3", info.Version)
	}
}

func TestRestoreUnknownVersion(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(ctx, id, 7); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore(7) = %v, want ErrNotFound", err)
	}
	if err := s.Restore(ctx, id+100, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore of missing recipe = %v, want ErrNotFound", err)
	}
}

func TestCloneIsIndependentAndRecordsOrigin(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)

	orig := fullRecipe(t, ids)
	origID, err := s.Create(ctx, orig)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, origID, orig); err != nil { // version 2
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, origID, "stays with the original"); err != nil {
		t.Fatal(err)
	}

	cloneID, err := s.Clone(ctx, origID)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if cloneID == origID {
		t.Fatal("clone has the original's id")
	}
	info, err := s.Info(ctx, cloneID)
	if err != nil {
		t.Fatal(err)
	}
	if info.ClonedFrom == nil || *info.ClonedFrom != (Origin{RecipeID: origID, Version: 2}) || info.Version != 1 {
		t.Errorf("clone Info = %+v, want origin recipe %d version 2", info, origID)
	}
	got, err := s.Get(ctx, cloneID)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, orig, got)
	if notes, _ := s.ListNotes(ctx, cloneID); len(notes) != 0 {
		t.Errorf("clone inherited %d notes", len(notes))
	}

	if err := s.Update(ctx, cloneID, simpleRecipe("Changed clone")); err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(ctx, origID)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, orig, after)
	if revs, _ := s.ListRevisions(ctx, origID); len(revs) != 1 {
		t.Errorf("editing the clone changed the original's revisions: %d", len(revs))
	}

	// Deleting the origin leaves the clone, minus the pointer.
	if err := s.Delete(ctx, origID); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Info(ctx, cloneID); err != nil || info.ClonedFrom != nil {
		t.Errorf("clone after origin delete = %+v, %v", info, err)
	}
}

func TestDelete(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, simpleRecipe("two")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(ctx, id, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(ctx, id, MadeIt{}); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
	for _, table := range []string{"recipes", "recipe_tags", "recipe_special_times", "components", "ingredient_lines", "line_qualifiers",
		"steps", "step_dependencies", "recipe_equipment", "recipe_equipment_steps", "notes", "made_it", "revisions", "revision_ingredients", "source_snapshots"} {
		if n := count(t, d, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after delete", table, n)
		}
	}
	if err := s.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestNotFoundIsDistinctFromQueryFailure(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)

	if _, err := s.Get(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
	if err := s.Update(ctx, 42, simpleRecipe("x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.Clone(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("Clone(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.Info(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("Info(missing) = %v, want ErrNotFound", err)
	}

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx, 42)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get on a closed database = %v, want a failure that is not ErrNotFound", err)
	}
}

func TestWritesNeedAnIdentity(t *testing.T) {
	s, d := newTestStore(t)
	authed := authedCtx(t)
	id, err := s.Create(authed, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	bare := context.Background()

	checks := map[string]func() error{
		"Create":      func() error { _, err := s.Create(bare, simpleRecipe("y")); return err },
		"Update":      func() error { return s.Update(bare, id, simpleRecipe("y")) },
		"Delete":      func() error { return s.Delete(bare, id) },
		"Restore":     func() error { return s.Restore(bare, id, 1) },
		"Clone":       func() error { _, err := s.Clone(bare, id); return err },
		"AddNote":     func() error { _, err := s.AddNote(bare, id, "n"); return err },
		"AddMadeIt":   func() error { _, err := s.AddMadeIt(bare, id, MadeIt{}); return err },
		"AddSnapshot": func() error { return s.AddSnapshot(bare, id, SnapshotText, []byte("t")) },
	}
	for name, f := range checks {
		if err := f(); !errors.Is(err, ErrNoIdentity) {
			t.Errorf("%s without identity = %v, want ErrNoIdentity", name, err)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 1 {
		t.Errorf("recipes = %d, want 1", n)
	}
}

func TestUnknownCatalogReferencesAreRefused(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)

	cases := map[string]func(r *recipe.Recipe){
		"ingredient": func(r *recipe.Recipe) { r.Components[0].Lines[0].Ingredient = 999999 },
		"equipment":  func(r *recipe.Recipe) { r.Equipment[0].Equipment = 999999 },
		"unit": func(r *recipe.Recipe) {
			r.Components[0].Lines[0].Amount.Unit = quantity.OtherUnit("zz-not-a-unit")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := fullRecipe(t, ids)
			mutate(&r)
			if _, err := s.Create(ctx, r); !errors.Is(err, ErrUnknownReference) {
				t.Fatalf("Create = %v, want ErrUnknownReference", err)
			}
			if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
				t.Errorf("failed create left %d recipes", n)
			}
		})
	}
}

func TestNegativeQuantityIsRefusedOnWrite(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	r := simpleRecipe("neg")
	neg, err := quantity.NewRat(-1, 2)
	if err != nil {
		t.Fatal(err)
	}
	r.Servings = quantity.Exact(neg)
	if _, err := s.Create(ctx, r); !errors.Is(err, ErrInvalid) {
		t.Errorf("Create = %v, want ErrInvalid", err)
	}
}

// Live lines hold a unit id and survive a rename; revision documents must too.
func TestRevisionsSurviveAUnitRename(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, simpleRecipe("edited")); err != nil {
		t.Fatal(err)
	}

	cup, ok, err := c.MatchUnit(ctx, "cup")
	if err != nil || !ok {
		t.Fatalf("MatchUnit(cup) = %v, %v", ok, err)
	}
	cup.Unit.Name = "zz renamed cup"
	if err := c.UpdateUnit(ctx, cup); err != nil {
		t.Fatalf("UpdateUnit: %v", err)
	}

	old, err := s.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatalf("GetVersion after rename: %v", err)
	}
	if got := old.Components[0].Lines[0].Amount.Unit.Name; got != "zz renamed cup" {
		t.Errorf("unit name = %q, want the renamed unit", got)
	}
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatalf("Restore after rename: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Components[0].Lines[0].Amount.Unit.Name != "zz renamed cup" || got.Components[0].Lines[0].Amount.Qty.String() != "1 1/3" {
		t.Errorf("restored line = %+v", got.Components[0].Lines[0])
	}
}

// A dependency on a step that is not there is a broken database, not a
// dependency to attach to position zero. Foreign keys are switched off on the
// single connection to manufacture the orphan.
func TestReadOfInconsistentRowsFailsNamingTheMiss(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	d.SetMaxOpenConns(1)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`DELETE FROM steps WHERE step_key = 'mix'`); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, id)
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "step") {
		t.Fatalf("Get = %v, %+v; want ErrCorrupt naming the missing step", err, got.Components)
	}
}

func TestLookupNamesTheMiss(t *testing.T) {
	m := map[int64]int{7: 1}
	if v, err := lookup(m, 7, "component"); err != nil || v != 1 {
		t.Errorf("lookup hit = %v, %v", v, err)
	}
	_, err := lookup(m, 8, "component")
	if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "component 8") {
		t.Errorf("lookup miss = %v, want ErrCorrupt naming component 8", err)
	}
}

// A multi-query read must see one snapshot: a write that lands between its
// queries is not visible to the later ones.
func TestReadsSeeOneSnapshot(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	notes := func(q queryer) int {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM notes WHERE recipe_id = ?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	err = s.read(ctx, func(q queryer) error {
		before := notes(q)
		if _, err := s.AddNote(ctx, id, "lands mid-read"); err != nil {
			t.Fatal(err)
		}
		if after := notes(q); after != before {
			t.Errorf("a write between two queries of one read was visible: %d then %d", before, after)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListNotes(ctx, id); len(got) != 1 {
		t.Errorf("notes after the read = %d, want 1", len(got))
	}
}
