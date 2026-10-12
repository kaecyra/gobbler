// Package ingest is the web side of ingestion: the form that takes a URL or
// pasted text, and the review screen where the user checks, edits and confirms
// the draft before anything is saved (ADR-00006). It drives the pipeline in
// internal/ingest (FromURL or FromText, then Confirm, then Saver.Save) and
// never saves anything itself.
//
// A draft waits in memory under a random token between ingest and confirm.
// Only the confirm route saves, and only through ingest.Confirm, so no route
// stores an ingested recipe without the review step.
package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// maxFormBytes bounds a submitted form: pasted text and the edited draft.
const maxFormBytes = 4 << 20

// recipePath is where a saved recipe is viewed. The recipe view owns the
// route; this package only redirects to it.
func recipePath(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) }

// Drafter produces drafts; *ingestcore.Pipeline satisfies it.
type Drafter interface {
	FromURL(ctx context.Context, rawURL string) (ingestcore.Draft, error)
	FromText(ctx context.Context, text string) (ingestcore.Draft, error)
}

// Saver stores a confirmed draft; *ingestcore.Saver satisfies it.
type Saver interface {
	Save(ctx context.Context, c ingestcore.Confirmed) (ingestcore.Saved, error)
}

// Deps is what the ingest routes need to build the pipeline.
type Deps struct {
	DB        *sql.DB
	Ingestion config.Ingestion
	// Logger receives this package's logs; the default is slog.Default().
	Logger *slog.Logger
}

type handler struct {
	drafter Drafter
	saver   Saver
	matcher ingredientMatcher
	pending *pendingDrafts
	log     *slog.Logger
	// initErr is why the pipeline could not be built. Every route still exists
	// and answers 500 with it (see requireReady), so a bad configuration is a
	// loud failure rather than a missing page.
	initErr error
}

// Routes returns the ingest routes, to be mounted at /ingest. The pipeline,
// catalog and recipe store are built over d.DB. The LLM provider is not wired
// here: a draft below the threshold is flagged on the review screen until the
// fallback lands.
func Routes(d Deps) http.Handler {
	h := &handler{pending: newPendingDrafts(time.Now), log: d.Logger}
	if h.log == nil {
		h.log = slog.Default()
	}
	cat := catalog.New(d.DB)
	pipeline, err := ingestcore.New(d.Ingestion, ingestcore.Deps{Catalog: cat})
	if err != nil {
		h.initErr = fmt.Errorf("build ingest pipeline: %w", err)
		h.log.Error("ingest routes unavailable", "error", err.Error())
	} else {
		h.drafter = pipeline
		h.saver = ingestcore.NewSaver(store.New(d.DB), cat)
		h.matcher = cat
	}
	return h.routes()
}

func (h *handler) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.requireReady)
	r.Get("/", h.form)
	r.Post("/", h.submit)
	r.Get("/{token}", h.review)
	r.Post("/{token}/confirm", h.confirm)
	r.Post("/{token}/cancel", h.cancel)
	return r
}

// requireReady answers every route with 500 when the pipeline could not be
// built, before any handler or token lookup runs.
func (h *handler) requireReady(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.initErr != nil {
			layout.WriteError(w, r, h.initErr)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// formView is what the ingest form renders.
type formView struct {
	URL, Text string
	Error     string
}

// reviewView is what the review screen renders.
type reviewView struct {
	Token string
	Draft ingestcore.Draft
	// Problems are why the last confirm did not go through, in words.
	Problems []string
}

func (h *handler) form(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, formPage(formView{}))
}

// submit runs the pipeline and redirects to the review screen. Nothing is
// saved. A failure to produce a draft is shown on the form, never as a draft
// (error-handling.md: an error is not turned into a low-scored draft).
func (h *handler) submit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("read form: %w", layout.ErrInvalid))
		return
	}
	v := formView{URL: strings.TrimSpace(r.PostForm.Get("url")), Text: r.PostForm.Get("text")}
	hasText := strings.TrimSpace(v.Text) != ""
	switch {
	case v.URL == "" && !hasText:
		v.Error = "Enter a recipe URL or paste the recipe text."
	case v.URL != "" && hasText:
		v.Error = "Give a URL or pasted text, not both."
	case v.URL != "" && !isWebURL(v.URL):
		v.Error = "That does not look like a web address. It must start with http:// or https://."
	}
	if v.Error != "" {
		layout.Render(w, r, http.StatusBadRequest, formPage(v))
		return
	}

	var (
		draft ingestcore.Draft
		err   error
	)
	if v.URL != "" {
		draft, err = h.drafter.FromURL(r.Context(), v.URL)
	} else {
		draft, err = h.drafter.FromText(r.Context(), v.Text)
	}
	if err != nil {
		status := layout.StatusFor(err)
		msg, ok := inputFailure(err)
		if status == http.StatusInternalServerError || !ok {
			layout.WriteError(w, r, fmt.Errorf("ingest: %w", err))
			return
		}
		h.log.WarnContext(r.Context(), "ingest failed", "error", err.Error(), "status", status)
		v.Error = msg
		layout.Render(w, r, status, formPage(v))
		return
	}
	token, err := h.pending.add(draft)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
}

