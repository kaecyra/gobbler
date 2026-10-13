package operator

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// PromotionPattern is where the router mounts PromotionRoutes. It sits under
// Pattern, which layout.OperatorPaths lists, so only an operator reaches it.
const PromotionPattern = Pattern + "/promotions"

// Promoter is what the promotion pages use from the catalog store.
// *catalog.Store satisfies it.
type Promoter interface {
	ListSuggestedIngredients(ctx context.Context) ([]catalog.Ingredient, error)
	PreviewPromotion(ctx context.Context, id int64) (catalog.Promotion, error)
	PromoteIngredient(ctx context.Context, id int64) (catalog.Promotion, error)
}

// PromotionDeps is what the promotion routes need.
type PromotionDeps struct {
	Catalog Promoter
}

type promotionHandler struct{ PromotionDeps }

// PromotionRoutes returns the review of overlay entries households suggested
// for the central catalog (ADR-00018), relative to PromotionPattern. It shows
// the entry and how many duplicates a promotion merges, never which household
// suggested it or owns a duplicate.
func PromotionRoutes(d PromotionDeps) chi.Router {
	h := &promotionHandler{d}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{id}", h.review)
	r.Post("/{id}", h.promote)
	return r
}

func (h *promotionHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	suggested, err := h.Catalog.ListSuggestedIngredients(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, promotionsPage(newPromotionsView(suggested)))
}

func (h *promotionHandler) review(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	id, ok := promotionID(w, r)
	if !ok {
		return
	}
	p, err := h.Catalog.PreviewPromotion(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, reviewPage(newReviewView(p)))
}

func (h *promotionHandler) promote(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorEmail(w, r); !ok {
		return
	}
	id, ok := promotionID(w, r)
	if !ok {
		return
	}
	if _, err := h.Catalog.PromoteIngredient(r.Context(), id); err != nil {
		fail(w, r, err)
		return
	}
	http.Redirect(w, r, PromotionPattern, http.StatusSeeOther)
}

func promotionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		fail(w, r, fmt.Errorf("ingredient id %q: %w", raw, layout.ErrInvalid))
		return 0, false
	}
	return id, true
}

type promotionsView struct {
	Entries []suggestedView
}

type suggestedView struct {
	ID      int64
	Name    string
	Aisle   string
	Aliases string
	Path    string
}

func newPromotionsView(in []catalog.Ingredient) promotionsView {
	v := promotionsView{Entries: make([]suggestedView, 0, len(in))}
	for _, i := range in {
		v.Entries = append(v.Entries, suggestedView{
			ID: i.ID, Name: i.Name, Aisle: i.Aisle, Aliases: strings.Join(i.Aliases, ", "),
			Path: PromotionPattern + "/" + strconv.FormatInt(i.ID, 10),
		})
	}
	return v
}

type reviewView struct {
	suggestedView
	Duplicates int
}

func newReviewView(p catalog.Promotion) reviewView {
	return reviewView{
		suggestedView: newPromotionsView([]catalog.Ingredient{p.Ingredient}).Entries[0],
		Duplicates:    p.Duplicates,
	}
}
