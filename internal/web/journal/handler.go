// Package journal is the recipe journal: free-text notes, the recipe rating
// and the made-it log. It supplies routes that render the journal as an HTMX
// fragment (or, without HTMX, as a page of its own), so the recipe page can
// include it by URL without this package importing the recipe view or the
// reverse (ADR-00012). Persistence is the recipe store (ADR-00004); the author
// of a note or entry is the verified identity on the request context, never a
// form field (ADR-00003). It is mounted from internal/web.
package journal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. The journal of a recipe is the
// URL below the recipe's own page.
const Pattern = "/recipes/{id}/journal"

// maxFormBytes bounds a journal form body; a note is prose, not a document.
const maxFormBytes = 64 << 10

// Rating bounds: a rating is a whole number of stars.
const (
	minRating = 1
	maxRating = 5
)

// dateLayout is the form of the made-it date, as in the date input and the
// store.
const dateLayout = "2006-01-02"

// Journal is what the routes use from the recipe store. The rating lives on
// the recipe, so setting it is a Get then an Update, which writes a revision.
type Journal interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	Update(ctx context.Context, id int64, r recipe.Recipe) error
	AddNote(ctx context.Context, id int64, body string) (store.Note, error)
	ListNotes(ctx context.Context, id int64) ([]store.Note, error)
	SetNotePinned(ctx context.Context, id, noteID int64, pinned bool) error
	AddMadeIt(ctx context.Context, id int64, e store.MadeIt) (store.MadeIt, error)
	ListMadeIt(ctx context.Context, id int64) ([]store.MadeIt, error)
}

// Deps is what the routes need. Now is optional and defaults to time.Now.
type Deps struct {
	Journal Journal
	Now     func() time.Time
}

// Routes returns the routes of the journal, relative to Pattern.
func Routes(d Deps) chi.Router {
	if d.Now == nil {
		d.Now = time.Now
	}
	h := &handler{Deps: d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/notes", h.addNote)
	r.Post("/notes/{noteID}/pin", h.pinNote)
	r.Post("/rating", h.setRating)
	r.Post("/made", h.addMadeIt)
	return r
}

type handler struct{ Deps }

// Messages shown beside the form that failed. They are fixed text: the cause
// of a rejected input is in the log.
const (
	emptyNoteMessage = "Write something before saving the note."
	ratingMessage    = "Rating must be a whole number from 1 to 5."
	dateMessage      = "Enter the date as YYYY-MM-DD, for example 2024-03-09."
)

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	id, ok := recipeID(w, r)
	if !ok {
		return
	}
	v, err := h.load(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, v)
}

func (h *handler) addNote(w http.ResponseWriter, r *http.Request) {
	id, form, ok := h.post(w, r)
	if !ok {
		return
	}
	body := form.Get("body")
	var problem formProblem
	if strings.TrimSpace(body) == "" {
		problem = formProblem{note: emptyNoteMessage}
	} else if _, err := h.Journal.AddNote(r.Context(), id, body); err != nil {
		// The store rejects a note only for an empty body, which is handled
		// above; any other ErrInvalid is a mismatch between us and the store, so
		// it is reported as a bad request rather than as the empty-note message.
		if errors.Is(err, store.ErrInvalid) {
			err = fmt.Errorf("%w: %w", layout.ErrInvalid, err)
		}
		layout.WriteError(w, r, fmt.Errorf("add note to recipe %d: %w", id, err))
		return
	}
	h.finish(w, r, id, problem, func(v *view) { v.NoteBody = body })
}

// pinNote pins a note as a tip or unpins it: the form's pinned field is 1 or 0,
// anything else is a bad request. The note must be one of this recipe's.
func (h *handler) pinNote(w http.ResponseWriter, r *http.Request) {
	id, form, ok := h.post(w, r)
	if !ok {
		return
	}
	raw := chi.URLParam(r, "noteID")
	noteID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || noteID <= 0 {
		layout.WriteError(w, r, fmt.Errorf("note id %q: %w", raw, layout.ErrNotFound))
		return
	}
	var pinned bool
	switch choice := form.Get("pinned"); choice {
	case "1":
		pinned = true
	case "0":
	default:
		layout.WriteError(w, r, fmt.Errorf("%w: pinned must be 1 or 0, got %q", layout.ErrInvalid, choice))
		return
	}
	if err := h.Journal.SetNotePinned(r.Context(), id, noteID, pinned); err != nil {
		layout.WriteError(w, r, fmt.Errorf("pin note %d of recipe %d: %w", noteID, id, err))
		return
	}
	h.finish(w, r, id, formProblem{}, nil)
}

