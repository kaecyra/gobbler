// Package ingest is the web side of ingestion: the form that takes a URL or
// pasted text, and the review screen where the user checks, edits and confirms
// the draft before anything is saved (ADR-00006). The review screen is the
// recipe editor operating on the unsaved draft (ADR-00020 decision 3): this
// package holds no editor control of its own. It drives the pipeline in
// internal/ingest (FromURL, FromText or FromOCR, then Confirm, then Saver.Save) and
// never saves anything itself.
//
// A draft waits as a pending proposal (ingest.Drafts, ADR-00035) under a random
// token between ingest and confirm, so a restart does not lose it and an agent's
// draft can be reviewed the next day. Only the confirm route saves, and only
// through ingest.Confirm, so no route stores an ingested recipe without the
// review step.
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

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/ingest/ocr"
	"github.com/kaecyra/gobbler/internal/parse"
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

// Recipes is what the ingest screens need from the recipe service
// (*apirecipes.Service satisfies it): the drafts that wait for review, the save
// of a confirmed one, the start of a re-import and the picture candidates the
// picker shows. Fetching and reading a page into a draft stays here, in the
// pipeline.
type Recipes interface {
	DraftsFull(ctx context.Context, c apirecipes.Caller) (bool, error)
	AddDraft(ctx context.Context, c apirecipes.Caller, d ingestcore.Draft, by apirecipes.Provenance) (string, error)
	GetDraft(ctx context.Context, c apirecipes.Caller, token string) (ingestcore.Draft, error)
	ReplaceDraft(ctx context.Context, c apirecipes.Caller, token string, d ingestcore.Draft) error
	DiscardDraft(ctx context.Context, c apirecipes.Caller, token string) error
	ChoosePicture(ctx context.Context, c apirecipes.Caller, token string, choice ingestcore.PictureChoice) error
	SaveProposed(ctx context.Context, c apirecipes.Caller, token string, conf ingestcore.Confirmed) (ingestcore.Saved, error)
	// StartReimport drafts the recipe again from its source with drafter
	// (ADR-00042).
	StartReimport(ctx context.Context, c apirecipes.Caller, id int64, drafter ingestcore.URLDrafter, by apirecipes.Provenance) (string, error)
	// PictureKey finds the picture a re-import's "Keep current picture" keeps;
	// Candidates are the draft's picture candidates.
	PictureKey(ctx context.Context, c apirecipes.Caller, recipeID int64) (string, bool, error)
	Candidates(ctx context.Context, c apirecipes.Caller, proposalID int64) ([]apirecipes.Candidate, error)
}

// Deps is what the ingest routes need to build the pipeline.
type Deps struct {
	// DB is the catalog's database; the recipes are reached through Recipes.
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
	// Recipes holds the drafts that wait for review and saves a confirmed one.
	// Sharing one service with the MCP endpoint is what lets an agent's draft
	// open here (ADR-00034). It is required: without it every route answers
	// 500 (see requireReady).
	Recipes Recipes
}

type handler struct {
	drafter Drafter
	// unsaved is the recipe editor over the unsaved draft.
	unsaved editor.Unsaved
	recipes Recipes
	// photoCache holds the photos behind OCR drafts, in memory only.
	photoCache *ingestcore.Photos
	retrier    Retrier
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

// Routes returns the ingest routes, to be mounted at /ingest. The pipeline
// and catalog are built over d.DB; drafts and saved recipes are d.Recipes. The LLM provider comes from
// d.Provider or is selected from d.LLM; a provider that cannot be built does
// not take the rule-based path down (see handler.llmInitErr).
func Routes(d Deps) http.Handler {
	h := &handler{recipes: d.Recipes, photoCache: ingestcore.NewPhotos(time.Now), log: d.Logger}
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
	if err == nil && d.Recipes == nil {
		err = errors.New("the recipe service is required")
	}
	if err != nil {
		h.initErr = fmt.Errorf("build ingest pipeline: %w", err)
		h.log.Error("ingest routes unavailable", "error", err.Error())
	} else {
		h.drafter = pipeline
		h.photos = pipeline
		h.ocr = ocr.Runner{}
		h.retrier = pipeline.Fallback()
		h.unsaved = editor.NewUnsaved(cat)
	}
	return h.routes()
}

func (h *handler) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.requireReady, requireHousehold)
	r.Get("/", h.form)
	r.Post("/", h.submit)
	r.Post("/reimport/{id}", h.reimport)
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
			layout.WriteError(w, r, fmt.Errorf("ingest: %w", apirecipes.ErrNoHousehold))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// formView is what the ingest form renders.
