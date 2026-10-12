package web

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// serveForTest runs Serve on a loopback listener and returns its address, a
// function that starts shutdown, and the channel Serve's result arrives on.
func serveForTest(t *testing.T, h http.Handler) (addr string, shutdown func(), done <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	res := make(chan error, 1)
	go func() { res <- Serve(ctx, ln, h) }()
	return ln.Addr().String(), cancel, res
}

// An open event stream must not hold shutdown for the whole grace period.
func TestShutdownEndsOpenEventStreams(t *testing.T) {
	open := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
		close(open)
		<-r.Context().Done()
	})
	addr, shutdown, done := serveForTest(t, h)

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close stream: %v", err)
		}
	}()
	<-open

	start := time.Now()
	shutdown()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve with an open stream = %v, want a clean shutdown", err)
		}
	case <-time.After(shutdownTimeout / 3):
		t.Fatalf("shutdown still waiting on the open stream after %s (shutdownTimeout is %s)", time.Since(start), shutdownTimeout)
	}
}

// A request that is not a stream still gets the grace period to finish.
func TestShutdownLetsOrdinaryRequestsFinish(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	addr, shutdown, done := serveForTest(t, h)

	status := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			status <- 0
			return
		}
		status <- resp.StatusCode
		if err := resp.Body.Close(); err != nil {
			t.Logf("close body: %v", err)
		}
	}()
	<-started
	shutdown()
	time.Sleep(200 * time.Millisecond) // shutdown is under way and still waiting
	close(release)
	if got := <-status; got != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want 200", got)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v", err)
	}
}
