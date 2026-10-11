package web

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerHasTimeouts(t *testing.T) {
	s := newHTTPServer(http.NotFoundHandler())
	if s.ReadHeaderTimeout <= 0 || s.ReadTimeout <= 0 || s.WriteTimeout <= 0 || s.IdleTimeout <= 0 {
		t.Errorf("timeouts must all be set: %+v", s)
	}
}

func TestServeStopsCleanlyOnContextCancel(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) }))
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v, want nil on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if err := Serve(context.Background(), ln, http.NotFoundHandler()); err == nil {
		t.Fatal("want error serving on a closed listener")
	}
}
