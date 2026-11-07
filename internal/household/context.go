package household

import "context"

type ctxKey struct{}

// WithContext returns ctx carrying h as the household of the request.
func WithContext(ctx context.Context, h Household) context.Context {
	return context.WithValue(ctx, ctxKey{}, h)
}

// FromContext returns the household the membership middleware stored on the
// request context, the way auth.EmailFromContext returns the identity. ok is
// false outside a request of a household member.
func FromContext(ctx context.Context) (h Household, ok bool) {
	h, ok = ctx.Value(ctxKey{}).(Household)
	return h, ok && h.ID != 0
}

// WithoutHousehold returns ctx carrying no household, whatever ctx carried:
// FromContext reports false on it. The operator's curation of the central
// catalog runs on it, because an operator who is also a member arrives with
// that household on the request context, and a catalog write in a household's
// scope changes the household's overlay instead of the central entry
// (ADR-00018, Catalog).
func WithoutHousehold(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, Household{})
}
