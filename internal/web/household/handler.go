package household

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	domain "github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Patterns where the router mounts MemberRoutes and InvitationRoutes.
const (
	Pattern            = "/household"
	InvitationsPattern = "/invitations"
)

// Store is what the pages use from the household store. *household.Store
// satisfies it.
type Store interface {
	Members(ctx context.Context, householdID int64) ([]string, error)
	PendingForHousehold(ctx context.Context, householdID int64) ([]domain.Invitation, error)
	Invite(ctx context.Context, in domain.Invite) (int64, error)
	Revoke(ctx context.Context, id int64, issuedBy string) error
	Remove(ctx context.Context, householdID int64, by, target string) error
	Leave(ctx context.Context, email string) error
	Delete(ctx context.Context, householdID int64, by, confirm string) error
	InvitationFor(ctx context.Context, id int64, email string) (domain.Invitation, error)
	Accept(ctx context.Context, id int64, email, householdName string) (domain.Household, error)
}

// Deps is what the routes need.
type Deps struct {
	Households Store
}

type handler struct{ Deps }

// MemberRoutes returns the routes of the members page, relative to Pattern.
// The household acted in is always the one the membership middleware stored on
// the request, never one the request names (ADR-00018).
func MemberRoutes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/", h.show)
	r.Post("/invitations", h.invite)
	r.Post("/invitations/{id}/revoke", h.revoke)
	r.Post("/members/remove", h.remove)
	r.Post("/leave", h.leave)
	r.Post("/delete", h.delete)
	return r
}

// InvitationRoutes returns the routes for opening and accepting an invitation,
// relative to InvitationsPattern. A verified non-member can reach them.
func InvitationRoutes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/{id}", h.showInvitation)
	r.Post("/{id}/accept", h.accept)
	return r
}

// actor is the verified email and the household of a member's request. A
// request without both never reaches a handler (the membership middleware), so
// a missing one is refused rather than guessed at.
func actor(r *http.Request) (email string, hh domain.Household, err error) {
	email, ok := auth.EmailFromContext(r.Context())
	hh, inHousehold := domain.FromContext(r.Context())
	if !ok || !inHousehold {
		return "", domain.Household{}, fmt.Errorf("no member on the request: %w", layout.ErrNotFound)
	}
	return email, hh, nil
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invitation id %q: %w", chi.URLParam(r, "id"), layout.ErrNotFound)
	}
	return id, nil
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	email, hh, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	members, err := h.Households.Members(r.Context(), hh.ID)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	pending, err := h.Households.PendingForHousehold(r.Context(), hh.ID)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, membersPage(pageView{
		Name: hh.Name, Self: email, Members: members, Pending: newPendingViews(email, pending),
	}))
}

// done sends the browser back to the members page after a change.
func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	email, hh, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	_, err = h.Households.Invite(r.Context(), domain.Invite{
		Kind: domain.KindJoin, Email: r.PostFormValue("email"), HouseholdID: hh.ID, IssuedBy: email,
	})
	done(w, r, err)
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	email, _, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	id, err := idParam(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	done(w, r, h.Households.Revoke(r.Context(), id, email))
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	email, hh, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	done(w, r, h.Households.Remove(r.Context(), hh.ID, email, r.PostFormValue("email")))
}

func (h *handler) leave(w http.ResponseWriter, r *http.Request) {
	email, _, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if err := h.Households.Leave(r.Context(), email); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	// Having left, the reader is a non-member: the home page says so.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	email, hh, err := actor(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if err := h.Households.Delete(r.Context(), hh.ID, email, r.PostFormValue("confirm")); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) showInvitation(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	id, err := idParam(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	inv, err := h.Households.InvitationFor(r.Context(), id, email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	_, member := domain.FromContext(r.Context())
	layout.Render(w, r, http.StatusOK, acceptPage(acceptView{
		ID: inv.ID, Self: email, Kind: inv.Kind, Household: inv.HouseholdName,
		IssuedBy: inv.IssuedBy, Expires: inv.ExpiresAt.UTC().Format(timeLayout), Member: member,
	}))
}

func (h *handler) accept(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		layout.WriteError(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	id, err := idParam(r)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	if _, err := h.Households.Accept(r.Context(), id, email, r.PostFormValue("name")); err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}
