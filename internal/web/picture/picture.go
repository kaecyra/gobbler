// Package picture serves recipe pictures and draft candidates (ADR-00039). The
// app is the only way to a picture: every byte comes from the media store
// through these handlers, after the same admission as the page that shows it
// (ADR-00018), the way avatars are served (ADR-00033). Nothing points the
// browser at the media store.
//
// A request is scoped to the household on its context. A recipe or a draft of
// another household answers 404, exactly as one that does not exist.
package picture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

const (
	// Pattern is where the router mounts Routes: a recipe's picture.
	Pattern = "/recipes/{id}/picture"
	// DraftsPattern is where the router mounts DraftRoutes: a draft's
	// candidates, addressed by the draft's proposal and the candidate's
	// position.
	DraftsPattern = "/pictures/drafts"

	// immutable is for a picture asked for by its own version: the version is
	// new each time the picture changes, so the bytes at that address never do.
	immutable = "private, max-age=31536000, immutable"
	// revalidate is for everything else; the browser asks, and a matching
	// ETag costs no body.
	revalidate = "private, no-cache"
)

// Pictures is what the handlers use from the recipe service
// (ADR-00043), which checks the caller's membership.
// *apirecipes.Service satisfies it.
type Pictures interface {
	PictureKey(ctx context.Context, c apirecipes.Caller, recipeID int64) (string, bool, error)
	Candidates(ctx context.Context, c apirecipes.Caller, proposalID int64) ([]apirecipes.Candidate, error)
}

// Deps is what the routes need.
type Deps struct {
	Pictures Pictures
	// Media is the store the service writes to. *media.Local and *media.S3
	// satisfy it.
	Media interface {
		Get(ctx context.Context, key string) ([]byte, error)
	}
	// Logger receives the missing-object warning; nil means slog.Default().
	Logger *slog.Logger
}

type handler struct {
	Deps
	log *slog.Logger
}

func newHandler(d Deps) *handler {
	l := d.Logger
	if l == nil {
		l = slog.Default()
	}
	return &handler{Deps: d, log: l}
}

// Src is the address of recipe id's picture. The key is the picture's
// reference (recipes/<id>/<version>.webp); its version alone goes in the query,
// so a new picture is a new address and no cache keeps showing the old one.
func Src(recipeID int64, key string) string {
	v := strings.TrimSuffix(path.Base(key), ".webp")
	return "/recipes/" + strconv.FormatInt(recipeID, 10) + "/picture?v=" + url.QueryEscape(v)
}

// CandidateSrc is the address of candidate position of the draft proposal.
func CandidateSrc(proposalID int64, position int) string {
	return DraftsPattern + "/" + strconv.FormatInt(proposalID, 10) + "/" + strconv.Itoa(position)
}

// Routes returns the routes of a recipe's picture, relative to Pattern.
//
//	GET /   the picture, 404 when the recipe has none or is not the household's
func Routes(d Deps) chi.Router {
	h := newHandler(d)
	r := chi.NewRouter()
	r.Get("/", h.serveRecipe)
	return r
}

// DraftRoutes returns the routes of a draft's candidates, relative to
// DraftsPattern.
//
//	GET /{proposal}/{n}   candidate n of the draft proposal
func DraftRoutes(d Deps) chi.Router {
	h := newHandler(d)
	r := chi.NewRouter()
	r.Get("/{proposal}/{n}", h.serveCandidate)
	return r
}

func positive(r *http.Request, name string) (int64, error) {
	raw := chi.URLParam(r, name)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s %q: %w", name, raw, layout.ErrNotFound)
	}
	return n, nil
}

