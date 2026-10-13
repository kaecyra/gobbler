package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"

	"github.com/kaecyra/gobbler/internal/household"
)

// twoHouseholds returns a store over a fresh database and a context for each
// of two households, plus the bare context of no household.
func twoHouseholds(t *testing.T) (*Store, *sql.DB, context.Context, context.Context) {
	t.Helper()
	d := newTestDB(t)
	return New(d), d, householdCtx(t, d, "alpha"), householdCtx(t, d, "beta")
}

func TestOverlayEntryIsPrivateToItsHousehold(t *testing.T) {
	s, _, alpha, beta := twoHouseholds(t)
	ghee := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy", Aliases: []string{"zz clarified butter"}})
	if ghee.Central {
		t.Fatal("an ingredient created in a household's scope is central")
	}

	if got, err := s.GetIngredient(alpha, ghee.ID); err != nil || got.Name != "zz ghee" || got.Central {
		t.Errorf("owner Get = %+v, %v", got, err)
	}
	for name, ctx := range map[string]context.Context{"another household": beta, "no household": context.Background()} {
		if _, err := s.GetIngredient(ctx, ghee.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Get = %v, want ErrNotFound", name, err)
		}
		if _, ok, err := s.MatchIngredient(ctx, "zz ghee"); err != nil || ok {
			t.Errorf("%s: Match name = ok %v, err %v, want no match", name, ok, err)
		}
		if _, ok, err := s.MatchIngredient(ctx, "zz clarified butter"); err != nil || ok {
			t.Errorf("%s: Match alias = ok %v, err %v, want no match", name, ok, err)
		}
		all, err := s.ListIngredients(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range all {
			if i.ID == ghee.ID {
				t.Errorf("%s: list shows the overlay entry", name)
			}
		}
		if err := s.UpdateIngredient(ctx, Ingredient{ID: ghee.ID, Name: "zz ghee", Aisle: "x"}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Update = %v, want ErrNotFound", name, err)
		}
	}

	// The same name is free in another household: no conflict reveals the first.
	other, err := s.CreateIngredient(beta, Ingredient{Name: "zz ghee", Aisle: "oils", Aliases: []string{"zz clarified butter"}})
	if err != nil {
		t.Fatalf("beta creating the same name: %v", err)
	}
	if other.ID == ghee.ID {
		t.Fatal("two households share one entry")
	}
	if m, ok, _ := s.MatchIngredient(alpha, "zz ghee"); !ok || m.ID != ghee.ID {
		t.Errorf("alpha matches %+v, want its own entry", m)
	}
	if m, ok, _ := s.MatchIngredient(beta, "zz ghee"); !ok || m.ID != other.ID {
		t.Errorf("beta matches %+v, want its own entry", m)
	}
	// Within one household the name is taken.
	if _, err := s.CreateIngredient(alpha, Ingredient{Name: "zz ghee", Aisle: "x"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate in one household = %v, want ErrConflict", err)
	}
}

func TestMatchConsultsOverlayBeforeCentral(t *testing.T) {
	s, d, alpha, beta := twoHouseholds(t)
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz relish", Aisle: "pantry"})
	// The operator later adds a central entry of the same name.
	if _, err := d.Exec(`INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz relish', 'condiments', 1)`); err != nil {
		t.Fatal(err)
	}
	if m, ok, err := s.MatchIngredient(alpha, "zz relish"); err != nil || !ok || m.ID != mine.ID {
		t.Errorf("owner matches %+v ok=%v err=%v, want its overlay entry first", m, ok, err)
	}
	if m, ok, err := s.MatchIngredient(beta, "zz relish"); err != nil || !ok || !m.Central {
		t.Errorf("other household matches %+v ok=%v err=%v, want the central entry", m, ok, err)
	}
}

func TestCentralEntryIsReadOnlyToAHousehold(t *testing.T) {
	s, _, alpha, beta := twoHouseholds(t)
	onion, ok, err := s.MatchIngredient(context.Background(), "onion")
	if err != nil || !ok || !onion.Central {
		t.Fatalf("seed onion = %+v ok=%v err=%v", onion, ok, err)
	}

	renamed := onion
	renamed.Name = "zz not onion"
	err = s.UpdateIngredient(alpha, renamed)
	if !errors.Is(err, ErrCentral) || !errors.Is(err, ErrInvalid) {
		t.Errorf("rename a central entry = %v, want ErrCentral wrapped in ErrInvalid", err)
	}
	if err := mergeCentralAway(alpha, s, onion.ID); !errors.Is(err, ErrCentral) {
		t.Errorf("merging a central entry away = %v, want ErrCentral", err)
	}

	// Its own alias and aisle are the household's to set.
	mine := onion
	mine.Aliases = append(append([]string{}, onion.Aliases...), "zz bulb")
	mine.Aisle = "zz veg"
	if err := s.UpdateIngredient(alpha, mine); err != nil {
		t.Fatalf("household alias and aisle on a central entry: %v", err)
	}
	if m, ok, _ := s.MatchIngredient(alpha, "zz bulb"); !ok || m.ID != onion.ID || m.Aisle != "zz veg" {
		t.Errorf("owner matches %+v ok=%v, want onion in aisle %q", m, ok, "zz veg")
	}
	if got, _ := s.GetIngredient(alpha, onion.ID); !contains(got.Aliases, "zz bulb") || contains(got.Aliases, "zz bulb ") {
		t.Errorf("owner aliases = %v", got.Aliases)
	}
	if _, ok, _ := s.MatchIngredient(beta, "zz bulb"); ok {
		t.Error("another household matches the first household's alias")
	}
	if got, _ := s.GetIngredient(beta, onion.ID); got.Aisle != onion.Aisle || contains(got.Aliases, "zz bulb") {
		t.Errorf("another household sees %+v, want the central aisle and aliases", got)
	}
	if got, _ := s.GetIngredient(context.Background(), onion.ID); got.Aisle != onion.Aisle || contains(got.Aliases, "zz bulb") {
		t.Errorf("central view sees %+v, want the central aisle and aliases", got)
	}

	// Setting the aisle back to the central one drops the override.
	mine.Aisle = onion.Aisle
	if err := s.UpdateIngredient(alpha, mine); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s.db, `SELECT count(*) FROM ingredient_aisle_overrides`); n != 0 {
		t.Errorf("%d aisle overrides remain after restoring the central aisle", n)
	}
}

// mergeCentralAway is a test helper: it merges the central entry id into
// another central entry as a household would try to.
func mergeCentralAway(ctx context.Context, s *Store, loser int64) error {
	other, _, _ := s.MatchIngredient(ctx, "carrot")
	_, err := s.MergeIngredients(ctx, other.ID, loser)
	return err
}

func contains(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}

func TestMergeOverlayIntoCentralKeepsHouseholdAliasesAndRepointsRows(t *testing.T) {
	s, d, alpha, beta := twoHouseholds(t)
	refTable(t, d, "zz_lines")
	s.OnIngredientMerge(repoint("zz_lines"))
	onion, _, _ := s.MatchIngredient(context.Background(), "onion")
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz spanish onion", Aisle: "zz produce", Aliases: []string{"zz big onion"}, Reviewed: true})
	if _, err := d.Exec(`INSERT INTO zz_lines (ingredient_id) VALUES (?)`, mine.ID); err != nil {
		t.Fatal(err)
	}

	merged, err := s.MergeIngredients(alpha, onion.ID, mine.ID)
	if err != nil {
		t.Fatalf("merge an overlay entry into a central one: %v", err)
	}
	if !merged.Central || merged.Aisle != "zz produce" {
		t.Errorf("merged = %+v, want the central entry in the household's reviewed aisle", merged)
	}
	if n := count(t, d, `SELECT count(*) FROM zz_lines WHERE ingredient_id = ?`, onion.ID); n != 1 {
		t.Errorf("%d rows point at the central entry, want the household's one", n)
	}
	for _, term := range []string{"zz spanish onion", "zz big onion"} {
		if m, ok, _ := s.MatchIngredient(alpha, term); !ok || m.ID != onion.ID {
			t.Errorf("owner: %q resolves to %+v ok=%v, want the central entry", term, m, ok)
		}
		if _, ok, _ := s.MatchIngredient(beta, term); ok {
			t.Errorf("another household resolves %q", term)
		}
	}
	if got, _ := s.GetIngredient(beta, onion.ID); got.Aisle != onion.Aisle {
		t.Errorf("another household's aisle = %q, want the central %q", got.Aisle, onion.Aisle)
	}
	// Nothing of the household's reached the central layer.
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE household_id IS NULL AND alias LIKE 'zz %'`); n != 0 {
		t.Errorf("%d household aliases became central", n)
	}
}

func TestMergeAcrossHouseholdsIsRefused(t *testing.T) {
	s, _, alpha, beta := twoHouseholds(t)
	a := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz a", Aisle: "x"})
	b := mustIngredientIn(beta, t, s, Ingredient{Name: "zz b", Aisle: "x"})
	if _, err := s.MergeIngredients(alpha, a.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("loser of another household = %v, want ErrNotFound", err)
	}
	if _, err := s.MergeIngredients(alpha, b.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("winner of another household = %v, want ErrNotFound", err)
	}
	if _, err := s.MergeIngredients(context.Background(), a.ID, b.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("merge outside a household = %v, want ErrInvalid", err)
	}
}

func TestSuggestIsLimitedToOwnOverlayEntries(t *testing.T) {
	s, _, alpha, beta := twoHouseholds(t)
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy", Aliases: []string{"zz clarified butter"}})
	onion, _, _ := s.MatchIngredient(context.Background(), "onion")

	for name, c := range map[string]struct {
		ctx context.Context
		id  int64
	}{"another household": {beta, mine.ID}, "no household": {context.Background(), mine.ID}, "a central entry": {alpha, onion.ID}} {
		if err := s.SuggestIngredient(c.ctx, c.id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Suggest = %v, want ErrNotFound", name, err)
		}
	}
	if got, _ := s.ListSuggestedIngredients(context.Background()); len(got) != 0 {
		t.Fatalf("suggested after refusals = %+v", got)
	}

	if err := s.SuggestIngredient(alpha, mine.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListSuggestedIngredients(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != mine.ID || got[0].Name != "zz ghee" || !contains(got[0].Aliases, "zz clarified butter") {
		t.Fatalf("suggested = %+v, err %v", got, err)
	}
	if g, _ := s.GetIngredient(alpha, mine.ID); !g.Suggested {
		t.Error("owner does not see its entry as suggested")
	}
	if err := s.UnsuggestIngredient(alpha, mine.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSuggestedIngredients(context.Background()); len(got) != 0 {
		t.Errorf("suggested after withdrawing = %+v", got)
	}
}

func TestPromoteMovesEntryToCentralAndMergesExactDuplicates(t *testing.T) {
	d := newTestDB(t)
	s := New(d)
	alpha, beta, gamma, delta := householdCtx(t, d, "alpha"), householdCtx(t, d, "beta"), householdCtx(t, d, "gamma"), householdCtx(t, d, "delta")
	refTable(t, d, "zz_lines")
	s.OnIngredientMerge(repoint("zz_lines"))

	ghee := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy", Aliases: []string{"zz clarified butter"}})
	// Duplicates: beta's name is alpha's alias; gamma's alias is alpha's name.
	byName := mustIngredientIn(beta, t, s, Ingredient{Name: "zz clarified butter", Aisle: "zz oils", Aliases: []string{"zz beta ghee"}, Reviewed: true})
	byAlias := mustIngredientIn(gamma, t, s, Ingredient{Name: "zz gamma fat", Aisle: "zz fats", Aliases: []string{"zz ghee"}, Reviewed: true})
	// Not a duplicate: only a substring of alpha's name.
	near := mustIngredientIn(delta, t, s, Ingredient{Name: "zz ghee jar", Aisle: "dairy"})
	for _, id := range []int64{ghee.ID, byName.ID, byAlias.ID, near.ID} {
		if _, err := d.Exec(`INSERT INTO zz_lines (ingredient_id) VALUES (?)`, id); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SuggestIngredient(alpha, ghee.ID); err != nil {
		t.Fatal(err)
	}
	pre, err := s.PreviewPromotion(context.Background(), ghee.ID)
	if err != nil || pre.Duplicates != 2 {
		t.Fatalf("preview = %+v, err %v, want 2 duplicates", pre, err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE household_id IS NULL AND name = 'zz ghee'`); n != 0 {
		t.Fatal("preview promoted the entry")
	}

	got, err := s.PromoteIngredient(context.Background(), ghee.ID)
	if err != nil {
		t.Fatalf("PromoteIngredient: %v", err)
	}
	if got.Duplicates != 2 || got.Ingredient.ID != ghee.ID || !got.Ingredient.Central {
		t.Errorf("promotion = %+v, want the same id, central, 2 merged", got)
	}

	// Every household reads it now.
	for name, ctx := range map[string]context.Context{"alpha": alpha, "beta": beta, "gamma": gamma, "delta": delta, "none": context.Background()} {
		if m, ok, err := s.MatchIngredient(ctx, "zz ghee"); err != nil || !ok || m.ID != ghee.ID || !m.Central {
			t.Errorf("%s: match zz ghee = %+v ok=%v err=%v", name, m, ok, err)
		}
	}
	// The promoting household's rows needed no rewrite; the duplicates' moved.
	if n := count(t, d, `SELECT count(*) FROM zz_lines WHERE ingredient_id = ?`, ghee.ID); n != 3 {
		t.Errorf("%d rows on the promoted entry, want its own and the two duplicates'", n)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredients WHERE id IN (?, ?)`, byName.ID, byAlias.ID); n != 0 {
		t.Errorf("%d duplicate entries remain", n)
	}
	// The non-duplicate is untouched.
	if m, ok, _ := s.MatchIngredient(delta, "zz ghee jar"); !ok || m.ID != near.ID || m.Central {
		t.Errorf("non-duplicate = %+v ok=%v, want it left in delta's overlay", m, ok)
	}
	// A duplicate's own names stay with its household, as aliases of the entry.
	if m, ok, _ := s.MatchIngredient(beta, "zz beta ghee"); !ok || m.ID != ghee.ID {
		t.Errorf("beta's alias resolves to %+v ok=%v", m, ok)
	}
	if m, ok, _ := s.MatchIngredient(gamma, "zz gamma fat"); !ok || m.ID != ghee.ID {
		t.Errorf("gamma's name resolves to %+v ok=%v", m, ok)
	}
	for name, ctx := range map[string]context.Context{"alpha": alpha, "delta": delta, "none": context.Background()} {
		for _, term := range []string{"zz beta ghee", "zz gamma fat"} {
			if _, ok, _ := s.MatchIngredient(ctx, term); ok {
				t.Errorf("%s resolves %q, a name that belongs to another household", name, term)
			}
		}
	}
	// Their aisles are kept as overrides, so shopping order does not change.
	for _, c := range []struct {
		ctx   context.Context
		aisle string
	}{{alpha, "dairy"}, {beta, "zz oils"}, {gamma, "zz fats"}, {delta, "dairy"}} {
		if g, _ := s.GetIngredient(c.ctx, ghee.ID); g.Aisle != c.aisle {
			t.Errorf("aisle = %q, want %q", g.Aisle, c.aisle)
		}
	}
	// A suggestion is spent.
	if left, _ := s.ListSuggestedIngredients(context.Background()); len(left) != 0 {
		t.Errorf("suggested after promotion = %+v", left)
	}
}

func TestPromoteRefusesNameAlreadyCentralAndChangesNothing(t *testing.T) {
	s, d, alpha, _ := twoHouseholds(t)
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz relish", Aisle: "pantry"})
	if _, err := d.Exec(`INSERT INTO ingredients (name, aisle, reviewed) VALUES ('zz relish', 'condiments', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.SuggestIngredient(alpha, mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PromoteIngredient(context.Background(), mine.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("PromoteIngredient = %v, want ErrConflict", err)
	}
	if g, err := s.GetIngredient(alpha, mine.ID); err != nil || g.Central {
		t.Errorf("entry after the refused promotion = %+v, err %v", g, err)
	}
	if _, err := s.PromoteIngredient(context.Background(), 987654); !errors.Is(err, ErrNotFound) {
		t.Errorf("promote a missing id = %v, want ErrNotFound", err)
	}
	onion, _, _ := s.MatchIngredient(context.Background(), "onion")
	if _, err := s.PromoteIngredient(context.Background(), onion.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("promote a central entry = %v, want ErrNotFound", err)
	}
}

func TestOperatorCannotPromoteOrPreviewAnEntryNobodySuggested(t *testing.T) {
	s, _, alpha, _ := twoHouseholds(t)
	private := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz private", Aisle: "x"})
	if _, err := s.PreviewPromotion(context.Background(), private.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("preview of an unsuggested entry = %v, want ErrNotFound", err)
	}
	if _, err := s.PromoteIngredient(context.Background(), private.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("promotion of an unsuggested entry = %v, want ErrNotFound", err)
	}
	if g, err := s.GetIngredient(alpha, private.ID); err != nil || g.Central {
		t.Errorf("entry = %+v, err %v; want it still private", g, err)
	}
}

func TestPromotionFailureRollsBackTheWholePromotion(t *testing.T) {
	d := newTestDB(t)
	s := New(d)
	alpha, beta := householdCtx(t, d, "alpha"), householdCtx(t, d, "beta")
	ghee := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy"})
	dup := mustIngredientIn(beta, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy"})
	if err := s.SuggestIngredient(alpha, ghee.ID); err != nil {
		t.Fatal(err)
	}
	s.OnIngredientMerge(func(context.Context, *sql.Tx, int64, int64) error { return errors.New("boom") })

	if _, err := s.PromoteIngredient(context.Background(), ghee.ID); err == nil {
		t.Fatal("promotion succeeded although a hook failed")
	}
	if g, err := s.GetIngredient(alpha, ghee.ID); err != nil || g.Central {
		t.Errorf("entry after the failed promotion = %+v, err %v; want it still in the overlay", g, err)
	}
	if g, err := s.GetIngredient(beta, dup.ID); err != nil || g.Central {
		t.Errorf("duplicate after the failed promotion = %+v, err %v; want it untouched", g, err)
	}
}

func TestDeletingAHouseholdRemovesItsOverlay(t *testing.T) {
	s, d, alpha, beta := twoHouseholds(t)
	onion, _, _ := s.MatchIngredient(context.Background(), "onion")
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy", Aliases: []string{"zz clarified butter"}})
	onionMine := onion
	onionMine.Aliases = append(append([]string{}, onion.Aliases...), "zz bulb")
	onionMine.Aisle = "zz veg"
	if err := s.UpdateIngredient(alpha, onionMine); err != nil {
		t.Fatal(err)
	}
	keep := mustIngredientIn(beta, t, s, Ingredient{Name: "zz keep", Aisle: "x"})
	h, _ := household.FromContext(alpha)

	if _, err := d.Exec(`DELETE FROM households WHERE id = ?`, h.ID); err != nil {
		t.Fatalf("delete household: %v", err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM ingredients WHERE id = ` + strconv.FormatInt(mine.ID, 10):             0,
		`SELECT count(*) FROM ingredient_aliases WHERE alias IN ('zz clarified butter', 'zz bulb')`: 0,
		`SELECT count(*) FROM ingredient_aisle_overrides`:                                           0,
		`SELECT count(*) FROM ingredients WHERE id = ` + strconv.FormatInt(keep.ID, 10):             1,
		`SELECT count(*) FROM ingredients WHERE id = ` + strconv.FormatInt(onion.ID, 10):            1,
	} {
		if got := count(t, d, q); got != want {
			t.Errorf("%s = %d, want %d", q, got, want)
		}
	}
}

// The store keeps an alias in its scope: nothing it writes puts a household's
// alias on another household's entry, or a central alias on an overlay entry.
func TestAliasStaysInItsScope(t *testing.T) {
	s, d, alpha, beta := twoHouseholds(t)
	mine := mustIngredientIn(alpha, t, s, Ingredient{Name: "zz ghee", Aisle: "dairy"})
	hb, _ := household.FromContext(beta)
	ha, _ := household.FromContext(alpha)
	onion, _, _ := s.MatchIngredient(context.Background(), "onion")

	if err := replaceIngredientAliases(beta, s.q, owner(hb.ID), mine.ID, []string{"zz sneaky"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("beta's alias on alpha's entry = %v, want ErrInvalid", err)
	}
	if err := replaceIngredientAliases(beta, s.q, owner(0), mine.ID, []string{"zz central sneaky"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a central alias on an overlay entry = %v, want ErrInvalid", err)
	}
	if err := addIngredientAlias(beta, s.q, owner(hb.ID), "zz sneaky", mine.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("addIngredientAlias across households = %v, want ErrInvalid", err)
	}
	// What is allowed: an owner's alias on its entry, and a household's on a central one.
	if err := replaceIngredientAliases(alpha, s.q, owner(ha.ID), mine.ID, []string{"zz fine"}); err != nil {
		t.Errorf("owner alias: %v", err)
	}
	if err := addIngredientAlias(alpha, s.q, owner(ha.ID), "zz bulb", onion.ID); err != nil {
		t.Errorf("household alias on a central entry: %v", err)
	}
	if n := count(t, d, `SELECT count(*) FROM ingredient_aliases WHERE alias LIKE 'zz %sneaky'`); n != 0 {
		t.Errorf("%d out-of-scope aliases were written", n)
	}
}

func TestNeedsReviewListsOnlyTheScopesOwnEntries(t *testing.T) {
	s, d, alpha, _ := twoHouseholds(t)
	if _, err := d.Exec(`UPDATE ingredients SET reviewed = 0 WHERE name = 'onion'`); err != nil {
		t.Fatal(err)
	}
	mustIngredientIn(alpha, t, s, Ingredient{Name: "zz proposed", Aisle: "x"})
	got, err := s.ListUnreviewedIngredients(alpha)
	if err != nil || len(got) != 1 || got[0].Name != "zz proposed" {
		t.Errorf("household needs-review = %+v, err %v; want only its own entry", got, err)
	}
	got, err = s.ListUnreviewedIngredients(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "onion" {
		t.Errorf("central needs-review = %+v, err %v; want only the central entry", got, err)
	}
}
