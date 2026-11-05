package netguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/kaecyra/gobbler/internal/netguard"
)

func TestBlockedAddresses(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "fe80::1", "fc00::1", "0.0.0.0", "224.0.0.1", "100.64.0.1", "::ffff:10.0.0.1", "2001:db8::1", "255.255.255.255"} {
		if !netguard.Blocked(netip.MustParseAddr(a)) {
			t.Errorf("%s is not blocked", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if netguard.Blocked(netip.MustParseAddr(a)) {
			t.Errorf("%s is blocked", a)
		}
	}
}

func get(t *testing.T, c *http.Client, u string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

// The real guard refuses a name that resolves to loopback, and the server on
// it is never reached.
func TestClientRefusesANameThatResolvesToAPrivateAddress(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	for _, host := range []string{"localhost", "127.0.0.1"} {
		_, err := get(t, netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects}), "http://"+host+":"+u.Port()+"/")
		if !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s: err = %v, want ErrBlocked", host, err)
		}
	}
	if hits != 0 {
		t.Fatalf("the server on loopback was reached %d times", hits)
	}
}

// blockPort is a dial guard that refuses one port, so two servers on loopback
// can stand for a public and a private host.
func blockPort(port string) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		if strings.HasSuffix(address, ":"+port) {
			return netguard.Control(network, "127.0.0.1:"+port, c)
		}
		return nil
	}
}

func TestClientRefusesARedirectToAPrivateAddress(t *testing.T) {
	var private int
	priv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { private++ }))
	t.Cleanup(priv.Close)
	pu, _ := url.Parse(priv.URL)
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, priv.URL, http.StatusFound)
	}))
	t.Cleanup(pub.Close)
	c := netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, Dial: blockPort(pu.Port())})
	_, err := get(t, c, pub.URL)
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if private != 0 {
		t.Fatal("the private server was reached")
	}
}

func redirecting(t *testing.T, hops int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if n >= hops {
			_, _ = w.Write([]byte("done"))
			return
		}
		http.Redirect(w, r, "/"+strconv.Itoa(n+1), http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/0"
}

func allow(string, string, syscall.RawConn) error { return nil }

func TestClientFollowsFiveRedirectsAndRefusesTheSixth(t *testing.T) {
	c := netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, Dial: allow})
	resp, err := get(t, c, redirecting(t, 5))
	if err != nil {
		t.Fatalf("five redirects: %v", err)
	}
	_ = resp.Body.Close()
	_, err = get(t, c, redirecting(t, 6))
	if !errors.Is(err, netguard.ErrRedirect) {
		t.Fatalf("six redirects: err = %v, want ErrRedirect", err)
	}
}

func TestClientRefusesARedirectToAnotherScheme(t *testing.T) {
	for _, target := range []string{"ftp://example.com/x", "file:///etc/passwd", "gopher://example.com/"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target, http.StatusFound)
		}))
		c := netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, Dial: allow})
		_, err := get(t, c, srv.URL)
		srv.Close()
		if !errors.Is(err, netguard.ErrRedirect) {
			t.Errorf("%s: err = %v, want ErrRedirect", target, err)
		}
	}
}

func TestClientWithNoRedirectsReturnsTheRedirectResponse(t *testing.T) {
	c := netguard.NewClient(netguard.Options{Dial: allow})
	resp, err := get(t, c, redirecting(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the 302 itself", resp.StatusCode)
	}
}

// net/http skips the proxy for loopback targets, so a request to a test server
// cannot show the proxy is ignored; the transport's proxy function can.
func TestClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.invalid:3128")
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	tr, ok := netguard.NewClient(netguard.Options{}).Transport.(*http.Transport)
	if !ok {
		t.Fatal("the client's transport is not an *http.Transport")
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://recipes.example/", nil)
	if tr.Proxy != nil {
		if u, _ := tr.Proxy(req); u != nil {
			t.Fatalf("the client would use the proxy %s", u)
		}
	}
}

// AllowLoopback admits loopback and nothing else the guard refuses.
func TestAllowLoopbackAdmitsOnlyLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	loopback := "http://127.0.0.1:" + u.Port() + "/"

	off := netguard.NewClient(netguard.Options{})
	if _, err := get(t, off, loopback); !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("loopback with the option off: err = %v, want ErrBlocked", err)
	}
	on := netguard.NewClient(netguard.Options{AllowLoopback: true})
	resp, err := get(t, on, loopback)
	if err != nil {
		t.Fatalf("loopback with the option on: %v", err)
	}
	_ = resp.Body.Close()
	for _, target := range []string{"http://10.0.0.1/", "http://192.168.1.1/", "http://169.254.169.254/", "http://100.64.0.1/", "http://0.0.0.0/"} {
		if _, err := get(t, on, target); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s with the option on: err = %v, want ErrBlocked", target, err)
		}
	}
}