func (h *handler) serveRecipe(w http.ResponseWriter, r *http.Request) {
	id, err := positive(r, "id")
	if err != nil {
		notFound(w, r, err)
		return
	}
	key, ok, err := h.Pictures.PictureKey(r.Context(), caller.FromContext(r.Context()), id)
	if err != nil {
		notFound(w, r, fmt.Errorf("picture of recipe %d: %w", id, err))
		return
	}
	if !ok {
		notFound(w, r, fmt.Errorf("recipe %d has no picture: %w", id, layout.ErrNotFound))
		return
	}
	etag := `"` + key + `"`
	cache := revalidate
	if r.URL.Query().Get("v") == strings.TrimSuffix(path.Base(key), ".webp") {
		cache = immutable
	}
	if notModified(w, r, etag, cache) {
		return
	}
	b, err := h.Media.Get(r.Context(), key)
	if err != nil {
		h.missing(w, r, key, "recipe_id", id, err)
		return
	}
	writeImage(w, b)
}

func (h *handler) serveCandidate(w http.ResponseWriter, r *http.Request) {
	proposal, err := positive(r, "proposal")
	if err != nil {
		notFound(w, r, err)
		return
	}
	n, err := positive(r, "n")
	if err != nil {
		notFound(w, r, err)
		return
	}
	cs, err := h.Pictures.Candidates(r.Context(), caller.FromContext(r.Context()), proposal)
	if err != nil {
		notFound(w, r, fmt.Errorf("candidates of proposal %d: %w", proposal, err))
		return
	}
	var key string
	for _, c := range cs {
		if int64(c.Position) == n {
			key = c.Key
		}
	}
	if key == "" {
		notFound(w, r, fmt.Errorf("proposal %d has no candidate %d: %w", proposal, n, layout.ErrNotFound))
		return
	}
	// A draft's candidates are replaced under the same keys when they are
	// found again, so the address is not immutable: the ETag is the bytes'.
	b, err := h.Media.Get(r.Context(), key)
	if err != nil {
		h.missing(w, r, key, "proposal_id", proposal, err)
		return
	}
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	if notModified(w, r, etag, revalidate) {
		return
	}
	writeImage(w, b)
}

// missing answers a picture whose object the store does not hold. The page
// turns the 404 into the placeholder; the fault is the operator's, so it is
// logged with the key. Any other failure of the store is a 500.
func (h *handler) missing(w http.ResponseWriter, r *http.Request, key, owner string, ownerID int64, err error) {
	if errors.Is(err, media.ErrNotFound) {
		h.log.WarnContext(r.Context(), "picture object is missing from the media store",
			"key", key, owner, ownerID, "error", err.Error())
		notFound(w, r, fmt.Errorf("picture object %q: %w", key, layout.ErrNotFound))
		return
	}
	failed(w)
	layout.WriteError(w, r, fmt.Errorf("read picture object %q: %w", key, err))
}

// notFound answers 404 and keeps the answer out of every cache: the picture
// may be set a moment later.
func notFound(w http.ResponseWriter, r *http.Request, err error) {
	failed(w)
	layout.WriteError(w, r, err)
}

// failed clears the validators and cache lifetime a handler set before it knew
// the bytes were readable: an error must never be cached, least of all as
// immutable for a year at a versioned address.
func failed(w http.ResponseWriter) {
	w.Header().Del("ETag")
	w.Header().Set("Cache-Control", "no-store")
}

// notModified sets the validators and answers 304 when the browser holds the
// bytes. It reports whether it answered.
func notModified(w http.ResponseWriter, r *http.Request, etag, cache string) bool {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cache)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

func writeImage(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b) // the client went away if this fails; there is nobody to tell
}

// Households is the household store with deletion that also deletes the
// household's picture objects (ADR-00039). It embeds the store, so it is a
// drop-in for the members page, whose Delete it replaces.
type Households struct {
	*apihouseholds.Store
	// Reaper is *picture.Reaper.
	Reaper apihouseholds.PictureReaper
}

// Delete deletes the household and, after the delete commits, its pictures.
func (h Households) Delete(ctx context.Context, householdID int64, by, confirm string) error {
	return h.DeleteWithPictures(ctx, householdID, by, confirm, h.Reaper)
}
