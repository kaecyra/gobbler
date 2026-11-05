package ingest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/kaecyra/gobbler/internal/netguard"
)

// With no client in Deps the pipeline fetches through the netguard client, so a
// name that resolves to a private address is refused and the server behind it
// is never reached.
func TestFetchRefusesAPrivateAddressByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	e := newEnv(t)
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]"} {
		_, err := e.pipeline(t, nil).FromURL(e.ctx, "http://"+host+":"+u.Port()+"/recipe")
		if !errors.Is(err, ErrFetch) || !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s: err = %v, want ErrFetch wrapping ErrBlocked", host, err)
			continue
		}
		if !strings.Contains(err.Error(), "private or local network") {
			t.Errorf("%s: message does not say why: %v", host, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("the server on loopback was reached")
	}
}

// A refused hop of a redirect chain is a fetch error the importer can read.
func TestFetchRefusesARedirectToAPrivateAddress(t *testing.T) {
	var private atomic.Int32
	priv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { private.Add(1) }))
	t.Cleanup(priv.Close)
	pu, _ := url.Parse(priv.URL)
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, priv.URL, http.StatusFound)
	}))
	t.Cleanup(pub.Close)
	// Both servers are on loopback, so the guard refuses only the "private" port.
	client := netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, Dial: func(network, address string, c syscall.RawConn) error {
		if strings.HasSuffix(address, ":"+pu.Port()) {
			return netguard.Control(network, address, c)
		}
		return nil
	}})
	e := newEnv(t)
	_, err := e.pipeline(t, client).FromURL(e.ctx, pub.URL)
	if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), "private or local network") {
		t.Fatalf("err = %v, want ErrFetch saying the address is private", err)
	}
	if private.Load() != 0 {
		t.Fatal("the private server was reached")
	}
}

func TestFetchRefusesARedirectLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	client := netguard.NewClient(netguard.Options{Redirects: netguard.MaxRedirects, Dial: func(string, string, syscall.RawConn) error { return nil }})
	e := newEnv(t)
	_, err := e.pipeline(t, client).FromURL(e.ctx, srv.URL)
	if !errors.Is(err, ErrFetch) || !errors.Is(err, netguard.ErrRedirect) || !strings.Contains(err.Error(), "will not follow") {
		t.Fatalf("err = %v, want ErrFetch for a refused redirect", err)
	}
}
