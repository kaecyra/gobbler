// Package ingest is the web side of ingestion: the form that takes a URL or
// pasted text, and the review screen where the user checks, edits and confirms
// the draft before anything is saved (ADR-00006). The review screen is the
// recipe editor operating on the unsaved draft (ADR-00020 decision 3): this
// package holds no editor control of its own. It drives the pipeline in
// internal/ingest (FromURL, FromText or FromOCR, then Confirm, then Saver.Save) and
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
	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/ingest/ocr"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	"github.com/kaecyra/gobbler/internal/web/editor"
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
	// LLM selects the fallback provider and models (ADR-00007). Empty means
	// no provider: drafts below the threshold are flagged and the retry
	// action is not offered.
	LLM config.LLM
	// Provider, when set, is used instead of building one from LLM; tests
	// substitute a fake here.
	Provider ingestcore.Provider
	// Logger receives this package's logs; the default is slog.Default().
	Logger *slog.Logger
	// Thumbnails downloads the preview image of a saved recipe's video; nil
	// means YouTube's image host. Tests substitute a fake here (ADR-00021).
	Thumbnails video.ThumbnailSource
}

type handler struct {
	drafter Drafter
	saver   Saver
	// unsaved is the recipe editor over the unsaved draft.
	unsaved editor.Unsaved
	pending *pendingDrafts
	retrier Retrier
	// photos drafts from a photo's OCR text, and ocr reads the photo.
	photos PhotoDrafter
	ocr    TextReader
	// llmInitErr is why the configured provider could not be built. The rule
	// based path still works; only the retry action reports it.
	llmInitErr error
	log        *slog.Logger
	// initErr is why the pipeline could not be built. Every route still exists
	// and answers 500 with it (see requireReady), so a bad configuration is a
	// loud failure rather than a missing page.
	initErr error
}

// Routes returns the ingest routes, to be mounted at /ingest. The pipeline,
// catalog and recipe store are built over d.DB. The LLM provider comes from
// d.Provider or is selected from d.LLM; a provider that cannot be built does
// not take the rule-based path down (see handler.llmInitErr).
func Routes(d Deps) http.Handler {
	h := &handler{pending: newPendingDrafts(time.Now), log: d.Logger}
	if h.log == nil {
		h.log = slog.Default()
	}
	cat := catalog.New(d.DB)
	provider := d.Provider
	if provider == nil {
		var perr error
		provider, perr = ingestcore.SelectProvider(context.Background(), d.LLM, h.log, providerFactories())
		if perr != nil {
			h.llmInitErr = perr
			h.log.Error("llm provider unavailable", "error", perr.Error())
		}
	}
	pipeline, err := ingestcore.New(d.Ingestion, ingestcore.Deps{Catalog: cat, Provider: provider, Logger: h.log})
	if err != nil {
		h.initErr = fmt.Errorf("build ingest pipeline: %w", err)
		h.log.Error("ingest routes unavailable", "error", err.Error())
	} else {
		h.drafter = pipeline
		h.photos = pipeline
		h.ocr = ocr.Runner{}
		h.retrier = pipeline.Fallback()
		h.saver = ingestcore.NewSaver(store.New(d.DB), cat).WithThumbnails(d.Thumbnails)
		h.unsaved = editor.NewUnsaved(cat)
	}
	return h.routes()
}

func (h *handler) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.requireReady, requireHousehold)
	r.Get("/", h.form)
	r.Post("/", h.submit)
	r.Get("/photo", h.photoForm)
	r.Post("/photo", h.photoSubmit)
	r.Get("/{token}", h.review)
	r.Post("/{token}/confirm", h.confirm)
	r.Post("/{token}/retry", h.retry)
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

