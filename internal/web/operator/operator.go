package operator

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. It is also listed in
// layout.OperatorPaths, which is what keeps everyone but an operator out.
const Pattern = "/operator"

// Households is what the pages use from the household store.
// *household.Store satisfies it.
type Households interface {
	Invite(ctx context.Context, in household.Invite) (int64, error)
	Revoke(ctx context.Context, id int64, issuedBy string) error
	PendingHouseholdInvitations(ctx context.Context, issuer string) ([]household.Invitation, error)
}

// Deps is what the routes need.
type Deps struct {
	Households Households
}

type handler struct{ Deps }

// Routes returns the operator's routes, relative to Pattern.
func Routes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/invitations", h.invite)
	r.Post("/invitations/{id}/revoke", h.revoke)
	return r
}

// operatorEmail is the verified email of an operator's request. Package web
// has already refused everyone else; the check here is the second lock, so a
// route mounted outside the operator paths by mistake still refuses.
func operatorEmail(w http.ResponseWriter, r *http.Request) (string, bool) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok || !layout.IsOperator(r.Context()) {
		layout.Render(w, r, http.StatusForbidden, layout.ErrorPage(http.StatusForbidden, "This page is for the operator."))
		return "", false
	}
	return email, true
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	email, ok := operatorEmail(w, r)
	if !ok {
		return
	}
	pending, err := h.Households.PendingHouseholdInvitations(r.Context(), email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, invitationsPage(newInvitationsView(pending)))
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	email, ok := operatorEmail(w, r)
	if !ok {
		return
	}
	_, err := h.Households.Invite(r.Context(), household.Invite{
		Kind: household.KindHousehold, Email: r.PostFormValue("email"), IssuedBy: email,
	})
	redirect(w, r, err)
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	email, ok := operatorEmail(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		layout.WriteError(w, r, fmt.Errorf("invitation id %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound))
		return
	}
	redirect(w, r, h.Households.Revoke(r.Context(), id, email))
}

func redirect(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}
