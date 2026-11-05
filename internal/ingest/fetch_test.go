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

// ADR-00040: loopback is admitted for the page fetch only when the
// configuration allows it, and a private address that is not loopback is
// refused either way.
func TestFetchLoopbackFollowsTheConfiguration(t *testing.T) {
	srv, _ := serve(t, []byte("<html></html>"))
	u, _ := url.Parse(srv.URL)
	e := newEnv(t)
	pipeline := func(allow bool) *Pipeline {
		cfg := testConfig()
		cfg.AllowLoopbackFetch = allow
		p, err := New(cfg, Deps{Catalog: e.catalog})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, _, err := pipeline(false).fetch(e.ctx, "http://127.0.0.1:"+u.Port()+"/"); !errors.Is(err, ErrFetch) || !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("loopback, option off: err = %v, want ErrFetch wrapping ErrBlocked", err)
	}
	body, _, err := pipeline(true).fetch(e.ctx, "http://127.0.0.1:"+u.Port()+"/")
	if err != nil || string(body) != "<html></html>" {
		t.Fatalf("loopback, option on: body %q, err %v", body, err)
	}
	for _, allow := range []bool{false, true} {
		for _, target := range []string{"http://10.0.0.1/", "http://192.168.1.1/"} {
			if _, _, err := pipeline(allow).fetch(e.ctx, target); !errors.Is(err, netguard.ErrBlocked) {
				t.Errorf("%s, option %v: err = %v, want ErrBlocked", target, allow, err)
			}
		}
	}
}
