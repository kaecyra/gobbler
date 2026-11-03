package mcp

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// limiter is a leaky bucket for each credential (ADR-00034): a request pours
// one unit in, the bucket drains at rate units a second, and a request that
// would overflow burst is refused. It is safe for concurrent use.
type limiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]bucket
}

type bucket struct {
	level float64
	at    time.Time
}

func newLimiter(rate float64, burst int, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: float64(burst), now: now, buckets: map[string]bucket{}}
}

// allow pours a request into subject's bucket. When it would overflow, it
// returns how long to wait before the same request fits.
func (l *limiter) allow(subject string) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Idle buckets are empty, so dropping them changes nothing and keeps the
	// table from growing with every credential ever seen.
	if len(l.buckets) > 1024 {
		for k, b := range l.buckets {
			if l.drained(b, now) {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[subject]
	b.level = math.Max(0, b.level-now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.level+1 > l.burst {
		l.buckets[subject] = b
		over := b.level + 1 - l.burst
		return false, time.Duration(over / l.rate * float64(time.Second))
	}
	b.level++
	l.buckets[subject] = b
	return true, 0
}

func (l *limiter) drained(b bucket, now time.Time) bool {
	return b.level-now.Sub(b.at).Seconds()*l.rate <= 0
}

// limit holds each credential to its bucket. A refused request gets 429 with
// Retry-After in whole seconds, rounded up.
func (l *limiter) limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := CredentialFrom(r.Context())
		if !ok {
			// Routes sits behind Bearer; reaching here is a wiring fault.
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if allowed, wait := l.allow(c.Subject()); !allowed {
			secs := max(1, int(math.Ceil(wait.Seconds())))
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			http.Error(w, "rate limit exceeded; retry later", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
