package operator

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is where the router mounts Routes. It is also listed in
// layout.OperatorPaths, which is what keeps everyone but an operator out.
const Pattern = "/operator"

// Households is what the pages call: the Operator* methods of the household
// service (ADR-00043), which check the operator and not membership.
// *households.Service satisfies it.
type Households interface {
	OperatorInvite(ctx context.Context, c apihouseholds.Caller, email string) (string, error)
	OperatorRevoke(ctx context.Context, c apihouseholds.Caller, code string) error
	OperatorPending(ctx context.Context, c apihouseholds.Caller) ([]apihouseholds.Invitation, error)
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
	r.Post("/invitations/{"+layout.InvitationCodeParam+"}/revoke", h.revoke)
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

// operatorCaller is the Caller of an operator's request: the verified email and
// no household, since an operator need not be a member of one. operatorEmail
// is the second lock; the service checks the operator a third time.
func operatorCaller(w http.ResponseWriter, r *http.Request) (apihouseholds.Caller, bool) {
	email, ok := operatorEmail(w, r)
	if !ok {
		return apihouseholds.Caller{}, false
	}
	return apihouseholds.Caller{Person: email}, true
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	c, ok := operatorCaller(w, r)
	if !ok {
		return
	}
	pending, err := h.Households.OperatorPending(r.Context(), c)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, invitationsPage(invitationsView{Pending: layout.NewPendingInvitations(c.Person, pending)}))
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	c, ok := operatorCaller(w, r)
	if !ok {
		return
	}
	_, err := h.Households.OperatorInvite(r.Context(), c, r.PostFormValue("email"))
	redirect(w, r, err)
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	c, ok := operatorCaller(w, r)
	if !ok {
		return
	}
	code, err := layout.InvitationCode(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	redirect(w, r, h.Households.OperatorRevoke(r.Context(), c, code))
}

func redirect(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}
