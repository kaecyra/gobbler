package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/nonmember"
	"github.com/kaecyra/gobbler/internal/web/settings"
)

// InvitationsPrefix is where invitation acceptance lives. A verified non-member
// can reach it, and nothing else but the health check, the not-a-member page
// and the embedded assets that page is drawn with (ADR-00018, Admission).
const InvitationsPrefix = "/invitations/"

// operatorPaths are the operator's pages (ADR-00018, Invitations): deployment
// settings now, and later the household invitations and the review of overlay
// entries suggested for central. A path under one of them is open to an
// operator email, member or not, and closed to everyone else. A new operator
// page is added here.
var operatorPaths = []string{settings.Pattern}

// memberLookup finds the household of a verified email. *household.Store
// satisfies it.
type memberLookup interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// membership decides admission, after Access has verified the identity. It is
// the only lock now that Access admits any email that authenticates
// (ADR-00018), so it fails closed: a request with no verified identity, or
// whose membership cannot be read, never reaches a handler.
//
//   - A member reaches every page but the operator's, and the request carries
//     its household.
//   - An operator email reaches the operator's pages, member or not.
//   - A non-member reaches only the invitation routes, the not-a-member page
//     and the assets, and gets the not-a-member page for anything else.
func membership(members memberLookup, ops config.Operator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == HealthPath {
				next.ServeHTTP(w, r)
				return
			}
			// chi routes the raw path, so "/settings/.." is dispatched into the
			// /settings mount while its cleaned form "/" names no operator page.
			// No page lives behind a dot segment, so refuse one before any
			// admission decision, whoever asks, and the checks below can read
			// the path as written.
			if hasDotSegment(r) {
				layout.WriteError(w, r, fmt.Errorf("dot segment in %q: %w", r.URL.Path, layout.ErrNotFound))
				return
			}
			email, ok := auth.EmailFromContext(r.Context())
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			operator := ops.IsOperator(email)
			onOperatorPage := isOperatorPath(r)

			h, err := members.ForEmail(r.Context(), email)
			switch {
			case err == nil:
				if onOperatorPage && !operator {
					layout.Render(w, r, http.StatusForbidden, layout.ErrorPage(http.StatusForbidden, "This page is for the operator."))
					return
				}
				next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
			case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
				// ErrInvalid is an identity that is not a plain address, which
				// cannot be a member.
				if (onOperatorPage && operator) || nonMemberMay(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				nonmember.Refuse(w, r)
			default:
				layout.WriteError(w, r, err)
			}
		})
	}
}

// nonMemberMay reports whether a verified non-member may open p. The caller
// has already refused dot segments, so the prefixes mean what they say.
func nonMemberMay(p string) bool {
	return p == nonmember.Pattern || strings.HasPrefix(p, InvitationsPrefix) || strings.HasPrefix(p, "/static/")
}

// hasDotSegment reports whether the request path has a "." or ".." segment,
// in its decoded form or as sent (so "%2e%2e" is caught either way).
func hasDotSegment(r *http.Request) bool {
	for _, p := range []string{r.URL.Path, r.URL.EscapedPath()} {
		if slices.ContainsFunc(strings.Split(p, "/"), func(seg string) bool {
			return seg == "." || seg == ".." || strings.EqualFold(seg, "%2e") || strings.EqualFold(seg, "%2e%2e") ||
				strings.EqualFold(seg, ".%2e") || strings.EqualFold(seg, "%2e.")
		}) {
			return true
		}
	}
	return false
}

// isOperatorPath reports whether the request is for an operator page: its
// decoded or its escaped path, cleaned, is an operator root or lies under one.
// Dot segments never get this far (hasDotSegment), so cleaning changes nothing
// on a request that reaches it; it only normalises repeated slashes.
func isOperatorPath(r *http.Request) bool {
	for _, p := range []string{r.URL.Path, r.URL.EscapedPath()} {
		p = path.Clean("/" + p)
		for _, op := range operatorPaths {
			if p == op || strings.HasPrefix(p, op+"/") {
				return true
			}
		}
	}
	return false
}

// devSeeded is the member lookup under the dev bypass. Before its first lookup
// it gives the dev identities something to be a member of: DevEmail in one
// household and DevOtherEmail in another. Only the bypass uses it, and the
// bypass exists only on a loopback listener (ADR-00003), so a deployment
// behind Access never has these members. Seeding waits for the first request,
// not for construction, so building the router (the e2e route lister does)
// needs no migrated database; a failed seed is retried by the next request and
// fails the one that hit it.
type devSeeded struct {
	store *household.Store
	mu    sync.Mutex
	done  bool
}

func (d *devSeeded) ForEmail(ctx context.Context, email string) (household.Household, error) {
	if err := d.seed(ctx); err != nil {
		return household.Household{}, err
	}
	return d.store.ForEmail(ctx, email)
}

func (d *devSeeded) seed(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done {
		return nil
	}
	if err := d.store.Seed(ctx, "Dev household", auth.DevEmail); err != nil {
		return fmt.Errorf("seed dev household: %w", err)
	}
	if err := d.store.Seed(ctx, "Other household", auth.DevOtherEmail); err != nil {
		return fmt.Errorf("seed other dev household: %w", err)
	}
	d.done = true
	return nil
}