// inputFailure is the form's wording for an input the pipeline rejected; the
// status comes from layout's table. ok is false for an error that is not about
// the input.
func inputFailure(err error) (msg string, ok bool) {
	switch {
	case errors.Is(err, ingestcore.ErrFetch):
		return "Could not fetch that page: " + err.Error(), true
	case errors.Is(err, parse.ErrEmptyText), errors.Is(err, extract.ErrEmptyInput):
		return "There was nothing to read. Enter a recipe URL or paste the recipe text.", true
	}
	return "", false
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (h *handler) review(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	d, ok := h.pending.get(token)
	if !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	layout.Render(w, r, http.StatusOK, reviewPage(reviewView{Token: token, Draft: d}))
}

// errGone is the not-found error for a draft token that is unknown, expired or
// already used.
var errGone = fmt.Errorf("that draft is no longer available, ingest it again: %w", layout.ErrNotFound)

// confirm applies the user's edits, confirms the draft and saves it. The draft
// is taken from the table first, so a second submit cannot save twice; it goes
// back if anything fails, with the user's edits.
func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("read form: %w", layout.ErrInvalid))
		return
	}
	d, ok := h.pending.take(token)
	if !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	retry := func(status int, problems []string) {
		h.pending.put(token, d)
		layout.Render(w, r, status, reviewPage(reviewView{Token: token, Draft: d, Problems: problems}))
	}
	fail := func(err error) {
		h.pending.put(token, d)
		layout.WriteError(w, r, err)
	}

	problems, err := applyEdits(r.Context(), r.PostForm, &d, h.matcher)
	if err != nil {
		fail(err)
		return
	}
	if len(problems) > 0 {
		retry(http.StatusUnprocessableEntity, problems)
		return
	}
	confirmed, err := ingestcore.Confirm(d)
	if errors.Is(err, ingestcore.ErrIncompleteDraft) {
		retry(http.StatusUnprocessableEntity, incompleteProblems(err))
		return
	}
	if err != nil {
		fail(err)
		return
	}
	saved, err := h.saver.Save(r.Context(), confirmed)
	if err != nil {
		fail(fmt.Errorf("save confirmed draft: %w", err))
		return
	}
	h.log.InfoContext(r.Context(), "recipe ingested", "recipe_id", saved.RecipeID,
		"new_ingredients", len(saved.NewIngredients), "new_units", len(saved.NewUnits))
	http.Redirect(w, r, recipePath(saved.RecipeID), http.StatusSeeOther)
}

// incompleteProblems turns Confirm's joined error into one message per
// problem, without the sentinel.
func incompleteProblems(err error) []string {
	var out []string
	for _, e := range leaves(err) {
		if errors.Is(e, ingestcore.ErrIncompleteDraft) {
			continue
		}
		if text := strings.TrimSpace(e.Error()); text != "" {
			out = append(out, strings.ToUpper(text[:1])+text[1:]+".")
		}
	}
	return out
}

// leaves flattens errors joined with errors.Join or a multi-%w Errorf into the
// errors that were joined.
func leaves(err error) []error {
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range multi.Unwrap() {
			out = append(out, leaves(e)...)
		}
		return out
	}
	return []error{err}
}

// cancel discards the draft. Nothing was saved, so there is nothing to undo.
func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.pending.discard(chi.URLParam(r, "token"))
	http.Redirect(w, r, "/ingest", http.StatusSeeOther)
}
