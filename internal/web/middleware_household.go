package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/nonmember"
)

// memberLookup finds the household of a verified email. *household.Store
// satisfies it.
type memberLookup interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// membership decides admission, after the identity middleware has verified
// the identity. It is the only lock, since any email that can authenticate
// gets an identity (ADR-00018, ADR-00030), so it fails closed: a request with no verified identity, or
// whose membership cannot be read, never reaches a handler, unless its path is
// public (ADR-00029). What it admits is
// layout.Admits, the rule the nav also reads; a member's request carries its
// household.
func membership(members memberLookup, ops config.Operator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The health check is public, and it must answer whatever the
			// state of the database or the identity on the request, so it
			// skips the membership lookup entirely.
			if r.URL.Path == HealthPath {
				next.ServeHTTP(w, r)
				return
			}
			// chi routes the raw path, so "/settings/.." is dispatched into the
			// /settings mount while its cleaned form "/" names no operator page.
			// No page lives behind a dot segment, so refuse one before any
			// admission decision, whoever asks, and the rule below can read
			// the path as written.
			if hasDotSegment(r) {
				layout.WriteError(w, r, fmt.Errorf("dot segment in %q: %w", r.URL.Path, layout.ErrNotFound))
				return
			}
			email, ok := auth.EmailFromContext(r.Context())
			if !ok {
				// No identity: the identity middleware let the request through
				// only because its path is public (ADR-00029). Check that
				// again, on both forms of the path, rather than trust it.
				if layout.IsPublic(r.URL.Path) && layout.IsPublic(r.URL.EscapedPath()) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			operator := ops.IsOperator(email)
			if operator {
				// Presentation only: the nav shows operator links. The checks
				// below stay the lock.
				r = r.WithContext(layout.WithOperator(r.Context()))
			}

			h, err := members.ForEmail(r.Context(), email)
			member := err == nil
			switch {
			case member:
				r = r.WithContext(household.WithContext(r.Context(), h))
			case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
				// ErrInvalid is an identity that is not a plain address, which
				// cannot be a member.
			default:
				layout.WriteError(w, r, err)
				return
			}

			switch {
			case admits(r, member, operator):
				next.ServeHTTP(w, r)
			case member:
				// A member is refused only an operator page.
				layout.Render(w, r, http.StatusForbidden, layout.ErrorPage(http.StatusForbidden, "This page is for the operator."))
			default:
				nonmember.Refuse(w, r)
			}
		})
	}
}

// admits applies layout.Admits to the request's decoded and escaped paths and
// admits only what both forms allow, so an encoding cannot carry a request past
// the operator check.
func admits(r *http.Request, member, operator bool) bool {
	return layout.Admits(r.URL.Path, member, operator) && layout.Admits(r.URL.EscapedPath(), member, operator)
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
