package video

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fetcher is tested at the HTTP boundary only: a local server stands in for
// YouTube's image host.
func serve(t *testing.T, h http.HandlerFunc) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Fetcher{BaseURL: srv.URL}
}

func TestFetchDownloadsTheHqdefaultImage(t *testing.T) {
	var path string
	f := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegbytes"))
	})
	got, err := f.Fetch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/vi/"+id+"/hqdefault.jpg" {
		t.Errorf("requested %q, want the hqdefault path", path)
	}
	if string(got.Content) != "jpegbytes" || got.ContentType != "image/jpeg" {
		t.Errorf("got %q %q", got.Content, got.ContentType)
	}
}

func TestFetchRefusesWhatIsNotAnImageWorthKeeping(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"not found": func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
		"html": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<p>hi</p>"))
		},
		"svg": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte("<svg/>"))
		},
		"empty": func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "image/jpeg") },
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte(strings.Repeat("x", MaxThumbnailBytes+1)))
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := serve(t, h).Fetch(context.Background(), id); err == nil {
				t.Fatalf("Fetch succeeded with %d bytes, want an error", len(got.Content))
			}
		})
	}
}

func TestFetchRejectsAMalformedIDBeforeAnyRequest(t *testing.T) {
	called := false
	f := serve(t, func(http.ResponseWriter, *http.Request) { called = true })
	_, err := f.Fetch(context.Background(), "../etc/passwd")
	if !errors.Is(err, ErrInvalidID) || called {
		t.Fatalf("err = %v, request made = %v; want ErrInvalidID and no request", err, called)
	}
}

// The download is pinned to the image host: a redirect is a failed download,
// and nothing is stored.
func TestFetchDoesNotFollowARedirect(t *testing.T) {
	elsewhere := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere = true
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("other host bytes"))
	}))
	t.Cleanup(target.Close)
	f := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })

	if got, err := f.Fetch(context.Background(), id); err == nil {
		t.Fatalf("Fetch followed a redirect and returned %q", got.Content)
	}
	if elsewhere {
		t.Error("the redirect target was requested")
	}

	st := &memStore{}
	if err := EnsureThumbnail(context.Background(), st, f, id); err == nil {
		t.Error("EnsureThumbnail succeeded across a redirect")
	}
	if st.saved {
		t.Error("a thumbnail was stored after a redirect")
	}
}

type memStore struct{ saved bool }

func (*memStore) HasThumbnail(context.Context, string) (bool, error) { return false, nil }

func (m *memStore) SaveThumbnail(context.Context, string, []byte, string) error {
	m.saved = true
	return nil
}
