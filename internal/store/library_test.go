package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

// libraryRecipe is a recipe with the parts a library card shows.
func libraryRecipe(name string, ids catalogIDs) recipe.Recipe {
	line := func(id int64, optional bool, raw string) recipe.IngredientLine {
		return recipe.IngredientLine{Ingredient: recipe.IngredientID(id), Optional: optional, Rule: quantity.RuleLinear, Raw: raw}
	}
	return recipe.Recipe{
		Name:       name,
		SourceURL:  "https://www.example.test/some/page",
		Author:     "A. Cook",
		MealType:   recipe.MealMain,
		SpiceLevel: ptr(2),
		Components: []recipe.Component{
			{
				Name: "Dough",
				Lines: []recipe.IngredientLine{
					line(ids.flour, false, "flour"),
					line(ids.salt, true, "salt, optional"),
					{Rule: quantity.RuleLinear, Raw: "water"}, // no catalog ingredient: not a key ingredient
					line(ids.butter, false, "butter"),
				},
				Steps: []recipe.Step{
					{ID: "s1", Text: "Mix.", Duration: 10 * time.Minute, Kind: recipe.StagePrep},
					{ID: "s2", Text: "Rise.", Duration: time.Hour, Passive: true, Kind: recipe.StageRise, DependsOn: []recipe.StepID{"s1"}},
					{ID: "s3", Text: "Think."},
				},
			},
			{
				Name:  "Topping",
				Lines: []recipe.IngredientLine{line(ids.egg, false, "egg"), line(ids.flour, false, "more flour")},
				Steps: []recipe.Step{{ID: "s4", Text: "Bake.", Duration: 30 * time.Minute, Passive: true, Kind: recipe.StageBake, DependsOn: []recipe.StepID{"s2"}}},
			},
		},
	}
}

func TestCardsCarryWhatTheLibraryShows(t *testing.T) {
	ctx := authedCtx(t)
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)

	id, err := s.Create(ctx, libraryRecipe("Focaccia", ids))
	if err != nil {
		t.Fatal(err)
	}
	plain := simpleRecipe("Plain")
	if _, err := s.Create(ctx, plain); err != nil {
		t.Fatal(err)
	}
	for _, e := range []MadeIt{{On: day(time.March, 4)}, {On: day(time.May, 9)}, {On: day(time.April, 1)}} {
		if _, err := s.AddMadeIt(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}

	cards, err := s.Cards(ctx, SearchQuery{})
	if err != nil {
		t.Fatalf("Cards: %v", err)
	}
	if len(cards) != 2 || cards[0].Name != "Focaccia" || cards[1].Name != "Plain" {
		t.Fatalf("cards = %+v", cards)
	}
	f := cards[0]
	if f.ID != id || f.MealType != recipe.MealMain || f.SpiceLevel == nil || *f.SpiceLevel != 2 {
		t.Errorf("identity and fields = %+v", f)
	}
	if f.Source != "example.test" {
		t.Errorf("Source = %q, want the host without www", f.Source)
	}
	if want := []string{"Dough", "Topping"}; !reflect.DeepEqual(f.Components, want) {
		t.Errorf("Components = %v, want %v", f.Components, want)
	}
	// Optional lines and lines with no catalog ingredient are not key
	// ingredients; each name appears once, in recipe order.
	if want := []string{"zz test flour", "zz test butter", "zz test egg"}; !reflect.DeepEqual(f.KeyIngredients, want) {
		t.Errorf("KeyIngredients = %v, want %v", f.KeyIngredients, want)
	}
	// Steps are in recipe order, so the ribbon reads left to right.
	if len(f.Steps) != 4 || f.Steps[0].Kind != recipe.StagePrep || f.Steps[1].Kind != recipe.StageRise || !f.Steps[1].Passive || f.Steps[3].Duration != 30*time.Minute {
		t.Errorf("Steps = %+v", f.Steps)
	}
	if f.MadeCount != 3 || f.LastMade == nil || !f.LastMade.Equal(day(time.May, 9)) {
		t.Errorf("made = %d, last %v; want 3 and 9 May", f.MadeCount, f.LastMade)
	}

	p := cards[1]
	if p.MadeCount != 0 || p.LastMade != nil || p.MealType != "" || p.SpiceLevel != nil || p.Source != "" || len(p.Components) != 0 {
		t.Errorf("plain card = %+v, want nothing set", p)
	}
}

