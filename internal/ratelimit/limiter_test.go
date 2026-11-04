package ratelimit_test

import (
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
	h := l.Middleware(func(r *http.Request) (string, bool) { return r.Header.Get("X-Key"), true })(
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
	h := l.Middleware(func(*http.Request) (string, bool) { return "", false })(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", rec.Code)
	}
}
