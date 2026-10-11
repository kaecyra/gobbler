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
)

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
	err := serve(context.Background(), serveConfig(t), fsys, listen)
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
	ctx, cancel := context.WithCancel(context.Background())
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

func TestServeRejectsListenFailure(t *testing.T) {
	listen := func(context.Context, string) (net.Listener, error) { return nil, errors.New("address in use") }
	err := serve(context.Background(), serveConfig(t), fstest.MapFS{}, listen)
	if err == nil || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeRejectsBadAccessConfig(t *testing.T) {
	cfg := serveConfig(t)
	cfg.Server.DevAuthBypass = false // no team domain or AUD
	if err := serve(context.Background(), cfg, fstest.MapFS{}, tcpListen); err == nil {
		t.Fatal("want error without Access settings")
	}
}
