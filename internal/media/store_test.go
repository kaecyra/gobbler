package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
)

// fakeS3 speaks just enough path-style S3 for the media store: put, get and
// delete of objects in one bucket.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}, types: map[string]string{}} }

// unchunk decodes the aws-chunked body minio sends over plain HTTP.
func unchunk(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		line, rest, ok := bytes.Cut(b, []byte("\r\n"))
		if !ok {
			break
		}
		sizeHex, _, _ := strings.Cut(string(line), ";")
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil || n == 0 || int(n) > len(rest) {
			break
		}
		out = append(out, rest[:n]...)
		b = rest[n:]
		b = bytes.TrimPrefix(b, []byte("\r\n"))
	}
	return out
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key, ok := strings.CutPrefix(r.URL.Path, "/media-bkt/")
	if !ok {
		http.Error(w, "wrong bucket "+r.URL.Path, http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
			b = unchunk(b)
		}
		f.objects[key] = b
		f.types[key] = r.Header.Get("Content-Type")
		w.Header().Set("ETag", `"x"`)
	case http.MethodGet:
		b, found := f.objects[key]
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>no such key</Message></Error>`)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Header().Set("ETag", `"x"`)
		_, _ = w.Write(b)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method, http.StatusBadRequest)
	}
}

// storeContract is what every Store must do (ADR-00033 obligation 1).
func storeContract(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Get(ctx, "avatars/nothing.webp"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get of a missing key = %v, want ErrNotFound", err)
	}
	if err := s.Put(ctx, "avatars/a.webp", []byte("one"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "avatars/a.webp", []byte("two"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "avatars/a.webp")
	if err != nil || string(got) != "two" {
		t.Errorf("Get = %q, %v, want the replaced bytes", got, err)
	}
	if err := s.Delete(ctx, "avatars/a.webp"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "avatars/a.webp"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, "avatars/a.webp"); err != nil {
		t.Errorf("Delete of a missing key = %v, want nil", err)
	}
	for _, bad := range []string{"", "../x", "a/../../x", "/abs", "a//b", "a b"} {
		if err := s.Put(ctx, bad, []byte("x"), "image/webp"); err == nil {
			t.Errorf("Put(%q) accepted", bad)
		}
	}
}

func TestLocalStore(t *testing.T) {
	dir := t.TempDir()
	s := NewLocal(filepath.Join(dir, "media"))
	storeContract(t, s)
	if entries, _ := os.ReadDir(filepath.Join(dir, "media", "avatars")); len(entries) != 0 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestS3Store(t *testing.T) {
	fake := newFakeS3()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s, err := NewS3(config.S3{Endpoint: srv.URL, Region: "garage"}, "media-bkt")
	if err != nil {
		t.Fatal(err)
	}
	storeContract(t, s)
	if err := s.Put(context.Background(), "avatars/b.webp", []byte("bytes"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	if string(fake.objects["avatars/b.webp"]) != "bytes" || fake.types["avatars/b.webp"] != "image/webp" {
		t.Errorf("stored %q as %q", fake.objects["avatars/b.webp"], fake.types["avatars/b.webp"])
	}
}

func TestNewChoosesTheStoreFromConfiguration(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Server: config.Server{DataDir: dir}}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*Local); !ok {
		t.Errorf("without S3, New = %T, want *Local", s)
	}
	// S3 for backups alone leaves media on disk.
	cfg.Backup.S3 = config.S3{Endpoint: "http://127.0.0.1:1", Region: "r", Bucket: "backups"}
	if s, err = New(cfg); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*Local); !ok {
		t.Errorf("S3 without a media bucket, New = %T, want *Local", s)
	}
	cfg.Backup.S3.MediaBucket = "media"
	if s, err = New(cfg); err != nil {
		t.Fatal(err)
	} else if _, ok := s.(*S3); !ok {
		t.Errorf("with a media bucket, New = %T, want *S3", s)
	}
}