type formView struct {
	URL, Text string
	Error     string
	// Source is the field the page asked for with ?source=; empty is the link.
	Source string
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
	// PhotoGone is set for a draft read from a photo that has left memory
	// (ADR-00035): the photo is never stored.
	PhotoGone bool
	// Picker is the picture picker above the editor.
	Picker pickerView
	// Reimport is set when the draft replaces a saved recipe: the review page
	// then carries the callout saying so.
	Reimport *ingestcore.Reimport
}

func (h *handler) form(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, formPage(formView{Source: r.URL.Query().Get("source")}))
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

	if h.full(w, r, func(msg string) {
		v.Error = msg
		layout.Render(w, r, http.StatusConflict, formPage(v))
	}) {
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
	token, ok := h.hold(w, r, draft, func(msg string) {
		v.Error = msg
		layout.Render(w, r, http.StatusConflict, formPage(v))
	})
	if !ok {
		return
	}
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
}

// full answers a household that already holds the most drafts it may before any
// work is spent on one: no fetch, no pipeline, no LLM call. refuse is called
// with the message to show. It reports whether the request has been answered.
// hold's check, made where the draft is written, stays the authority.
func (h *handler) full(w http.ResponseWriter, r *http.Request, refuse func(msg string)) bool {
	full, err := h.recipes.DraftsFull(r.Context(), caller.FromContext(r.Context()))
	switch {
	case err != nil:
		layout.WriteError(w, r, err)
		return true
	case full:
		refuse(sentence(apirecipes.ErrDraftLimit.Error()))
		return true
	}
	return false
}

// hold stores draft for review and returns its token. A household that already
// holds the most drafts it may is refused: refuse is called with the message to
// show, and nothing is dropped to make room (ADR-00035). Any other failure is
// answered here. ok is false when a response has been written.
func (h *handler) hold(w http.ResponseWriter, r *http.Request, draft ingestcore.Draft, refuse func(msg string)) (token string, ok bool) {
	email, found := auth.EmailFromContext(r.Context())
	if !found {
		layout.WriteError(w, r, fmt.Errorf("hold draft: %w", apirecipes.ErrNoIdentity))
		return "", false
	}
	token, err := h.recipes.AddDraft(r.Context(), caller.FromContext(r.Context()), draft, apirecipes.Provenance{Person: email})
	switch {
	case errors.Is(err, apirecipes.ErrDraftLimit):
		refuse(sentence(apirecipes.ErrDraftLimit.Error()))
		return "", false
	case err != nil:
		layout.WriteError(w, r, err)
		return "", false
	}
	return token, true
}

