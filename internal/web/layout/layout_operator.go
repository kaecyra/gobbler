package layout

import (
	"context"
	"path"
	"strings"

	"github.com/kaecyra/gobbler/internal/household"
)

// OperatorPaths are the operator's pages (ADR-00018, Invitations). Admits
// opens a path under one of them to an operator alone. Paths are written out,
// not imported, for the same reason navItems' are (ADR-00012), so a new
// operator page is added here.
var OperatorPaths = []string{"/settings", "/operator"}

// NotAMemberPath is the page a verified non-member may always open. Package
// nonmember mounts it here.
const NotAMemberPath = "/not-a-member"

// staticPrefix is where the embedded assets are served. The not-a-member page
// is drawn with them, so a non-member may fetch them.
const staticPrefix = "/static/"

// Admits reports whether a request for p may be served, given whether its
// verified identity is a member of a household and whether it is an
// operator's (ADR-00018). It is the one admission rule: package web's
// membership middleware enforces it, and the nav shows only the links it
// admits, so the two cannot disagree.
//
//   - An operator page admits an operator, member or not, and nobody else.
//   - Every other page admits a member.
//   - A non-member may open only the not-a-member page, the invitation routes
//     and the assets.
//
// The caller has refused dot segments, so the prefixes mean what they say.
func Admits(p string, member, operator bool) bool {
	switch {
	case isOperatorPath(p):
		return operator
	case member:
		return true
	default:
		return p == NotAMemberPath || strings.HasPrefix(p, InvitationsPath+"/") || strings.HasPrefix(p, staticPrefix)
	}
}

// isOperatorPath reports whether p, cleaned, is an operator root or lies under
// one. Cleaning only normalises repeated slashes on a path with no dot
// segments.
func isOperatorPath(p string) bool {
	p = path.Clean("/" + p)
	for _, op := range OperatorPaths {
		if p == op || strings.HasPrefix(p, op+"/") {
			return true
		}
	}
	return false
}

type operatorKey struct{}

// WithOperator marks ctx as the request of an operator email, member or not.
// It only decides what the nav shows; the lock is the membership middleware.
func WithOperator(ctx context.Context) context.Context {
	return context.WithValue(ctx, operatorKey{}, true)
}

// IsOperator reports whether ctx was marked by WithOperator.
func IsOperator(ctx context.Context) bool {
	v, _ := ctx.Value(operatorKey{}).(bool)
	return v
}

// visible reports whether the nav shows item to the request in ctx: exactly
// when Admits would let that request open it.
func visible(ctx context.Context, item navItem) bool {
	_, member := household.FromContext(ctx)
	return Admits(item.Path, member, IsOperator(ctx))
}
