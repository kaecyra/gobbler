package store

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// keyIngredientCap is how many ingredient names a library card lists.
const keyIngredientCap = 5

// Card is a recipe as the library shows it (ADR-00022): the search summary plus
// what a card adds. Steps carry only what the time ribbon reads: duration,
// passive flag and stage kind, in recipe order.
type Card struct {
	Summary
	MealType   recipe.MealType
	SpiceLevel *int
	// Source is where the recipe came from: the site of its source URL without
	// "www.", else its author, else empty.
	Source string
	// Components are the named parts of the recipe, in order. A recipe with one
	// unnamed part has none.
	Components []string
	// KeyIngredients are the catalog names of the first required lines, each
	// once, in recipe order.
	KeyIngredients []string
	Steps          []recipe.Step
	// MadeCount is the number of made-it entries; LastMade is the date of the
	// latest, nil when there is none.
	MadeCount int
	LastMade  *time.Time
}

// MadeEntry is one line of the made-it log with the name of its recipe, for the
// "recently made" panel.
type MadeEntry struct {
	RecipeID int64
	Name     string
	On       time.Time
	Comment  string
	Rating   *int
}

// Cards returns the recipes matching q as library cards, in the order Search
// gives. The card parts of the whole household come in a fixed number of
// queries, so a page costs the same however many recipes it lists.
func (s *Store) Cards(ctx context.Context, q SearchQuery) ([]Card, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	found, err := s.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, len(found))
	for i, sum := range found {
		cards[i] = Card{Summary: sum}
	}
	if len(cards) == 0 {
		return cards, nil
	}
	err = s.read(ctx, func(qs *storedb.Queries) error { return fillCards(ctx, qs, hid, cards) })
	if err != nil {
		return nil, err
	}
	return cards, nil
}

// fillCards adds the parts of a card that Search does not read.
func fillCards(ctx context.Context, q *storedb.Queries, hid int64, cards []Card) error {
	at := make(map[int64]*Card, len(cards))
	for i := range cards {
		at[cards[i].ID] = &cards[i]
	}

	fields, err := q.ListLibraryFields(ctx, hid)
	if err != nil {
		return readErr("library fields", err)
	}
	for _, r := range fields {
		if c := at[r.ID]; c != nil {
			c.MealType, c.SpiceLevel, c.Source = recipe.MealType(r.MealType), intPtr(r.SpiceLevel), sourceOf(r.SourceUrl, r.Author)
		}
	}

	comps, err := q.ListLibraryComponents(ctx, hid)
	if err != nil {
		return readErr("library components", err)
	}
	for _, r := range comps {
		if c := at[r.RecipeID]; c != nil {
			c.Components = append(c.Components, r.Name)
		}
	}

	ings, err := q.ListLibraryIngredients(ctx, hid)
	if err != nil {
		return readErr("library ingredients", err)
	}
	for _, r := range ings {
		if c := at[r.RecipeID]; c != nil && len(c.KeyIngredients) < keyIngredientCap && !slices.Contains(c.KeyIngredients, r.Name) {
			c.KeyIngredients = append(c.KeyIngredients, r.Name)
		}
	}

	steps, err := q.ListLibrarySteps(ctx, hid)
	if err != nil {
		return readErr("library steps", err)
	}
	for _, r := range steps {
		if c := at[r.RecipeID]; c != nil {
			c.Steps = append(c.Steps, recipe.Step{Duration: time.Duration(r.DurationNs), Passive: r.Passive == 1, Kind: recipe.StageKind(r.StageKind)})
		}
	}

	made, err := q.ListLibraryMadeStats(ctx, hid)
	if err != nil {
		return readErr("library made-it counts", err)
	}
	for _, r := range made {
		c := at[r.RecipeID]
		if c == nil {
			continue
		}
		last, err := time.Parse(dateLayout, r.LastMade)
		if err != nil {
			return readErr("library made-it dates", err)
		}
		c.MadeCount, c.LastMade = int(r.Made), &last
	}
	return nil
}

// sourceOf names where a recipe came from for a card.
func sourceOf(rawURL, author string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return author
}

// RecentlyMade returns the newest made-it entries of the household, at most
// limit of them, the later log entry first when two share a date.
func (s *Store) RecentlyMade(ctx context.Context, limit int) ([]MadeEntry, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, nil
	}
	var out []MadeEntry
	err = s.read(ctx, func(q *storedb.Queries) error {
		rows, err := q.ListRecentMadeIt(ctx, storedb.ListRecentMadeItParams{HouseholdID: hid, MaxRows: int64(limit)})
		if err != nil {
			return readErr("recently made", err)
		}
		for _, r := range rows {
			on, err := time.Parse(dateLayout, r.MadeOn)
			if err != nil {
				return readErr("recently made", err)
			}
			out = append(out, MadeEntry{RecipeID: r.RecipeID, Name: r.Name, On: on, Comment: r.Comment, Rating: intPtr(r.Rating)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Idle returns every recipe of the household as a card, the one made longest
// ago first (see IdleOrder). It is what a suggestion draws from.
func (s *Store) Idle(ctx context.Context) ([]Card, error) {
	cards, err := s.Cards(ctx, SearchQuery{})
	if err != nil {
		return nil, err
	}
	return IdleOrder(cards), nil
}

// IdleOrder sorts cards, in place and returned, with the one made longest ago
// first: recipes made at least once by the date of their latest entry, then the
// recipes never made, by name. A page that already holds every card uses it
// instead of reading them again.
func IdleOrder(cards []Card) []Card {
	slices.SortStableFunc(cards, func(a, b Card) int {
		switch {
		case a.LastMade != nil && b.LastMade != nil:
			return a.LastMade.Compare(*b.LastMade)
		case a.LastMade != nil:
			return -1
		case b.LastMade != nil:
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return cards
}
