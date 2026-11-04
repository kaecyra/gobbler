package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
)

// oauthRegister posts a client registration as the connection at remote, with
// the CF-Connecting-IP header cf when it is not empty.
func oauthRegister(h http.Handler, remote, cf string) int {
	r := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"client_name":"a","redirect_uris":["https://app.example/cb"]}`))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote
	if cf != "" {
		r.Header.Set("CF-Connecting-IP", cf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}

func limitedConfig() config.Config {
	cfg := devConfig()
	cfg.OAuth = config.OAuth{Register: config.Bucket{Rate: 0.01, Burst: 3}, Token: config.Bucket{Rate: 0.01, Burst: 5}}
	cfg.Server.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")}
	return cfg
}

// The production wiring (ADR-00036): the router builds the OAuth limiters from
// the configuration, and resolves the client address before anything uses it.
func TestRouterLimitsRegistrationPerAddress(t *testing.T) {
	h := testRouter(t, limitedConfig(), Deps{})
	for i := 0; i < 3; i++ {
		if got := oauthRegister(h, "192.0.2.1:4000", ""); got != http.StatusCreated {
			t.Fatalf("registration %d = %d", i+1, got)
		}
	}
	if got := oauthRegister(h, "192.0.2.1:4000", ""); got != http.StatusTooManyRequests {
		t.Errorf("registration past the burst = %d, want 429", got)
	}
	if got := oauthRegister(h, "192.0.2.2:4000", ""); got != http.StatusCreated {
		t.Errorf("another address = %d, want 201", got)
	}
}

// Each endpoint is held to its own setting: the token bucket has a different
// burst from the registration one.
func TestRouterHoldsEachEndpointToItsOwnSetting(t *testing.T) {
	h := testRouter(t, limitedConfig(), Deps{})
	token := func() int {
		r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=nope"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = "192.0.2.1:4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	for i := 0; i < 5; i++ {
		if got := token(); got != http.StatusBadRequest {
			t.Fatalf("token request %d = %d, want it answered (400)", i+1, got)
		}
	}
	if got := token(); got != http.StatusTooManyRequests {
		t.Errorf("token request past its burst of 5 = %d, want 429", got)
	}
	// Registration has a burst of 3 and is a bucket of its own.
	for i := 0; i < 3; i++ {
		if got := oauthRegister(h, "192.0.2.1:4000", ""); got != http.StatusCreated {
			t.Fatalf("registration %d = %d", i+1, got)
		}
	}
	if got := oauthRegister(h, "192.0.2.1:4000", ""); got != http.StatusTooManyRequests {
		t.Errorf("registration past its burst of 3 = %d, want 429", got)
	}
}

func TestRouterKeysATrustedProxysRequestsByTheForwardedAddress(t *testing.T) {
	h := testRouter(t, limitedConfig(), Deps{})
	for i := 0; i < 3; i++ {
		oauthRegister(h, "172.18.0.1:4000", "203.0.113.9")
	}
	if got := oauthRegister(h, "172.18.0.1:4000", "203.0.113.9"); got != http.StatusTooManyRequests {
		t.Errorf("forwarded address past the burst = %d, want 429", got)
	}
	if got := oauthRegister(h, "172.18.0.1:4000", "203.0.113.10"); got != http.StatusCreated {
		t.Errorf("another forwarded address through the same proxy = %d, want 201", got)
	}
	// Not a trusted proxy: the header is ignored, so rotating it buys nothing.
	var last int
	for i := 0; i < 4; i++ {
		last = oauthRegister(h, "192.0.2.50:4000", "198.51.100."+string(rune('1'+i)))
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("untrusted peer rotating the header = %d, want 429", last)
	}
}
