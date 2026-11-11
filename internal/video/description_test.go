package video

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const secretKey = "AIza-test-key-123"

type fixedKey string

func (k fixedKey) APIKey(context.Context) (string, error) {
	if k == "" {
		return "", ErrNoKey
	}
	return string(k), nil
}

func apiServer(t *testing.T, status int, body string) (*Descriptions, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	d := &Descriptions{Keys: fixedKey(secretKey), BaseURL: srv.URL + "/"}
	return d, &seen
}

func TestFetchReadsTheSnippet(t *testing.T) {
	d, seen := apiServer(t, 200, `{"items":[{"snippet":{"title":" Pancakes ","description":"2 eggs\nhttps://blog.example/p "}}]}`)
	got, err := d.Fetch(context.Background(), "dQw4w9WgXcQ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Pancakes" || got.Text != "2 eggs\nhttps://blog.example/p" {
		t.Errorf("got %+v", got)
	}
	q := seen.URL.Query()
	if seen.URL.Path != "/youtube/v3/videos" || q.Get("part") != "snippet" || q.Get("id") != "dQw4w9WgXcQ" || q.Get("key") != secretKey {
		t.Errorf("request = %s", seen.URL)
	}
}

func TestFetchWithoutAKeyAsksNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(srv.Close)
	d := &Descriptions{Keys: fixedKey(""), BaseURL: srv.URL}
	if _, err := d.Fetch(context.Background(), "dQw4w9WgXcQ"); !errors.Is(err, ErrNoKey) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestFetchNamesWhatWentWrong(t *testing.T) {
	body := func(reason string) string {
		return `{"error":{"code":403,"message":"nope","errors":[{"domain":"youtube.quota","reason":"` + reason + `"}]}}`
	}
	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"quota":                      {403, body("quotaExceeded"), ErrQuota},
		"invalid key":                {400, body("keyInvalid"), ErrKeyRejected},
		"not enabled":                {403, body("accessNotConfigured"), ErrKeyRejected},
		"not found":                  {404, body("videoNotFound"), ErrVideoUnavailable},
		"private (none)":             {200, `{"items":[]}`, ErrVideoUnavailable},
		"server error":               {500, `oops`, ErrLookup},
		"not json":                   {200, `<html>`, ErrLookup},
		"other reason":               {403, body("forbidden"), ErrLookup},
		"short rate limit":           {403, body("rateLimitExceeded"), ErrLookup},
		"user rate limit":            {403, body("userRateLimitExceeded"), ErrLookup},
		"invalid key (current)":      {400, `{"error":{"code":400,"message":"API key not valid.","errors":[{"message":"API key not valid.","domain":"global","reason":"badRequest"}],"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID","domain":"googleapis.com","metadata":{"service":"youtube.googleapis.com"}}]}}`, ErrKeyRejected},
		"key blocked from the API":   {403, `{"error":{"code":403,"errors":[{"domain":"global","reason":"forbidden"}],"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_SERVICE_BLOCKED","domain":"googleapis.com"}]}}`, ErrKeyRejected},
		"api disabled":               {403, `{"error":{"code":403,"errors":[{"domain":"usageLimits","reason":"accessNotConfigured"}],"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED","domain":"googleapis.com"}]}}`, ErrKeyRejected},
		"api disabled, details only": {403, `{"error":{"code":403,"errors":[{"domain":"global","reason":"forbidden"}],"details":[{"reason":"SERVICE_DISABLED"}]}}`, ErrKeyRejected},
	} {
		t.Run(name, func(t *testing.T) {
			d, _ := apiServer(t, tc.status, tc.body)
			_, err := d.Fetch(context.Background(), "dQw4w9WgXcQ")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), secretKey) {
				t.Errorf("error carries the key: %v", err)
			}
		})
	}
}

// The transport error of net/http prints the request URL, which holds the key.
func TestFetchErrorsNeverCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close() // connection refused
	d := &Descriptions{Keys: fixedKey(secretKey), BaseURL: base}
	_, err := d.Fetch(context.Background(), "dQw4w9WgXcQ")
	if !errors.Is(err, ErrLookup) {
		t.Fatalf("err = %v, want ErrLookup", err)
	}
	if strings.Contains(err.Error(), secretKey) || strings.Contains(err.Error(), "key=") {
		t.Errorf("error carries the key: %v", err)
	}
}

func TestFetchRefusesABadID(t *testing.T) {
	d := &Descriptions{Keys: fixedKey(secretKey)}
	if _, err := d.Fetch(context.Background(), "nope"); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("err = %v", err)
	}
}

func TestIsYouTubeHost(t *testing.T) {
	for host, want := range map[string]bool{
		"youtube.com": true, "www.youtube.com": true, "m.youtube.com": true, "youtu.be": true,
		"www.youtube-nocookie.com": true, "notyoutube.com": false, "youtube.com.evil.example": false, "blog.example": false,
	} {
		if got := IsYouTubeHost(host); got != want {
			t.Errorf("IsYouTubeHost(%q) = %v, want %v", host, got, want)
		}
	}
}
