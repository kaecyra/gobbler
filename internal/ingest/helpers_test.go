package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/store"
)

const testUserAgent = "gobbler-test/1"

func testConfig() config.Ingestion {
	return config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: 2 * time.Second, UserAgent: testUserAgent}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// env is a real SQLite file with every migration applied, the real catalog and
// store over it, and a pipeline that reads that catalog.
type env struct {
	db      *sql.DB
	catalog *catalog.Store
	store   *store.Store
	ctx     context.Context
	spy     *providerSpy
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return &env{db: d, catalog: catalog.New(d), store: store.New(d), ctx: inDevHousehold(t, d, authedCtx(t)), spy: &providerSpy{}}
}

// inDevHousehold returns ctx carrying the dev household of d, seeded if need
// be, as the membership middleware would put it on a member's request.
func inDevHousehold(t *testing.T, d *sql.DB, ctx context.Context) context.Context {
	t.Helper()
	hs := household.New(d)
	if err := hs.Seed(ctx, "Dev household", auth.DevEmail); err != nil {
		t.Fatalf("seed household: %v", err)
	}
	h, err := hs.ForEmail(ctx, auth.DevEmail)
	if err != nil {
		t.Fatalf("household of %s: %v", auth.DevEmail, err)
	}
	return household.WithContext(ctx, h)
}

// authedCtx returns a context carrying the identity the real auth middleware
// stores (dev bypass is the only way to obtain one outside package auth).
func authedCtx(t *testing.T) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	var got context.Context
	h := a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("auth middleware did not call the handler")
	}
	return got
}

func (e *env) pipeline(t *testing.T, client HTTPClient) *Pipeline {
	t.Helper()
	p, err := New(testConfig(), Deps{HTTP: client, Catalog: e.catalog, Provider: e.spy})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func (e *env) saver() *Saver { return NewSaver(e.store, e.catalog) }

// serve starts an HTTP server answering every request with body, and records
// the User-Agent it saw.
func serve(t *testing.T, body []byte) (srv *httptest.Server, userAgent func() string) {
	t.Helper()
	var mu sync.Mutex
	var ua string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ua = r.UserAgent()
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { mu.Lock(); defer mu.Unlock(); return ua }
}

// providerSpy is a fake LLM provider that counts calls.
type providerSpy struct {
	mu    sync.Mutex
	calls int
}

func (s *providerSpy) Structure(context.Context, Request) (Structured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return Structured{}, nil
}

func (s *providerSpy) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

// roundTripFunc adapts a function to HTTPClient.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, contentType, body string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: h, Body: io.NopCloser(stringsReader(body))}
}

func stringsReader(s string) io.Reader { return bytes.NewReader([]byte(s)) }
