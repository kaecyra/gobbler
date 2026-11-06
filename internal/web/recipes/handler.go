// Package recipes is the home page and the recipe library (ADR-00022): a hero
// search on the home page; the library of recipe cards grouped by meal or by
// time to table, with a search box and side panels read from the made-it log.
// The results are an HTMX fragment inside the page, and the address bar always
// holds the query and the grouping, so a result page can be reloaded or shared
// (ADR-00002). Persistence is the recipe store (ADR-00004). The library is
// mounted from internal/web (ADR-00012); the home page is mounted at "/" from
// the same place.
package recipes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. The recipe view owns
// Pattern/{id}; this package owns the bare collection URL.
const Pattern = "/recipes"

// Query parameters of the two pages.
const (
	paramText  = "q"
	paramGroup = "group"
	paramIdea  = "idea"
)

// maxQueryRunes bounds the search text; a recipe search is a few words.
const maxQueryRunes = 200

// maxIdea bounds the suggestion index, a number from the URL.
const maxIdea = 1_000_000

// Library is what the pages read from the recipe service
// (*apirecipes.Service satisfies it).
type Library interface {
	Cards(ctx context.Context, c apirecipes.Caller, q apirecipes.SearchQuery) ([]apirecipes.Card, error)
	RecentlyMade(ctx context.Context, c apirecipes.Caller, limit int) ([]apirecipes.MadeEntry, error)
	Idle(ctx context.Context, c apirecipes.Caller) ([]apirecipes.Card, error)
}

// Pictures finds the picture of a recipe (ADR-00039). The picture package's
// service, through internal/web/picture.Srcs, satisfies it.
type Pictures interface {
	// Src is the address of recipe id's picture, empty when it has none.
	Src(ctx context.Context, id int64) (string, error)
}

// Deps is what the routes need.
type Deps struct {
	Library Library
	// Pictures gives the address of a recipe's picture for its card. Nil
	// shows every card with the placeholder band.
	Pictures Pictures
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Routes returns the routes of the library, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", library(d))
	return r
}

func library(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get(paramText)
		if utf8.RuneCountInString(text) > maxQueryRunes {
			layout.WriteError(w, r, fmt.Errorf("search text of %d characters: %w", utf8.RuneCountInString(text), layout.ErrInvalid))
			return
		}
		idea, err := ideaParam(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		q := strings.TrimSpace(text)
		group := r.URL.Query().Get(paramGroup)
		// The same URL answers a full page and, for htmx aimed at one part of
		// it, just that part.
		varyOnHTMX(w)
		if targets(r, "suggest") {
			all, err := d.Library.Cards(r.Context(), caller.FromContext(r.Context()), apirecipes.SearchQuery{})
			if err != nil {
				layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
				return
			}
			idle, now := apirecipes.IdleOrder(all), d.now()
			// No idea left (a recipe was made since the page loaded) is an
			// answer, not an error: an empty 200 removes the card.
			sg := suggestFor(ideasFrom(idle, now, forgotPick(idle, now)), idea, now, ideaHref(q, group))
			if err := d.addSuggestPicture(r.Context(), sg); err != nil {
				layout.WriteError(w, r, err)
				return
			}
			if sg != nil {
				layout.Render(w, r, http.StatusOK, suggestion(*sg))
			}
			return
		}
		found, err := d.Library.Cards(r.Context(), caller.FromContext(r.Context()), apirecipes.SearchQuery{Text: q})
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
			return
		}
		res := buildResults(q, group, found, d.now())
		if err := d.addPictures(r.Context(), res.Groups); err != nil {
			layout.WriteError(w, r, err)
			return
		}
		if targets(r, "results") {
			layout.Render(w, r, http.StatusOK, resultsFragment(res))
			return
		}
		// The side panels draw on the whole library: the cards already loaded
		// when nothing narrowed them, else one more read.
		all := found
		if q != "" {
			if all, err = d.Library.Cards(r.Context(), caller.FromContext(r.Context()), apirecipes.SearchQuery{}); err != nil {
				layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
				return
			}
		}
		recent, err := d.Library.RecentlyMade(r.Context(), caller.FromContext(r.Context()), recentLimit)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list recently made: %w", err))
			return
		}
		idle, now := apirecipes.IdleOrder(slices.Clone(all)), d.now()
		forgot := forgotPick(idle, now)
		side := sideView{
			Recent:  recentViews(recent),
			Forgot:  forgotFor(forgot, now),
			Suggest: suggestFor(ideasFrom(idle, now, forgot), idea, now, ideaHref(q, group)),
		}
		if err := d.addSuggestPicture(r.Context(), side.Suggest); err != nil {
			layout.WriteError(w, r, err)
			return
		}
		layout.Render(w, r, http.StatusOK, libraryPage(res, side))
	}
}

