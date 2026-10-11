package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// statusRecorder captures the status code a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// requestLogger logs one line per request. It records method, path, status,
// duration and remote address only: never a header, cookie or query string,
// so the Access JWT and the Authorization header cannot reach the log.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			log.InfoContext(r.Context(), "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// recoverer turns a handler panic into a logged 500 instead of a dropped
// connection. http.ErrAbortHandler is re-raised: it is how a handler aborts
// deliberately.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity as net/http does
					panic(v)
				}
				log.ErrorContext(r.Context(), "handler panic",
					"panic", v, "method", r.Method, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