// sentence capitalises msg and ends it with a full stop, for the page.
func sentence(msg string) string {
	if msg == "" {
		return msg
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
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
	d, ok := h.draft(w, r, token)
	if !ok {
		return
	}
	h.renderReview(w, r, http.StatusOK, token, d, nil)
}

// draft reads the pending draft for token, with its photo if one is still in
// memory. A token that names no pending draft of the household is the same
// not-found as an unknown one. ok is false when a response has been written.
func (h *handler) draft(w http.ResponseWriter, r *http.Request, token string) (ingestcore.Draft, bool) {
	d, err := h.recipes.GetDraft(r.Context(), caller.FromContext(r.Context()), token)
	switch {
	case errors.Is(err, ingestcore.ErrDraftGone):
		layout.WriteError(w, r, errGone)
		return ingestcore.Draft{}, false
	case err != nil:
		layout.WriteError(w, r, err)
		return ingestcore.Draft{}, false
	}
	if image, mime, found := h.photoCache.Get(token); found {
		d.Source.Image, d.Source.ImageMIME = image, mime
	}
	return d, true
}

// reviewOptions is where the review form's buttons post, and what the parts
// panel beside it can say about d's source. The panel sits under the source
// box, so it is shown beside the form only when there is a source box.
func reviewOptions(token string, d ingestcore.Draft) editor.ReviewOptions {
	rows := sourceRows(d)
	headings := 0
	for _, r := range rows {
		if r.Heading {
			headings++
		}
	}
	return editor.ReviewOptions{Action: "/ingest/" + token + "/confirm", SourceParts: headings, PartsBeside: len(rows) > 0}
}

// renderReview shows d in the editor. form, when set, is the editor form to
// show instead of a fresh one (what the user typed, with its messages). A
// request from htmx gets the form alone, to swap in place, with the parts
// panel beside it out of band.
func (h *handler) renderReview(w http.ResponseWriter, r *http.Request, status int, token string, d ingestcore.Draft, form *editor.Form) {
	if form == nil {
		f, err := h.unsaved.Open(r.Context(), d, reviewOptions(token, d))
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("open review: %w", err))
			return
		}
		form = &f
	}
	if editor.FromHTMX(r) {
		// htmx does not swap a 4xx, so the form alone is answered with a 200.
		layout.Render(w, r, http.StatusOK, form.Fragment())
		return
	}
	picker, err := h.picker(r.Context(), d)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("open review: %w", err))
		return
	}
	layout.Render(w, r, status, reviewPage(reviewView{
		Token: token, Draft: d, Form: *form, Picker: picker, Reimport: d.Reimport,
		CanRetry: h.retrier != nil && h.retrier.Configured(),
		// A draft read from a photo whose photo is no longer in memory:
		// asking the LLM again reads the recognised text alone.
		PhotoGone: d.Source.Kind == apirecipes.SnapshotOCR && len(d.Source.Image) == 0,
	}))
}

// errGone is the not-found error for a draft token that is unknown, expired or
// already used.
var errGone = fmt.Errorf("that draft is no longer available, ingest it again: %w", layout.ErrNotFound)

