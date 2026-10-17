// Package sharing is the web side of sending a recipe copy to another household
// (ADR-00018, Sharing): the form that sends a recipe, the confirmation, and the
// household's inbox where any member accepts or declines what was sent to it.
package sharing

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes.
const Pattern = "/shares"

// Service is what the pages use from internal/sharing; *sharing.Service
// satisfies it.
type Service interface {
	Send(ctx context.Context, id int64, recipientEmail string) error
	Inbox(ctx context.Context) ([]store.ShareOffer, error)
	Accept(ctx context.Context, id int64) (int64, error)
	Decline(ctx context.Context, id int64) error
}

// Recipes reads the recipe a send form is about and keeps the thumbnail of an
// accepted copy's video (ADR-00021); *store.Store satisfies it.
type Recipes interface {
	Get(ctx context.Context, id int64) (recipe.Recipe, error)
	video.ThumbnailStore
}

// Deps is what the routes need.
type Deps struct {
	Shares  Service
	Recipes Recipes
	// Thumbnails downloads the preview image of an accepted recipe's video;
	// nil means YouTube's image host.
	Thumbnails video.ThumbnailSource
}

type handler struct{ Deps }

// Routes returns the routes of the sharing pages, relative to Pattern. The
// household acted for is the one the membership middleware stored on the
// request, never one the request names.
func Routes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/", h.inbox)
	r.Get("/new/{recipeID}", h.form)
	r.Post("/new/{recipeID}", h.send)
	r.Get("/sent", h.sent)
	r.Post("/{id}/accept", h.accept)
	r.Post("/{id}/decline", h.decline)
	return r
}

func idParam(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s %q: %w", name, chi.URLParam(r, name), layout.ErrNotFound)
	}
	return id, nil
}

func (h *handler) inbox(w http.ResponseWriter, r *http.Request) {
	offers, err := h.Shares.Inbox(r.Context())
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, inboxPage(newInboxView(offers)))
}

func (h *handler) form(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "recipeID")
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	rec, err := h.Recipes.Get(r.Context(), id)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, sendPage(sendView{RecipeID: id, RecipeName: rec.Name}))
}

// send answers every address alike: a member of another household, a stranger
// and a member of the sender's own household all land on the same page, so the
// form cannot be used to find out who is a member.
func (h *handler) send(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "recipeID")
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	if err := h.Shares.Send(r.Context(), id, r.PostFormValue("email")); err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern+"/sent", http.StatusSeeOther)
}

func (h *handler) sent(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, sentPage())
}

func (h *handler) accept(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	recipeID, err := h.Shares.Accept(r.Context(), id)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	// The copy can carry a video this household has no thumbnail for
	// (ADR-00021 decision 3); the accept is already done.
	if rec, err := h.Recipes.Get(r.Context(), recipeID); err != nil {
		slog.WarnContext(r.Context(), "accepted recipe left without a video thumbnail check", "recipe_id", recipeID, "error", err.Error())
	} else {
		video.KeepThumbnail(r.Context(), h.Recipes, h.Thumbnails, recipeID, rec.VideoID)
	}
	http.Redirect(w, r, "/recipes/"+strconv.FormatInt(recipeID, 10), http.StatusSeeOther)
}

func (h *handler) decline(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	if err := h.Shares.Decline(r.Context(), id); err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}

// timeLayout is how a date is shown; UTC, so the page does not depend on the
// server's zone.
const timeLayout = "2006-01-02"

type offerView struct {
	ID          int64
	RecipeName  string
	Sender      string
	Sent        string
	AcceptPath  string
	DeclinePath string
}

type inboxView struct{ Offers []offerView }

func newInboxView(offers []store.ShareOffer) inboxView {
	v := inboxView{Offers: make([]offerView, 0, len(offers))}
	for _, o := range offers {
		base := Pattern + "/" + strconv.FormatInt(o.ID, 10)
		v.Offers = append(v.Offers, offerView{
			ID: o.ID, RecipeName: o.RecipeName, Sender: o.SenderName, Sent: o.SentAt.In(time.UTC).Format(timeLayout),
			AcceptPath: base + "/accept", DeclinePath: base + "/decline",
		})
	}
	return v
}

type sendView struct {
	RecipeID   int64
	RecipeName string
}

func (v sendView) Action() string { return Pattern + "/new/" + strconv.FormatInt(v.RecipeID, 10) }
