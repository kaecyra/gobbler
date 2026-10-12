package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
)

// csrfRouter is the real router, with a migrated database so the mounted
// feature routes (ingest, recipe view) are the real handlers (ADR-00016).
func csrfRouter(t *testing.T, log *slog.Logger) http.Handler {
	t.Helper()
	ctx := context.Background()
	cfg := devConfig()
	cfg.Server.DataDir = t.TempDir()
	cfg.Ingestion.ConfidenceThreshold = 0.7
	cfg.Ingestion.FetchTimeout = 5 * time.Second
	cfg.Ingestion.UserAgent = "gobbler-test"
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	return testRouter(t, cfg, Deps{DB: d, Logger: log})
}

func postIngest(h http.Handler, hdr ...string) *httptest.ResponseRecorder {
	body := url.Values{"text": {"2 eggs\n1 cup flour\nWhisk."}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCrossSitePostToFeatureRouteIsRefused(t *testing.T) {
	rec := postIngest(csrfRouter(t, nil), "Sec-Fetch-Site", "cross-site")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST /ingest = %d, want 403", rec.Code)
	}
}

func TestSameOriginAndMetadataFreePostsReachTheHandler(t *testing.T) {
	h := csrfRouter(t, nil)
	for name, hdr := range map[string][]string{
		"same-origin": {"Sec-Fetch-Site", "same-origin"},
		"no metadata": nil,
	} {
		if rec := postIngest(h, hdr...); rec.Code == http.StatusForbidden {
			t.Errorf("%s POST /ingest = 403, want it to reach the handler", name)
		}
	}
}

func TestCrossSiteGetIsAllowed(t *testing.T) {
	rec := get(csrfRouter(t, nil), "/ingest", "Sec-Fetch-Site", "cross-site")
	if rec.Code != http.StatusOK {
		t.Errorf("cross-site GET /ingest = %d, want 200", rec.Code)
	}
}

func TestHealthIsUnaffectedByCrossSiteMetadata(t *testing.T) {
	rec := get(csrfRouter(t, nil), HealthPath, "Sec-Fetch-Site", "cross-site")
	if rec.Code != http.StatusOK {
		t.Errorf("cross-site GET %s = %d, want 200", HealthPath, rec.Code)
	}
}

func TestCrossOriginRefusalIsAccessLogged(t *testing.T) {
	var buf bytes.Buffer
	h := csrfRouter(t, slog.New(slog.NewJSONHandler(&buf, nil)))
	postIngest(h, "Sec-Fetch-Site", "cross-site")
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"request"`) && strings.Contains(line, `"method":"POST"`) &&
			strings.Contains(line, `"path":"/ingest"`) && strings.Contains(line, `"status":403`) {
			found = true
		}
	}
	if !found {
		t.Errorf("no access log line with status 403 for the refused POST:\n%s", buf.String())
	}
}
