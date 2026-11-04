package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ratelimit"
	weboauth "github.com/kaecyra/gobbler/internal/web/oauth"
)

// limited is a site whose registration and token endpoints each allow two
// requests at once and drain too slowly to matter inside a test, with the
// clock frozen.
func limited(t *testing.T) *site {
	t.Helper()
	now := func() time.Time { return time.Unix(1000, 0) }
	return newSiteWith(t, stubMetadata{}, func(d *weboauth.Deps) {
		d.Register = ratelimit.New(0.1, 2, now)
		d.Token = ratelimit.New(0.1, 2, now)
	})
}

// fromAddr sends the request as if from the address.
func (s *site) fromAddr(req *http.Request, addr string) *httptest.ResponseRecorder {
	req.RemoteAddr = addr + ":4000"
	return s.do(req, "")
}

func registerReq() *http.Request {
	r := httptest.NewRequest(http.MethodPost, weboauth.RegisterPath, strings.NewReader(`{"client_name":"a","redirect_uris":["https://app.example/cb"]}`))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func tokenReq() *http.Request {
	r := httptest.NewRequest(http.MethodPost, weboauth.TokenPath, strings.NewReader(url.Values{"grant_type": {"nope"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// ADR-00036: each endpoint answers 429 with Retry-After past the burst for one
// address, and another address is unaffected.
func TestPublicEndpointsAreLimitedPerAddress(t *testing.T) {
	cases := []struct {
		name string
		req  func() *http.Request
		ok   int
	}{
		{"register", registerReq, http.StatusCreated},
		{"token", tokenReq, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := limited(t)
			for i := 0; i < 2; i++ {
				if rec := s.fromAddr(tc.req(), "192.0.2.1"); rec.Code != tc.ok {
					t.Fatalf("request %d = %d: %s", i+1, rec.Code, rec.Body)
				}
			}
			rec := s.fromAddr(tc.req(), "192.0.2.1")
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "10" {
				t.Fatalf("third = %d, Retry-After %q; want 429, 10", rec.Code, rec.Header().Get("Retry-After"))
			}
			if m := jsonOf(t, rec); m["error"] != "temporarily_unavailable" || rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("429 body = %v (%s), want an OAuth JSON error", m, rec.Header().Get("Content-Type"))
			}
			if rec := s.fromAddr(tc.req(), "192.0.2.2"); rec.Code != tc.ok {
				t.Errorf("another address = %d, want %d", rec.Code, tc.ok)
			}
		})
	}
}

func TestTheTwoEndpointsHaveSeparateBuckets(t *testing.T) {
	s := limited(t)
	for i := 0; i < 3; i++ {
		s.fromAddr(registerReq(), "192.0.2.1")
	}
	if rec := s.fromAddr(tokenReq(), "192.0.2.1"); rec.Code == http.StatusTooManyRequests {
		t.Error("an exhausted registration bucket limited the token endpoint")
	}
}

// A caller that is not a trusted proxy cannot pick its key by sending the
// header (ADR-00036).
func TestAForgedHeaderDoesNotChooseTheKey(t *testing.T) {
	s := limited(t)
	h := ratelimit.ResolveClient(nil)(s.h)
	var last int
	for i := 0; i < 3; i++ {
		r := registerReq()
		r.RemoteAddr = "192.0.2.1:4000"
		r.Header.Set("CF-Connecting-IP", "203.0.113."+string(rune('1'+i)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("third request with a fresh forged header = %d, want 429", last)
	}
}

// A public endpoint must never be left unlimited by leaving a limiter out.
func TestRoutesRequireBothLimiters(t *testing.T) {
	l := ratelimit.New(1, 1, time.Now)
	for name, d := range map[string]weboauth.Deps{
		"none": {}, "no token": {Register: l}, "no register": {Token: l},
	} {
		if _, err := weboauth.Routes(d); err == nil {
			t.Errorf("%s: Routes built without both limiters", name)
		}
	}
}
