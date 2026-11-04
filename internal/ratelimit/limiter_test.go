package ratelimit_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ratelimit"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAllowRefusesPastTheBurstAndDrainsAtTheRate(t *testing.T) {
	c := &clock{time.Unix(1000, 0)}
	l := ratelimit.New(2, 3, c.now)
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d refused inside the burst", i+1)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != 500*time.Millisecond {
		t.Fatalf("fourth request = %v, wait %v; want refused, 500ms", ok, wait)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("another key was refused")
	}
	c.t = c.t.Add(500 * time.Millisecond)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("still refused after the bucket drained one unit")
	}
}

func TestMiddlewareAnswers429WithRetryAfterPerKey(t *testing.T) {
	c := &clock{time.Unix(1000, 0)}
	l := ratelimit.New(0.1, 1, c.now)
	h := l.Middleware(func(r *http.Request) (string, bool) { return r.Header.Get("X-Key"), true }, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	do := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Key", key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	if got := do("a").Code; got != http.StatusNoContent {
		t.Fatalf("first = %d", got)
	}
	rec := do("a")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "10" {
		t.Fatalf("second = %d, Retry-After %q; want 429, 10", rec.Code, rec.Header().Get("Retry-After"))
	}
	if got := do("b").Code; got != http.StatusNoContent {
		t.Errorf("another key = %d, want it unaffected", got)
	}
}

func TestMiddlewareWithoutAKeyIsAWiringFault(t *testing.T) {
	l := ratelimit.New(1, 1, time.Now)
	h := l.Middleware(func(*http.Request) (string, bool) { return "", false }, nil)(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}

func TestMiddlewareLetsTheCallerWriteTheRefusal(t *testing.T) {
	l := ratelimit.New(0.1, 1, time.Now)
	h := l.Middleware(func(*http.Request) (string, bool) { return "k", true },
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("custom"))
		})(http.NotFoundHandler())
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "custom" || rec.Header().Get("Retry-After") == "" {
		t.Errorf("body %q, Retry-After %q; want the custom body and the header", rec.Body, rec.Header().Get("Retry-After"))
	}
}

// Keys are chosen by the caller, so the sweep of idle buckets must not run on
// every request once the table is large.
func TestIdleBucketsAreSweptAtMostOncePerInterval(t *testing.T) {
	c := &clock{time.Unix(1000, 0)}
	l := ratelimit.New(1, 1, c.now)
	for i := 0; i < 1100; i++ {
		l.Allow(fmt.Sprint("k", i))
	}
	c.t = c.t.Add(time.Minute) // every bucket is idle now
	l.Allow("first")           // sweeps
	if n := l.Len(); n != 1 {
		t.Fatalf("table after the sweep = %d, want only the live key", n)
	}
	for i := 0; i < 1100; i++ {
		l.Allow(fmt.Sprint("m", i))
	}
	c.t = c.t.Add(time.Second) // idle again, but inside the interval
	l.Allow("again")
	if n := l.Len(); n < 1100 {
		t.Errorf("table = %d: swept again within the interval", n)
	}
	c.t = c.t.Add(time.Minute)
	l.Allow("later")
	if n := l.Len(); n != 1 {
		t.Errorf("table after the next interval = %d, want 1", n)
	}
}