// requireHousehold refuses a request that carries no household with a 500,
// before any draft is created or looked up: pending drafts belong to a
// household (ADR-00018), and a route reached outside the membership middleware
// is a wiring error, never a request to serve unscoped.
func requireHousehold(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := household.FromContext(r.Context()); !ok {
			layout.WriteError(w, r, fmt.Errorf("ingest: %w", store.ErrNoHousehold))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// householdID is the id of the request's household. requireHousehold has
// already refused a request without one, so no handler sees zero.
func householdID(r *http.Request) int64 {
	h, _ := household.FromContext(r.Context())
	return h.ID
}

// formView is what the ingest form renders.
type formView struct {
	URL, Text string
	Error     string
}

// reviewView is what the review screen renders.
type reviewView struct {
	Token string
	// Draft is the draft as the rules read it, for the confidence summary.
	Draft ingestcore.Draft
	// Form is the recipe editor over the draft. Why a confirm did not go
	// through is in it.
	Form editor.Form
	// CanRetry is whether the retry-with-LLM action is offered.
	CanRetry bool
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
	token, err := h.pending.add(householdID(r), draft)
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
	d, ok := h.pending.get(householdID(r), token)
	if !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	h.renderReview(w, r, http.StatusOK, token, d, nil)
}

// reviewOptions is where the review form's buttons post.
func reviewOptions(token string) editor.ReviewOptions {
	return editor.ReviewOptions{Action: "/ingest/" + token + "/confirm"}
}

// renderReview shows d in the editor. form, when set, is the editor form to
// show instead of a fresh one (what the user typed, with its messages). A
// request from htmx gets the form alone, to swap in place.
func (h *handler) renderReview(w http.ResponseWriter, r *http.Request, status int, token string, d ingestcore.Draft, form *editor.Form) {
	if form == nil {
		f, err := h.unsaved.Open(r.Context(), d, reviewOptions(token))
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("open review: %w", err))
			return
		}
		form = &f
	}
	if editor.FromHTMX(r) {
		// htmx does not swap a 4xx, so the form alone is answered with a 200.
		layout.Render(w, r, http.StatusOK, form.Component())
		return
	}
	layout.Render(w, r, status, reviewPage(reviewView{
		Token: token, Draft: d, Form: *form,
		CanRetry: h.retrier != nil && h.retrier.Configured(),
	}))
}

// errGone is the not-found error for a draft token that is unknown, expired or
// already used.
var errGone = fmt.Errorf("that draft is no longer available, ingest it again: %w", layout.ErrNotFound)

// confirm reads the editor form posted from the review screen. A structural
// button (add, remove, move) or a form the editor rejects shows the form
// again and changes nothing. A form that passes the editor's validation is
// confirmed and saved. The draft is taken from the table before saving, so a
// second submit cannot save twice; it goes back, with the user's edits, if
// anything fails.
func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("read form: %w", layout.ErrInvalid))
		return
	}
	base, ok := h.pending.get(householdID(r), token)
	if !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	out, err := h.unsaved.Submit(r.Context(), r.PostForm, base, reviewOptions(token))
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("read review form: %w", err))
		return
	}
	if !out.Ready {
		status := http.StatusOK
		if out.Rejected {
			status = http.StatusUnprocessableEntity
		}
		h.renderReview(w, r, status, token, base, &out.Form)
		return
	}
	if _, ok := h.pending.take(householdID(r), token); !ok {
		layout.WriteError(w, r, errGone)
		return
	}
	d := out.Draft
	retry := func(problems []string) {
		h.pending.put(householdID(r), token, d)
		form := out.Form.WithProblems(problems...)
		h.renderReview(w, r, http.StatusUnprocessableEntity, token, d, &form)
	}
	fail := func(err error) {
		h.pending.put(householdID(r), token, d)
		layout.WriteError(w, r, err)
	}

	confirmed, err := ingestcore.Confirm(d)
	if errors.Is(err, ingestcore.ErrIncompleteDraft) {
		retry(incompleteProblems(err))
		return
	}
	if err != nil {
		fail(err)
		return
	}
	saved, err := h.saver.Save(r.Context(), confirmed)
	var unitErr *ingestcore.UnknownUnitError
	if errors.As(err, &unitErr) {
		// The catalog changed since drafting: a problem with the draft, not a
		// broken pipeline, so show it on the review screen.
		retry([]string{
			fmt.Sprintf("The unit %q is not in the catalog (it may have changed since this draft was made). Ingest the recipe again to re-read it.", unitErr.Name),
		})
		return
	}
	if err != nil {
		fail(fmt.Errorf("save confirmed draft: %w", err))
		return
	}
	h.log.InfoContext(r.Context(), "recipe ingested", "recipe_id", saved.RecipeID,
		"new_ingredients", len(saved.NewIngredients), "new_equipment", len(saved.NewEquipment))
	if editor.FromHTMX(r) {
		w.Header().Set("HX-Redirect", recipePath(saved.RecipeID))
		w.WriteHeader(http.StatusOK)
		return
	}
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
	h.pending.discard(householdID(r), chi.URLParam(r, "token"))
	http.Redirect(w, r, "/ingest", http.StatusSeeOther)
}
