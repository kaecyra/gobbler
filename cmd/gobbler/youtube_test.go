package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/settings"
	"github.com/kaecyra/gobbler/internal/video"
)

// The router wrapper is a provider the pipeline can take the lookup from.
var (
	_ ingest.Provider         = llmWithDescriptions{}
	_ ingest.DescriptionAware = llmWithDescriptions{}
)

// The lookup reads the key saved on the Deployment page at the moment of each
// call (ADR-00054): off with none, on once saved, off again once cleared, and
// off, with an error in the log, when the saved key cannot be opened.
func TestDescriptionLookupFollowsTheSavedKey(t *testing.T) {
	ctx := context.Background()
	var sawKey string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.URL.Query().Get("key")
		_, _ = w.Write([]byte(`{"items":[{"snippet":{"title":"T","description":"D"}}]}`))
	}))
	t.Cleanup(api.Close)
	dir := t.TempDir()
	cfg := loadCfg(t, dir, b64key(1))
	cfg.Video.APIHost = api.URL
	d := migrated(t, cfg)
	store, err := openSettings(ctx, cfg, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	lookup := newDescriptions(cfg, store, slog.New(slog.DiscardHandler))

	if _, err := lookup.Fetch(ctx, "dQw4w9WgXcQ"); !errors.Is(err, video.ErrNoKey) || sawKey != "" {
		t.Fatalf("with no key: err = %v, key sent = %q", err, sawKey)
	}
	field := settings.Field{Integration: video.Integration, Name: video.FieldAPIKey}
	if err := store.SetSecret(ctx, field, "saved-key"); err != nil {
		t.Fatal(err)
	}
	got, err := lookup.Fetch(ctx, "dQw4w9WgXcQ")
	if err != nil || got.Text != "D" || sawKey != "saved-key" {
		t.Fatalf("with a key: %+v, %v, key sent = %q", got, err, sawKey)
	}
	if err := store.Clear(ctx, field); err != nil {
		t.Fatal(err)
	}
	if _, err := lookup.Fetch(ctx, "dQw4w9WgXcQ"); !errors.Is(err, video.ErrNoKey) {
		t.Fatalf("after clearing: err = %v", err)
	}

	if err := store.SetSecret(ctx, field, "saved-key"); err != nil {
		t.Fatal(err)
	}
	wrong := loadCfg(t, dir, b64key(2))
	wrongStore, err := openSettings(ctx, wrong, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, err = newDescriptions(cfg, wrongStore, slog.New(slog.NewJSONHandler(&buf, nil))).Fetch(ctx, "dQw4w9WgXcQ")
	if !errors.Is(err, video.ErrNoKey) {
		t.Fatalf("unreadable key: err = %v, want the lookup off", err)
	}
	if !strings.Contains(buf.String(), `"level":"ERROR"`) || strings.Contains(buf.String(), "saved-key") {
		t.Errorf("log = %q, want an error without the key", buf.String())
	}
}