func (h *handler) setRating(w http.ResponseWriter, r *http.Request) {
	id, form, ok := h.post(w, r)
	if !ok {
		return
	}
	var problem formProblem
	rating, valid := parseRating(form.Get("rating"))
	if !valid {
		problem = formProblem{rating: ratingMessage}
	} else if err := h.saveRating(r.Context(), id, rating); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	h.finish(w, r, id, problem, nil)
}

// saveRating sets or clears the rating of recipe id. Update is the store's
// one way to change the recipe, and it writes a revision.
func (h *handler) saveRating(ctx context.Context, id int64, rating *int) error {
	rec, err := h.Journal.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("load recipe %d: %w", id, err)
	}
	rec.Rating = rating
	if err := h.Journal.Update(ctx, id, rec); err != nil {
		return fmt.Errorf("rate recipe %d: %w", id, err)
	}
	return nil
}

func (h *handler) addMadeIt(w http.ResponseWriter, r *http.Request) {
	id, form, ok := h.post(w, r)
	if !ok {
		return
	}
	on, rawRating, text := strings.TrimSpace(form.Get("on")), form.Get("rating"), form.Get("comment")
	var problem formProblem
	entry := store.MadeIt{Comment: text}
	if on != "" {
		t, err := time.Parse(dateLayout, on)
		if err != nil {
			problem.date = dateMessage
		}
		entry.On = t
	}
	rating, valid := parseRating(rawRating)
	if !valid {
		problem.madeRating = ratingMessage
	}
	entry.Rating = rating
	if problem == (formProblem{}) {
		if _, err := h.Journal.AddMadeIt(r.Context(), id, entry); err != nil {
			layout.WriteError(w, r, fmt.Errorf("log made-it on recipe %d: %w", id, err))
			return
		}
	}
	h.finish(w, r, id, problem, func(v *view) {
		v.MadeOn, v.MadeRating, v.MadeComment = on, rawRating, text
	})
}

// formProblem is the message of each form that rejected its input; the zero
// value means the post was accepted.
type formProblem struct{ note, rating, date, madeRating string }

// finish answers a post. An accepted post from a plain form redirects to the
// journal page (post/redirect/get); with HTMX it returns the fragment. A
// rejected one shows the journal again with the message and the typed values:
// htmx 2 does not swap a 4xx, so the fragment is a 200, and a plain form gets
// the page with 400.
func (h *handler) finish(w http.ResponseWriter, r *http.Request, id int64, p formProblem, keep func(*view)) {
	accepted := p == (formProblem{})
	if accepted && !fromHTMX(r) {
		http.Redirect(w, r, journalPath(id), http.StatusSeeOther)
		return
	}
	v, err := h.load(r.Context(), id)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	status := http.StatusOK
	if !accepted {
		v.NoteError, v.RatingError, v.DateError, v.MadeRatingError = p.note, p.rating, p.date, p.madeRating
		if keep != nil {
			keep(&v)
		}
		if !fromHTMX(r) {
			status = http.StatusBadRequest
		}
	}
	h.render(w, r, status, v)
}

// post validates the recipe id and reads the form, bounded in size.
func (h *handler) post(w http.ResponseWriter, r *http.Request) (int64, url.Values, bool) {
	id, ok := recipeID(w, r)
	if !ok {
		return 0, nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		layout.WriteError(w, r, fmt.Errorf("%w: read form: %w", layout.ErrInvalid, err))
		return 0, nil, false
	}
	return id, r.PostForm, true
}

func (h *handler) render(w http.ResponseWriter, r *http.Request, status int, v view) {
	if fromHTMX(r) {
		layout.Render(w, r, status, journalSection(v))
		return
	}
	layout.Render(w, r, status, journalPage(v))
}

// load reads everything the journal shows. The recipe is read first so a
// missing one is a 404 before anything else is asked.
func (h *handler) load(ctx context.Context, id int64) (view, error) {
	rec, err := h.Journal.Get(ctx, id)
	if err != nil {
		return view{}, fmt.Errorf("load recipe %d: %w", id, err)
	}
	notes, err := h.Journal.ListNotes(ctx, id)
	if err != nil {
		return view{}, fmt.Errorf("list notes of recipe %d: %w", id, err)
	}
	made, err := h.Journal.ListMadeIt(ctx, id)
	if err != nil {
		return view{}, fmt.Errorf("list made-it log of recipe %d: %w", id, err)
	}
	return newView(id, rec, notes, made, h.Now()), nil
}

func recipeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		layout.WriteError(w, r, fmt.Errorf("recipe id %q: %w", raw, layout.ErrNotFound))
		return 0, false
	}
	return id, true
}

// parseRating reads a rating field: empty is no rating (nil, valid), otherwise
// a whole number of stars in range. valid is false for anything else.
func parseRating(s string) (rating *int, valid bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < minRating || n > maxRating {
		return nil, false
	}
	return &n, true
}

func fromHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func journalPath(id int64) string { return "/recipes/" + strconv.FormatInt(id, 10) + "/journal" }
