package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/sharing"
	"github.com/kaecyra/gobbler/internal/store"
)

// Video IDs, one per save path, so each path's download is told apart: a
// thumbnail already stored for an ID is not downloaded again.
const (
	cloneVideo   = "cloneVid001"
	restoreVideo = "restoreVid1"
	shareVideo   = "shareVideo1"
	ingestVideo  = "ingestVideo"
)

// thumbnailHost stands in for the configured thumbnail host, the HTTP boundary,
// and records the video IDs asked for.
type thumbnailHost struct {
	*httptest.Server
	mu   sync.Mutex
	seen map[string]bool
}

func newThumbnailHost(t *testing.T) *thumbnailHost {
	t.Helper()
	h := &thumbnailHost{seen: map[string]bool{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vi/"), "/hqdefault.jpg")
		h.mu.Lock()
		h.seen[id] = true
		h.mu.Unlock()
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg"))
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *thumbnailHost) asked(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.seen[id]
}

// verifiedContext is a request context carrying email's verified identity, as
// the dev-bypass middleware sets it.
func verifiedContext(t *testing.T, email string) context.Context {
	t.Helper()
	a, err := auth.New(devConfig())
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	if ctx == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return ctx
}

func videoRecipe(name, videoID string) recipe.Recipe {
	return recipe.Recipe{
		Name: name, VideoID: videoID,
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s", Text: "Do it."}}}},
	}
}

// Every save path that stores a recipe with a video downloads its thumbnail
// from the configured host: the router hands one fetcher, built from
// configuration, to ingest, history (restore and clone) and sharing, so no
// path falls back to the real image host.
func TestEverySavePathDownloadsThumbnailsFromTheConfiguredHost(t *testing.T) {
	host := newThumbnailHost(t)
	cfg := devConfig()
	cfg.Video.ThumbnailHost = host.URL
	cfg.Ingestion = config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "test"}
	d := testDB(t)
	h := testRouter(t, cfg, Deps{DB: d, Backup: idleBackup{}})

	// The dev households are seeded on a member's first request.
	for _, who := range []string{auth.DevEmail, auth.DevOtherEmail} {
		do(h, http.MethodGet, "/", who)
	}
	ctx := context.Background()
	hs := household.New(d)
	mine, err := hs.ForEmail(ctx, auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := hs.ForEmail(ctx, auth.DevOtherEmail)
	if err != nil {
		t.Fatal(err)
	}
	myCtx := household.WithContext(verifiedContext(t, auth.DevEmail), mine)
	theirCtx := household.WithContext(verifiedContext(t, auth.DevOtherEmail), theirs)
	rec := store.New(d)

	t.Run("history clone", func(t *testing.T) {
		id, err := rec.Create(myCtx, videoRecipe("Clone me", cloneVideo))
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(t, "clone", post(h, "/recipes/"+strconv.FormatInt(id, 10)+"/clone", auth.DevEmail, nil), http.StatusSeeOther)
		if !host.asked(cloneVideo) {
			t.Error("clone did not download the thumbnail from the configured host")
		}
	})

	t.Run("history restore", func(t *testing.T) {
		id, err := rec.Create(myCtx, videoRecipe("Restore me", restoreVideo))
		if err != nil {
			t.Fatal(err)
		}
		// Version 2 drops the video; restoring version 1 brings it back.
		if err := rec.Update(myCtx, id, videoRecipe("Restore me", "")); err != nil {
			t.Fatal(err)
		}
		path := "/recipes/" + strconv.FormatInt(id, 10) + "/history/1/restore"
		wantStatus(t, "restore", post(h, path, auth.DevEmail, url.Values{"confirm": {"yes"}}), http.StatusSeeOther)
		if !host.asked(restoreVideo) {
			t.Error("restore did not download the thumbnail from the configured host")
		}
	})

	t.Run("share accept", func(t *testing.T) {
		id, err := rec.Create(myCtx, videoRecipe("Share me", shareVideo))
		if err != nil {
			t.Fatal(err)
		}
		svc := sharing.New(rec, hs, catalog.New(d))
		if err := svc.Send(myCtx, id, auth.DevOtherEmail); err != nil {
			t.Fatal(err)
		}
		inbox, err := svc.Inbox(theirCtx)
		if err != nil || len(inbox) != 1 {
			t.Fatalf("inbox = %v, %v; want the one share", inbox, err)
		}
		wantStatus(t, "accept", post(h, "/shares/"+strconv.FormatInt(inbox[0].ID, 10)+"/accept", auth.DevOtherEmail, nil), http.StatusSeeOther)
		if !host.asked(shareVideo) {
			t.Error("accepting a share did not download the thumbnail from the configured host")
		}
	})

	t.Run("ingest save", func(t *testing.T) {
		sub := post(h, "/ingest", auth.DevEmail, url.Values{"text": {"Toast\n\n1. Toast the bread."}})
		wantStatus(t, "ingest", sub, http.StatusSeeOther)
		review := sub.Header().Get("Location")
		form := url.Values{
			"name": {"Toast"}, "video": {ingestVideo},
			"c.0.name": {""}, "c.0.l.0.qty": {"2"}, "c.0.l.0.ing": {"bread"}, "c.0.l.0.propose": {"1"}, "c.0.l.0.raw": {"2 bread"}, "c.0.s.0.id": {"toast"}, "c.0.s.0.text": {"Toast the bread."}, "c.0.s.0.to": {"0"},
		}
		wantStatus(t, "confirm", post(h, review+"/confirm", auth.DevEmail, form), http.StatusSeeOther)
		if !host.asked(ingestVideo) {
			t.Error("ingest save did not download the thumbnail from the configured host")
		}
	})
}
