// Package picture serves recipe pictures, draft candidates (ADR-00039) and the
// pictures offered on a recipe's edit page (ADR-00051). The
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
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

const (
	// Pattern is where the router mounts Routes: a recipe's picture.
	Pattern = "/recipes/{id}/picture"
	// DraftsPattern is where the router mounts DraftRoutes: a draft's
	// candidates, addressed by the draft's proposal and the candidate's offer
	// ID (ADR-00052).
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
	// OfferedPictures are the pictures offered on the recipe's edit page
	// (ADR-00051).
	OfferedPictures(ctx context.Context, c apirecipes.Caller, recipeID int64) ([]apirecipes.Offered, error)
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
	return recipepath.Base(recipeID) + "/picture?v=" + url.QueryEscape(v)
}

// CandidateSrc is the address of the draft proposal's candidate whose offer
// ID is offer (ADR-00052).
func CandidateSrc(proposalID int64, offer string) string {
	return DraftsPattern + "/" + strconv.FormatInt(proposalID, 10) + "/" + url.PathEscape(offer)
}

// OfferedSrc is the address of the picture offered under offer, its ID, on
// recipe id's edit page (ADR-00051).
func OfferedSrc(recipeID int64, offer string) string {
	return recipepath.Base(recipeID) + "/picture/offered/" + url.PathEscape(offer)
}

// Routes returns the routes of a recipe's picture, relative to Pattern.
//
//	GET /             the picture, 404 when the recipe has none or is not the household's
//	GET /offered/{offer}  the picture offered on its edit page under ID offer
func Routes(d Deps) chi.Router {
	h := newHandler(d)
	r := chi.NewRouter()
	r.Get("/", h.serveRecipe)
	r.Get("/offered/{offer}", h.serveOffered)
	return r
}

// DraftRoutes returns the routes of a draft's candidates, relative to
// DraftsPattern.
//
//	GET /{proposal}/{offer}   the draft proposal's candidate under ID offer
func DraftRoutes(d Deps) chi.Router {
	h := newHandler(d)
	r := chi.NewRouter()
	r.Get("/{proposal}/{offer}", h.serveCandidate)
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
	offer := chi.URLParam(r, "offer")
	cs, err := h.Pictures.Candidates(r.Context(), caller.FromContext(r.Context()), proposal)
	if err != nil {
		notFound(w, r, fmt.Errorf("candidates of proposal %d: %w", proposal, err))
		return
	}
	c, ok := apirecipes.FindCandidate(cs, offer)
	if !ok {
		notFound(w, r, fmt.Errorf("proposal %d has no candidate %q: %w", proposal, offer, layout.ErrNotFound))
		return
	}
	h.serveReplaceable(w, r, c.Key, "proposal_id", proposal)
}

func (h *handler) serveOffered(w http.ResponseWriter, r *http.Request) {
	id, err := positive(r, "id")
	if err != nil {
		notFound(w, r, err)
		return
	}
	offer := chi.URLParam(r, "offer")
	offers, err := h.Pictures.OfferedPictures(r.Context(), caller.FromContext(r.Context()), id)
	if err != nil {
		notFound(w, r, fmt.Errorf("offered pictures of recipe %d: %w", id, err))
		return
	}
	o, ok := apirecipes.FindOffer(offers, offer)
	if !ok {
		notFound(w, r, fmt.Errorf("recipe %d offers no picture %q: %w", id, offer, layout.ErrNotFound))
		return
	}
	h.serveReplaceable(w, r, o.Key, "recipe_id", id)
}

// serveReplaceable serves a picture by an address that is not versioned, with
// the ETag of its bytes and a revalidated cache. It serves a draft's
// candidates and the pictures offered on a recipe's edit page. Their addresses
// name an offer ID that is never reused, but are not marked immutable, since
// the picture is deleted when the draft is confirmed or the form is saved.
func (h *handler) serveReplaceable(w http.ResponseWriter, r *http.Request, key, owner string, ownerID int64) {
	b, err := h.Media.Get(r.Context(), key)
	if err != nil {
		h.missing(w, r, key, owner, ownerID, err)
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
