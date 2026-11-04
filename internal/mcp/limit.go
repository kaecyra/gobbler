package mcp

import (
	"net/http"
	"time"

	"github.com/kaecyra/gobbler/internal/ratelimit"
)

// limiter holds each credential to its own leaky bucket (ADR-00034). The
// bucket itself is ratelimit.Limiter, shared with the public OAuth endpoints
// (ADR-00036).
type limiter struct{ l *ratelimit.Limiter }

func newLimiter(rate float64, burst int, now func() time.Time) *limiter {
	return &limiter{l: ratelimit.New(rate, burst, now)}
}

// limit holds each credential to its bucket. A refused request gets 429 with
// Retry-After in whole seconds, rounded up.
func (l *limiter) limit(next http.Handler) http.Handler {
	return l.l.Middleware(func(r *http.Request) (string, bool) {
		c, ok := CredentialFrom(r.Context())
		if !ok {
			// Routes sits behind Bearer; reaching here is a wiring fault.
			return "", false
		}
		return c.Subject(), true
	}, nil)(next)
}
