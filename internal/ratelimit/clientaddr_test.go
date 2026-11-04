package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/kaecyra/gobbler/internal/ratelimit"
)

// ADR-00036: the header is believed from a trusted peer, ignored from any
// other, and ignored entirely with no trusted proxies.
func TestResolve(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16"), netip.MustParsePrefix("::1/128")}
	cases := []struct {
		name, remote, cf string
		trusted          []netip.Prefix
		want             string
	}{
		{"trusted peer, header used", "172.18.0.1:5000", "203.0.113.9", trusted, "203.0.113.9"},
		{"trusted peer, v6 header", "172.18.0.1:5000", "2001:db8::1", trusted, "2001:db8::1"},
		{"trusted v6 peer", "[::1]:5000", "203.0.113.9", trusted, "203.0.113.9"},
		{"trusted peer, junk header", "172.18.0.1:5000", "not-an-ip", trusted, "172.18.0.1"},
		{"trusted peer, no header", "172.18.0.1:5000", "", trusted, "172.18.0.1"},
		{"other peer, header ignored", "192.168.1.50:5000", "203.0.113.9", trusted, "192.168.1.50"},
		{"no trusted proxies, header ignored", "172.18.0.1:5000", "203.0.113.9", nil, "172.18.0.1"},
		{"v4-mapped peer matches a v4 range", "[::ffff:172.18.0.1]:5000", "203.0.113.9", trusted, "203.0.113.9"},
		{"unreadable peer", "garbage", "203.0.113.9", trusted, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("CF-Connecting-IP", tc.cf)
			got := ""
			if a := ratelimit.Resolve(r, tc.trusted); a.IsValid() {
				got = a.String()
			}
			if got != tc.want {
				t.Errorf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientAddrIsTheResolvedOneAndNeverTheHeaderWithoutIt(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "172.18.0.1:5000"
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if got := ratelimit.ClientAddr(r).String(); got != "172.18.0.1" {
		t.Errorf("without the middleware = %s, want the peer", got)
	}
	var got string
	h := ratelimit.ResolveClient([]netip.Prefix{netip.MustParsePrefix("172.18.0.1/32")})(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = ratelimit.ClientAddr(r).String() }))
	h.ServeHTTP(httptest.NewRecorder(), r)
	if got != "203.0.113.9" {
		t.Errorf("through the middleware = %s, want the header", got)
	}
}

func TestClientKeyGivesAnUnreadableAddressOneSharedKey(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "garbage"
	if k, ok := ratelimit.ClientKey(r); !ok || k != "unknown" {
		t.Errorf("key = %q, %v", k, ok)
	}
}
