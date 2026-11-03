package web

import (
	"errors"
	"net/http"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The machine-facing endpoint (layout.BearerPath) has its own identity source
// (ADR-00031, ADR-00034). The three pieces below keep it apart from the
// session and keep one admission rule:
//
//   - onSession wraps the session authenticator and skips it for the endpoint,
//     so a cookie never authenticates it and a request without one is never
//     redirected to sign-in;
//   - onBearer authenticates the endpoint by bearer credential alone (mcp.Bearer);
//   - membership (middleware_household.go) then decides admission for the
//     credential's owner with the same layout.Admits rule as for a session.

// bearerRequest reports whether the request is for the bearer endpoint, in
// either form of its path. Either form counts, so an encoding cannot carry a
// request to the endpoint past the bearer check, nor onto the session path.
func bearerRequest(r *http.Request) bool {
	return layout.IsBearerPath(r.URL.Path) || layout.IsBearerPath(r.URL.EscapedPath())
}

// onSession applies the session authenticator to every request but the bearer
// endpoint's.
func onSession(session func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withSession := session(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bearerRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			withSession.ServeHTTP(w, r)
		})
	}
}

// onBearer applies the bearer authenticator to the bearer endpoint's requests
// and passes every other request through.
func onBearer(bearer func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withBearer := bearer(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bearerRequest(r) {
				withBearer.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// admitBearer is membership for the bearer endpoint, after mcp.Bearer verified
// the credential. A personal credential is admitted exactly as a session of
// its owner would be: by the owner's membership, found by members. A household
// credential acts for its household, which exists while the credential does
// (deleting a household deletes its keys). Refusals are plain text, since the
// caller is a program. The caller has refused dot segments.
func admitBearer(members memberLookup, ops config.Operator, next http.Handler, w http.ResponseWriter, r *http.Request) {
	c, ok := mcp.CredentialFrom(r.Context())
	if !ok {
		// onBearer runs first; reaching here is a wiring fault, and failing
		// closed is the only safe answer.
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "a valid API key is required", http.StatusUnauthorized)
		return
	}
	if !layout.IsBearerPath(r.URL.Path) || !layout.IsBearerPath(r.URL.EscapedPath()) {
		layout.WriteError(w, r, layout.ErrNotFound)
		return
	}
	h := household.Household{ID: c.HouseholdID, Name: c.HouseholdName}
	if c.HouseholdID == 0 {
		var err error
		h, err = members.ForEmail(r.Context(), c.Email)
		switch {
		case err == nil:
		case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
			http.Error(w, "this key's owner is not a member of a household", http.StatusForbidden)
			return
		default:
			layout.WriteError(w, r, err)
			return
		}
	}
	if !admits(r, true, ops.IsOperator(c.Email)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
}
