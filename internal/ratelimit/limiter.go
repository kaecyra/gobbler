package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// pruneAbove is the table size past which idle buckets are dropped.
	pruneAbove = 1024
	// pruneEvery is the most often the table is swept. Keys can be chosen by
	// the caller, so a sweep on every request would let one caller make every
	// request pay for a scan of the whole table under the lock.
	pruneEvery = 10 * time.Second
)

// Limiter is a leaky bucket for each key (ADR-00034): a request pours one unit
// in, the bucket drains at rate units a second, and a request that would
// overflow burst is refused. It is safe for concurrent use.
type Limiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]bucket
	nextPrune time.Time
}

type bucket struct {
	level float64
	at    time.Time
}

// New returns a Limiter. rate must be positive and burst at least 1; callers
// validate configuration before they get here.
func New(rate float64, burst int, now func() time.Time) *Limiter {
	return &Limiter{rate: rate, burst: float64(burst), now: now, buckets: map[string]bucket{}}
}

// Allow pours a request into key's bucket. When it would overflow, it returns
// how long to wait before the same request fits.
func (l *Limiter) Allow(key string) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Idle buckets are empty, so dropping them changes nothing and keeps the
	// table from growing with every key ever seen.
	if len(l.buckets) > pruneAbove && !now.Before(l.nextPrune) {
		l.nextPrune = now.Add(pruneEvery)
		for k, b := range l.buckets {
			if l.drained(b, now) {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	b.level = math.Max(0, b.level-now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.level+1 > l.burst {
		l.buckets[key] = b
		over := b.level + 1 - l.burst
		return false, time.Duration(over / l.rate * float64(time.Second))
	}
	b.level++
	l.buckets[key] = b
	return true, 0
}

func (l *Limiter) drained(b bucket, now time.Time) bool {
	return b.level-now.Sub(b.at).Seconds()*l.rate <= 0
}

// Middleware holds each request to the bucket of the key keyOf returns for it.
// A refused request gets 429 with Retry-After in whole seconds, rounded up,
// and refuse writes the body; nil refuse writes plain text. A request keyOf
// cannot key (ok false) is a wiring fault and gets 500.
func (l *Limiter) Middleware(keyOf func(*http.Request) (key string, ok bool), refuse func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	if refuse == nil {
		refuse = func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "rate limit exceeded; retry later", http.StatusTooManyRequests)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := keyOf(r)
			if !ok {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if allowed, wait := l.Allow(key); !allowed {
				secs := max(1, int(math.Ceil(wait.Seconds())))
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				refuse(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
