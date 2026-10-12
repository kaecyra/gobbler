package backup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
)

// fakeS3 speaks just enough path-style S3 for S3Store: put, list-v2, delete.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/bkt/")
	switch {
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[key] = string(b)
		w.Header().Set("ETag", `"x"`)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		prefix := r.URL.Query().Get("prefix")
		var sb strings.Builder
		sb.WriteString(`<?xml version="1.0"?><ListBucketResult><Name>bkt</Name><IsTruncated>false</IsTruncated>`)
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				fmt.Fprintf(&sb, "<Contents><Key>%s</Key><Size>1</Size></Contents>", k)
			}
		}
		sb.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, sb.String())
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func TestS3StoreRoundTrip(t *testing.T) {
	fake := &fakeS3{objects: map[string]string{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	store, err := NewS3Store(config.S3{Endpoint: srv.URL, Region: "us-east-1", Bucket: "bkt"})
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "f.db")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Put(ctx, "p/a.db", src); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "q/b.db", src); err != nil {
		t.Fatal(err)
	}
	if fake.objects["p/a.db"] != "data" {
		t.Fatalf("uploaded body = %q", fake.objects["p/a.db"])
	}
	keys, err := store.List(ctx, "p/")
	if err != nil || !slices.Equal(keys, []string{"p/a.db"}) {
		t.Fatalf("List = %v, %v", keys, err)
	}
	if err := store.Delete(ctx, "p/a.db"); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["p/a.db"]; ok {
		t.Fatal("object still present after Delete")
	}
}

func TestKeyPrefix(t *testing.T) {
	for in, want := range map[string]string{"": "", "/": "", "gobbler": "gobbler/", "/a/b/": "a/b/"} {
		if got := KeyPrefix(in); got != want {
			t.Errorf("KeyPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
