package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server timeouts. WriteTimeout is generous because the slowest handler waits
// on an LLM call bounded by config.LLM.Timeout (60s by default).
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
	srv := newHTTPServer(h)
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