// addPictures sets the address of each card's picture. A recipe deleted since
// the cards were read has no picture to show, which is an answer, not a
// failure; any other error is.
func (d Deps) addPictures(ctx context.Context, groups []groupView) error {
	for gi := range groups {
		for ci := range groups[gi].Cards {
			if err := d.addPicture(ctx, &groups[gi].Cards[ci]); err != nil {
				return err
			}
		}
	}
	return nil
}

// addPicture sets the address of one card's picture, on the same terms as
// addPictures.
func (d Deps) addPicture(ctx context.Context, c *cardView) error {
	if d.Pictures == nil {
		return nil
	}
	src, err := d.Pictures.Src(ctx, c.ID)
	if errors.Is(err, apirecipes.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find picture of recipe %d: %w", c.ID, err)
	}
	c.Picture = src
	return nil
}

// addSuggestPicture sets the picture of a suggestion's card; a nil suggestion
// has none to set.
func (d Deps) addSuggestPicture(ctx context.Context, s *suggestView) error {
	if s == nil {
		return nil
	}
	return d.addPicture(ctx, &s.Card)
}

// ideaParam reads the suggestion index from the URL; absent is the first idea.
func ideaParam(r *http.Request) (int, error) {
	raw := r.URL.Query().Get(paramIdea)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > maxIdea {
		return 0, fmt.Errorf("suggestion %q: %w", raw, layout.ErrInvalid)
	}
	return n, nil
}

// Home returns the handler of the home page.
func Home(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idea, err := ideaParam(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		idle, err := d.Library.Idle(r.Context(), caller.FromContext(r.Context()))
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list idle recipes: %w", err))
			return
		}
		// Home has no forgot panel, so its suggestion skips nothing.
		now := d.now()
		v := homeView{Empty: len(idle) == 0, Suggest: suggestFor(ideasFrom(idle, now, nil), idea, now, homeHref)}
		if err := d.addSuggestPicture(r.Context(), v.Suggest); err != nil {
			layout.WriteError(w, r, err)
			return
		}
		varyOnHTMX(w)
		if targets(r, "suggest") {
			// As on the library: no idea left answers with an empty 200.
			if v.Suggest != nil {
				layout.Render(w, r, http.StatusOK, suggestion(*v.Suggest))
			}
			return
		}
		recent, err := d.Library.RecentlyMade(r.Context(), caller.FromContext(r.Context()), recentLimit)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list recently made: %w", err))
			return
		}
		v.Recent = recentViews(recent)
		layout.Render(w, r, http.StatusOK, homePage(v))
	}
}

func varyOnHTMX(w http.ResponseWriter) {
	w.Header().Add("Vary", "HX-Request")
	w.Header().Add("Vary", "HX-Target")
}

// targets reports an htmx request that wants just the element with this id. A
// boosted navigation wants the whole page, and so does a history restore: on a
// history-cache miss htmx asks for the URL again and swaps the answer into
// <body>, so a fragment there would leave a page without its form or layout.
func targets(r *http.Request, id string) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-Target") == id &&
		r.Header.Get("HX-Boosted") != "true" &&
		r.Header.Get("HX-History-Restore-Request") != "true"
}
