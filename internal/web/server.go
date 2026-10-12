package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Server timeouts. WriteTimeout is fixed, not derived from configuration: it
// is sized for the slowest expected handler (an LLM call, whose timeout
// config.LLM.Timeout sets and defaults to 60s). If that setting is raised
// above this value, such a response is cut off, so raise this constant too.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 2 * time.Minute
	idleTimeout       = 2 * time.Minute
	shutdownTimeout   = 15 * time.Second
)

func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

// Serve serves h on ln until ctx is cancelled, then stops accepting and gives
// in-flight requests shutdownTimeout to finish. A clean shutdown returns nil.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	down := make(chan struct{})
	srv := newHTTPServer(endStreamsOnShutdown(h, down))
	srv.RegisterOnShutdown(func() { close(down) })
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	slog.InfoContext(ctx, "shutting down http server")
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shut down http server: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}

// endStreamsOnShutdown cancels the context of every event-stream request
// (Accept: text/event-stream, which is what EventSource sends) once down is
// closed. Shutdown waits for in-flight requests, and a stream never finishes on
// its own, so without this one open phone view would hold the server for all of
// shutdownTimeout and then fail the shutdown. Other requests keep the grace
// period.
func endStreamsOnShutdown(h http.Handler, down <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			h.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-down:
				cancel()
			case <-ctx.Done():
			}
		}()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
