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