// confirm reads the editor form posted from the review screen. A structural
// button (add, remove, move) or a form the editor rejects shows the form
// again and changes nothing. A form that passes the editor's validation is
// confirmed and saved. Saving marks the draft accepted in the same transaction
// as the recipe, so a second submit cannot save twice; when saving fails the
// draft stays pending, with the user's edits.
func (h *handler) confirm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("read form: %w", layout.ErrInvalid))
		return
	}
	base, ok := h.draft(w, r, token)
	if !ok {
		return
	}
	// The choice rides in the editor's form (the picker's radios belong to it),
	// so it is kept with the draft even when the form is shown again.
	if err := h.choosePicture(r.Context(), token, r.PostForm.Get(fieldPicture), &base); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	out, err := h.unsaved.Submit(r.Context(), r.PostForm, base, reviewOptions(token, base))
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
	d := out.Draft
	// keep stores the user's edits, so the draft is as they left it when it
	// cannot be saved. A draft that is gone is the same not-found as above.
	keep := func() bool {
		err := h.recipes.ReplaceDraft(r.Context(), caller.FromContext(r.Context()), token, d)
		switch {
		case errors.Is(err, ingestcore.ErrDraftGone):
			layout.WriteError(w, r, errGone)
		case err != nil:
			layout.WriteError(w, r, err)
		default:
			return true
		}
		return false
	}
	retry := func(problems []string) {
		if !keep() {
			return
		}
		form := out.Form.WithProblems(problems...)
		h.renderReview(w, r, http.StatusUnprocessableEntity, token, d, &form)
	}
	fail := func(err error) {
		if keep() {
			layout.WriteError(w, r, err)
		}
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
	saved, err := h.recipes.SaveProposed(r.Context(), caller.FromContext(r.Context()), token, confirmed)
	if errors.Is(err, apirecipes.ErrStaleVersion) && d.Reimport != nil {
		// The recipe was edited after the re-import started. Nothing was
		// saved; the person's edits to the draft are kept while they decide.
		retry([]string{staleReimport})
		return
	}
	var unitErr *ingestcore.UnknownUnitError
	if errors.As(err, &unitErr) {
		// The catalog changed since drafting: a problem with the draft, not a
		// broken pipeline, so show it on the review screen.
		retry([]string{
			fmt.Sprintf("The unit %q is not in the catalog (it may have changed since this draft was made). Ingest the recipe again to re-read it.", unitErr.Name),
		})
		return
	}
	if errors.Is(err, apirecipes.ErrNotPending) {
		// Confirmed or discarded by another request since this one began.
		layout.WriteError(w, r, errGone)
		return
	}
	if err != nil {
		fail(fmt.Errorf("save confirmed draft: %w", err))
		return
	}
	h.photoCache.Drop(token)
	h.log.InfoContext(r.Context(), "recipe ingested", "recipe_id", saved.RecipeID,
		"new_ingredients", len(saved.NewIngredients), "new_equipment", len(saved.NewEquipment))
	if editor.FromHTMX(r) {
		w.Header().Set("HX-Redirect", recipePath(saved.RecipeID))
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, recipePath(saved.RecipeID), http.StatusSeeOther)
}

// staleReimport is shown on the review screen when the recipe changed after the
// re-import started.
const staleReimport = "The recipe changed since this re-import was made, so it cannot be saved. Discard it and re-import again."

// reimport fetches a saved recipe's source again, drafts it with the whole
// ingest pipeline and lands on the review screen. A fetch or pipeline failure
// is shown and no draft is held (ADR-00042). Nothing is saved.
func (h *handler) reimport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		layout.WriteError(w, r, fmt.Errorf("recipe %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound))
		return
	}
	email, found := auth.EmailFromContext(r.Context())
	if !found {
		layout.WriteError(w, r, fmt.Errorf("re-import: %w", apirecipes.ErrNoIdentity))
		return
	}
	token, err := h.recipes.StartReimport(r.Context(), caller.FromContext(r.Context()), id, h.drafter, apirecipes.Provenance{Person: email})
	if err != nil {
		if msg, ok := inputFailure(err); ok && layout.StatusFor(err) != http.StatusInternalServerError {
			status := layout.StatusFor(err)
			h.log.WarnContext(r.Context(), "re-import failed", "recipe_id", id, "error", err.Error(), "status", status)
			layout.Render(w, r, status, layout.ErrorPage(status, msg+" The recipe is unchanged."))
			return
		}
		layout.WriteError(w, r, fmt.Errorf("re-import recipe %d: %w", id, err))
		return
	}
	http.Redirect(w, r, "/ingest/"+token, http.StatusSeeOther)
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
// Another household's token, or one already ended, is discarded as nothing: the
// answer is the same for every caller.
func (h *handler) cancel(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	// A discarded re-import goes back to its recipe, not to the add form.
	back := "/ingest"
	if d, err := h.recipes.GetDraft(r.Context(), caller.FromContext(r.Context()), token); err == nil && d.Reimport != nil {
		back = recipePath(d.Reimport.RecipeID)
	}
	if err := h.recipes.DiscardDraft(r.Context(), caller.FromContext(r.Context()), token); err != nil && !errors.Is(err, ingestcore.ErrDraftGone) {
		layout.WriteError(w, r, err)
		return
	}
	h.photoCache.Drop(token)
	http.Redirect(w, r, back, http.StatusSeeOther)
}
