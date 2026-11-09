package household

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Patterns where the router mounts MemberRoutes and InvitationRoutes.
const (
	Pattern            = "/household"
	InvitationsPattern = layout.InvitationsPath
)

// Households is what the pages call: the household service (ADR-00043), which
// checks that the Caller is a member of the household it acts in.
// *households.Service satisfies it.
type Households interface {
	Members(ctx context.Context, c apihouseholds.Caller) ([]string, error)
	Pending(ctx context.Context, c apihouseholds.Caller) ([]apihouseholds.Invitation, error)
	Invite(ctx context.Context, c apihouseholds.Caller, email string) (string, error)
	Revoke(ctx context.Context, c apihouseholds.Caller, code string) error
	Remove(ctx context.Context, c apihouseholds.Caller, target string) error
	Leave(ctx context.Context, c apihouseholds.Caller) error
	Delete(ctx context.Context, c apihouseholds.Caller, confirm string) error
	InvitationFor(ctx context.Context, c apihouseholds.Caller, code string) (apihouseholds.Invitation, error)
	Accept(ctx context.Context, c apihouseholds.Caller, code, name string) (apihouseholds.Household, error)
	HouseStyle(ctx context.Context, c apihouseholds.Caller) (string, error)
	SetHouseStyle(ctx context.Context, c apihouseholds.Caller, style string) error
}

// KeyStore is what the household keys section uses from the API key store
// (ADR-00031). *apikey.Store satisfies it.
type KeyStore interface {
	ListHousehold(ctx context.Context, householdID int64) ([]apikey.Key, error)
	CreateHousehold(ctx context.Context, householdID int64, by, name string, lifetime time.Duration) (string, error)
	RevokeHousehold(ctx context.Context, householdID, id int64) error
}

// Deps is what the routes need. Keys is required by MemberRoutes.
type Deps struct {
	Households Households
	Keys       KeyStore
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
	r.Post("/invitations/{"+layout.InvitationCodeParam+"}/revoke", h.revoke)
	r.Post("/members/remove", h.remove)
	r.Post("/leave", h.leave)
	r.Post("/delete", h.delete)
	r.Post("/style", h.saveStyle)
	h.keyRoutes(r)
	return r
}

// InvitationRoutes returns the routes for opening and accepting an invitation,
// relative to InvitationsPattern. A verified non-member can reach them.
func InvitationRoutes(d Deps) chi.Router {
	h := &handler{d}
	r := chi.NewRouter()
	r.Get("/{"+layout.InvitationCodeParam+"}", h.showInvitation)
	r.Post("/{"+layout.InvitationCodeParam+"}/accept", h.accept)
	return r
}

// actor is the Caller of a member's request: the verified email and the
// household the membership middleware stored. The service checks the
// membership; this only refuses a request that has neither to check. A
// request without both never reaches a handler (the membership middleware), so
// a missing one is refused rather than guessed at.
func actor(r *http.Request) (c apihouseholds.Caller, err error) {
	c = caller.FromContext(r.Context())
	if c.Person == "" || c.Household.ID == 0 {
		return apihouseholds.Caller{}, fmt.Errorf("no member on the request: %w", layout.ErrNotFound)
	}
	return c, nil
}

func (h *handler) show(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	members, err := h.Households.Members(r.Context(), c)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	pending, err := h.Households.Pending(r.Context(), c)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	keys, err := h.Keys.ListHousehold(r.Context(), c.Household.ID)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	style, err := h.Households.HouseStyle(r.Context(), c)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, membersPage(pageView{
		Name: c.Household.Name, Self: c.Person, Members: members, Pending: layout.NewPendingInvitations(c.Person, pending),
		Keys: layout.NewAPIKeyRows(keys, time.Now(), true, keyRevokePath), HouseStyle: style,
	}))
}

// saveStyle saves the house style (ADR-00048). A style over the bound is
// refused with the store's reason, as a mistyped email is.
func (h *handler) saveStyle(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	done(w, r, h.Households.SetHouseStyle(r.Context(), c, r.PostFormValue("style")))
}

// done sends the browser back to the members page after a change.
func done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}

func (h *handler) invite(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	_, err = h.Households.Invite(r.Context(), c, r.PostFormValue("email"))
	done(w, r, err)
}

func (h *handler) revoke(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	code, err := layout.InvitationCode(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	done(w, r, h.Households.Revoke(r.Context(), c, code))
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	done(w, r, h.Households.Remove(r.Context(), c, r.PostFormValue("email")))
}

func (h *handler) leave(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	if err := h.Households.Leave(r.Context(), c); err != nil {
		layout.Fail(w, r, err)
		return
	}
	// Having left, the reader is a non-member: the home page says so.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	c, err := actor(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	if err := h.Households.Delete(r.Context(), c, r.PostFormValue("confirm")); err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) showInvitation(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		layout.Fail(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	code, err := layout.InvitationCode(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	inv, err := h.Households.InvitationFor(r.Context(), apihouseholds.Caller{Person: email}, code)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	_, member := apihouseholds.FromContext(r.Context())
	layout.Render(w, r, http.StatusOK, acceptPage(acceptView{
		Code: inv.Code, Self: email, Kind: inv.Kind, Household: inv.HouseholdName,
		IssuedBy: inv.IssuedBy, Expires: layout.FormatTime(inv.ExpiresAt), Member: member,
	}))
}

func (h *handler) accept(w http.ResponseWriter, r *http.Request) {
	email, ok := auth.EmailFromContext(r.Context())
	if !ok {
		layout.Fail(w, r, fmt.Errorf("no identity on the request: %w", layout.ErrNotFound))
		return
	}
	code, err := layout.InvitationCode(r)
	if err != nil {
		layout.Fail(w, r, err)
		return
	}
	if _, err := h.Households.Accept(r.Context(), apihouseholds.Caller{Person: email}, code, r.PostFormValue("name")); err != nil {
		layout.Fail(w, r, err)
		return
	}
	http.Redirect(w, r, Pattern, http.StatusSeeOther)
}
