// Package recipes is the recipe list and search page: every recipe with its
// rating, tags and times, a search box and tag chips that narrow the list. The
// results are an HTMX fragment inside the page, and the address bar always
// holds the query and tags, so a result page can be reloaded or shared
// (ADR-00002). Persistence is the recipe store (ADR-00004). It is mounted from
// internal/web (ADR-00012).
package recipes

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. The recipe view owns
// Pattern/{id}; this package owns the bare collection URL.
const Pattern = "/recipes"

// Query parameters of the list page.
const (
	paramText = "q"
	paramTag  = "tag"
)

// maxQueryRunes bounds the search text; a recipe search is a few words.
const maxQueryRunes = 200

// Library is what the page reads from the recipe store.
type Library interface {
	Search(ctx context.Context, q store.SearchQuery) ([]store.Summary, error)
	Tags(ctx context.Context) ([]store.TagCount, error)
}

// Deps is what the routes need.
type Deps struct {
	Library Library
}

// Routes returns the routes of the list page, relative to Pattern.
func Routes(d Deps) chi.Router {
	r := chi.NewRouter()
	r.Get("/", handle(d))
	return r
}

func handle(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get(paramText)
		if utf8.RuneCountInString(text) > maxQueryRunes {
			layout.WriteError(w, r, fmt.Errorf("search text of %d characters: %w", utf8.RuneCountInString(text), layout.ErrInvalid))
			return
		}
		tags := r.URL.Query()[paramTag]
		sq := store.SearchQuery{Text: text, Tags: tags}
		found, err := d.Library.Search(r.Context(), sq)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("search recipes: %w", err))
			return
		}
		inUse, err := d.Library.Tags(r.Context())
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("list tags: %w", err))
			return
		}
		v := buildView(strings.TrimSpace(text), tags, found, inUse)
		// The same URL answers a full page and, for htmx, just the results.
		w.Header().Add("Vary", "HX-Request")
		if fromHTMX(r) {
			layout.Render(w, r, http.StatusOK, resultsFragment(v))
			return
		}
		layout.Render(w, r, http.StatusOK, listPage(v))
	}
}

// fromHTMX reports an htmx request that wants just the results. A boosted
// navigation wants the whole page, and so does a history restore: on a
// history-cache miss htmx asks for the URL again and swaps the answer into
// <body>, so a fragment there would leave a page without its form or layout.
func fromHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" &&
		r.Header.Get("HX-Boosted") != "true" &&
		r.Header.Get("HX-History-Restore-Request") != "true"
}

func recipeHref(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) }