func TestCardSourceFallsBackToTheAuthor(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	for name, c := range map[string]struct{ url, author, want string }{
		"Host":       {"https://example.test/x", "A. Cook", "example.test"},
		"Author":     {"", "A. Cook", "A. Cook"},
		"Neither":    {"", "", ""},
		"Unparsable": {"not a url", "B. Baker", "B. Baker"},
	} {
		r := simpleRecipe(name)
		r.SourceURL, r.Author = c.url, c.author
		if _, err := s.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	cards, err := s.Cards(ctx, SearchQuery{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cards {
		got[c.Name] = c.Source
	}
	want := map[string]string{"Host": "example.test", "Author": "A. Cook", "Neither": "", "Unparsable": "B. Baker"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %v, want %v", got, want)
	}
}

func TestCardsFollowTheSearch(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	for _, n := range []string{"Lemon cake", "Black bean soup", "Lemon tart"} {
		if _, err := s.Create(ctx, simpleRecipe(n)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Cards(ctx, SearchQuery{Text: "lemon"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("cards = %+v, want the two lemon recipes", got)
	}
	for _, c := range got {
		if c.Name == "Black bean soup" {
			t.Errorf("search for lemon returned %q", c.Name)
		}
	}
	none, err := s.Cards(ctx, SearchQuery{Text: "zzzz"})
	if err != nil || len(none) != 0 {
		t.Errorf("no match = %v, %v; want empty, nil", none, err)
	}
}

// seedMade creates a recipe in ctx's household and logs the given dates.
func seedMade(ctx context.Context, t *testing.T, s *Store, name string, rating *int, on ...time.Time) int64 {
	t.Helper()
	r := simpleRecipe(name)
	r.Rating = rating
	id, err := s.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range on {
		if _, err := s.AddMadeIt(ctx, id, MadeIt{On: d, Comment: name + " on " + d.Format("2 Jan")}); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestRecentlyMadeIsNewestFirstAndLimited(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	a := seedMade(ctx, t, s, "A", nil, day(time.March, 1), day(time.March, 20))
	b := seedMade(ctx, t, s, "B", nil, day(time.March, 10))
	seedMade(ctx, t, s, "Never", nil)
	// The same day twice: the later log entry comes first.
	c := seedMade(ctx, t, s, "C", nil, day(time.March, 20))

	tests := []struct {
		limit int
		want  []int64
	}{
		{limit: 10, want: []int64{c, a, b, a}},
		{limit: 2, want: []int64{c, a}},
		{limit: 0, want: nil},
	}
	for _, tc := range tests {
		got, err := s.RecentlyMade(ctx, tc.limit)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, e := range got {
			ids = append(ids, e.RecipeID)
		}
		if !reflect.DeepEqual(ids, tc.want) {
			t.Errorf("RecentlyMade(%d) recipes = %v, want %v", tc.limit, ids, tc.want)
		}
	}

	got, _ := s.RecentlyMade(ctx, 1)
	if len(got) != 1 || got[0].Name != "C" || got[0].Comment != "C on 20 Mar" || !got[0].On.Equal(day(time.March, 20)) {
		t.Errorf("newest entry = %+v", got)
	}
}

func TestIdleListsTheLongestUnmadeFirst(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	seedMade(ctx, t, s, "Recent", nil, day(time.June, 1))
	seedMade(ctx, t, s, "Old", ptr(4), day(time.January, 5), day(time.February, 1))
	seedMade(ctx, t, s, "Oldest", nil, day(time.January, 2))
	seedMade(ctx, t, s, "Zebra never", nil)
	seedMade(ctx, t, s, "Apple never", nil)

	got, err := s.Idle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range got {
		order = append(order, c.Name)
	}
	// Made recipes by their last date, oldest first; never made last, by name.
	want := []string{"Oldest", "Old", "Recent", "Apple never", "Zebra never"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("Idle order = %v, want %v", order, want)
	}
}

func TestLibraryReadsNeverCrossHouseholds(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	mine, theirs := authedCtx(t), otherCtx(t)

	id, err := s.Create(theirs, libraryRecipe("Their secret", ids))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(theirs, id, MadeIt{On: day(time.May, 1), Comment: "private"}); err != nil {
		t.Fatal(err)
	}
	seedMade(mine, t, s, "Mine", nil, day(time.May, 2))

	cards, err := s.Cards(mine, SearchQuery{})
	if err != nil || len(cards) != 1 || cards[0].Name != "Mine" || cards[0].MadeCount != 1 {
		t.Errorf("Cards = %+v, %v; want only Mine, made once", cards, err)
	}
	recent, err := s.RecentlyMade(mine, 10)
	if err != nil || len(recent) != 1 || recent[0].Name != "Mine" {
		t.Errorf("RecentlyMade = %+v, %v; want only Mine", recent, err)
	}
	idle, err := s.Idle(mine)
	if err != nil || len(idle) != 1 || idle[0].Name != "Mine" {
		t.Errorf("Idle = %+v, %v; want only Mine", idle, err)
	}
	// Their card still carries their own key ingredients and made-it log.
	theirCards, err := s.Cards(theirs, SearchQuery{})
	if err != nil || len(theirCards) != 1 || theirCards[0].MadeCount != 1 || len(theirCards[0].KeyIngredients) == 0 {
		t.Errorf("their Cards = %+v, %v", theirCards, err)
	}
}

func TestLibraryReadsNeedAHousehold(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := noHouseholdCtx(t)
	if _, err := s.Cards(ctx, SearchQuery{}); err == nil {
		t.Error("Cards without a household succeeded")
	}
	if _, err := s.RecentlyMade(ctx, 5); err == nil {
		t.Error("RecentlyMade without a household succeeded")
	}
	if _, err := s.Idle(ctx); err == nil {
		t.Error("Idle without a household succeeded")
	}
}
