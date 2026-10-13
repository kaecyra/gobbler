package layout

import (
	"context"
	"strings"
)

// OperatorPaths are the operator's pages (ADR-00018, Invitations). Package web
// admits only an operator to a path under one of them, and the nav hides the
// link to one from everyone else, so both read this one list: a new operator
// page is added here and nothing can drift. Paths are written out, not
// imported, for the same reason navItems' are (ADR-00012).
var OperatorPaths = []string{"/settings"}

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

// operatorOnly reports whether item's page is an operator page.
func operatorOnly(item navItem) bool {
	for _, p := range OperatorPaths {
		if item.Path == p || strings.HasPrefix(item.Path, p+"/") {
			return true
		}
	}
	return false
}

// visible reports whether the nav shows item to the request in ctx.
func visible(ctx context.Context, item navItem) bool {
	return !operatorOnly(item) || IsOperator(ctx)
}
