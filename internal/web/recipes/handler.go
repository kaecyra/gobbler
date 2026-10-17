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
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/store"
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

// Library is what the pages read from the recipe store.
type Library interface {
	Cards(ctx context.Context, q store.SearchQuery) ([]store.Card, error)
	RecentlyMade(ctx context.Context, limit int) ([]store.MadeEntry, error)
	Idle(ctx context.Context) ([]store.Card, error)
}

// Deps is what the routes need.
type Deps struct {
	Library Library
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
			all, err := d.Library.Cards(r.Context(), store.SearchQuery{})
			if err != nil {
				layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
				return
			}
			if idle := store.IdleOrder(all); len(idle) > 0 {
				layout.Render(w, r, http.StatusOK, suggestion(suggestFor(idle, idea, d.now(), ideaHref(q, group))))
				return
			}
		}
		found, err := d.Library.Cards(r.Context(), store.SearchQuery{Text: q})
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
			return
		}
		res := buildResults(q, group, found, d.now())
		if targets(r, "results") {
			layout.Render(w, r, http.StatusOK, resultsFragment(res))
			return
		}
		// The side panels draw on the whole library: the cards already loaded
		// when nothing narrowed them, else one more read.
		all := found
		if q != "" {
			if all, err = d.Library.Cards(r.Context(), store.SearchQuery{}); err != nil {
				layout.WriteError(w, r, fmt.Errorf("list library cards: %w", err))
				return
			}
		}
		recent, err := d.Library.RecentlyMade(r.Context(), recentLimit)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list recently made: %w", err))
			return
		}
		idle := store.IdleOrder(slices.Clone(all))
		side := sideView{Recent: recentViews(recent), Forgot: forgotFor(idle, d.now())}
		if len(idle) > 0 {
			sg := suggestFor(idle, idea, d.now(), ideaHref(q, group))
			side.Suggest = &sg
		}
		layout.Render(w, r, http.StatusOK, libraryPage(res, side))
	}
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
		idle, err := d.Library.Idle(r.Context())
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list idle recipes: %w", err))
			return
		}
		v := homeView{Empty: len(idle) == 0}
		if !v.Empty {
			v.Suggest = suggestFor(idle, idea, d.now(), homeHref)
		}
		varyOnHTMX(w)
		if targets(r, "suggest") && !v.Empty {
			layout.Render(w, r, http.StatusOK, suggestion(v.Suggest))
			return
		}
		recent, err := d.Library.RecentlyMade(r.Context(), recentLimit)
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
