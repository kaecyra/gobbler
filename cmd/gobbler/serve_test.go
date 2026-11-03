package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// testContext bounds a test so a regression that blocks fails instead of
// hanging the suite.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func serveConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{Server: config.Server{
		ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true,
	}}
}

func TestServeFailedMigrationNeverListens(t *testing.T) {
	fsys := fstest.MapFS{"00001_bad.sql": {Data: []byte("-- +goose Up\nCREATE TABLE (;\n-- +goose Down\nSELECT 1;\n")}}
	listened := false
	listen := func(context.Context, string) (net.Listener, error) {
		listened = true
		return nil, errors.New("must not be called")
	}
	err := serve(testContext(t), serveConfig(t), fsys, listen)
	if err == nil || !strings.Contains(err.Error(), "00001_bad.sql") {
		t.Fatalf("err = %v, want error naming 00001_bad.sql", err)
	}
	if listened {
		t.Fatal("server started listening after a failed migration")
	}
}

func TestServeRunsHealthAndStopsOnCancel(t *testing.T) {
	addrc := make(chan string, 1)
	listen := func(ctx context.Context, addr string) (net.Listener, error) {
		ln, err := tcpListen(ctx, addr)
		if err == nil {
			addrc <- ln.Addr().String()
		}
		return ln, err
	}
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, serveConfig(t), fstest.MapFS{}, listen) }()

	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve never listened")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health = %d, want 200", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v, want nil after cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop after cancel")
	}
}

// With sign-in configured, serve wires the session store, the identity
// middleware and sign-in before it listens (ADR-00030); the issuer is not
// contacted at start-up. A private page without a session is sent to sign in,
// which only the session-aware identity middleware does; and web.New refuses
// sign-in without the session store that ends a removed member's sessions, so
// serve reaching listen proves that store is wired too.
func TestServeWiresSignIn(t *testing.T) {
	cfg := config.Config{
		Server:  config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()},
		OIDC:    config.OIDC{Issuer: "https://issuer.invalid", ClientID: "c", PublicBaseURL: "https://gobbler.test"},
		Session: config.Session{Lifetime: time.Hour},
	}
	addrc := make(chan string, 1)
	live := func(ctx context.Context, addr string) (net.Listener, error) {
		ln, err := tcpListen(ctx, addr)
		if err == nil {
			addrc <- ln.Addr().String()
		}
		return ln, err
	}
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, db.Migrations(), live) }()
	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("serve returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve never listened")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/recipes", nil)
	if err != nil {
		t.Fatal(err)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/login?next=%2Frecipes" {
		t.Errorf("private page without a session = %d to %q, want 303 to /login?next=%%2Frecipes", resp.StatusCode, loc)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve = %v, want nil after cancel", err)
	}

	stop := errors.New("listening")
	listen := func(context.Context, string) (net.Listener, error) { return nil, stop }
	for name, edit := range map[string]func(c *config.Config){
		"no lifetime": func(c *config.Config) { c.Session.Lifetime = 0 },
		"no base URL": func(c *config.Config) { c.OIDC.PublicBaseURL = "" },
	} {
		bad := cfg
		bad.Server.DataDir = t.TempDir()
		edit(&bad)
		if err := serve(testContext(t), bad, db.Migrations(), listen); err == nil || errors.Is(err, stop) {
			t.Errorf("%s: err = %v, want a start-up error before listening", name, err)
		}
	}
}

func TestServeRejectsListenFailure(t *testing.T) {
	listen := func(context.Context, string) (net.Listener, error) { return nil, errors.New("address in use") }
	err := serve(testContext(t), serveConfig(t), fstest.MapFS{}, listen)
	if err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeRejectsBadAccessConfig(t *testing.T) {
	cfg := serveConfig(t)
	cfg.Server.DevAuthBypass = false // no team domain or AUD
	if err := serve(testContext(t), cfg, fstest.MapFS{}, tcpListen); err == nil {
		t.Fatal("want error without Access settings")
	}
}
