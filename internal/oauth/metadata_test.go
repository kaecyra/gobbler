package oauth_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/oauth"
)

// metadataServer serves handler over TLS on loopback and returns its URL and a
// fetcher that trusts it and has no dial guard, so what is under test is the
// rest of the fetch.
func metadataServer(t *testing.T, timeout time.Duration, limit int64, handler http.HandlerFunc) (string, *oauth.MetadataFetcher) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv.URL, oauth.NewMetadataFetcherFor(timeout, limit, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
}

func serveJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestFetchReadsAClientMetadataDocument(t *testing.T) {
	var base string
	url, f := metadataServer(t, time.Second, 4096, func(w http.ResponseWriter, r *http.Request) {
		serveJSON(`{"client_id":"`+base+r.URL.Path+`","client_name":"Doc client","redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"none","extra":1}`)(w, r)
	})
	base = url
	c, err := f.Fetch(context.Background(), url+"/client.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != url+"/client.json" || c.Name != "Doc client" || len(c.RedirectURIs) != 1 {
		t.Fatalf("client = %+v", c)
	}
}

func TestFetchRefusesADocumentThatIsNotValid(t *testing.T) {
	for name, body := range map[string]string{
		"another client_id": `{"client_id":"https://evil.example/x","redirect_uris":["https://app.example/cb"]}`,
		"a secret":          `{"client_id":"%s","redirect_uris":["https://app.example/cb"],"client_secret":"s"}`,
		"a secret method":   `{"client_id":"%s","redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"client_secret_post"}`,
		"a bad redirect":    `{"client_id":"%s","redirect_uris":["http://app.example/cb"]}`,
		"no redirects":      `{"client_id":"%s"}`,
		"not json":          `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			var base string
			url, f := metadataServer(t, time.Second, 4096, func(w http.ResponseWriter, r *http.Request) {
				serveJSON(strings.ReplaceAll(body, "%s", base+r.URL.Path))(w, r)
			})
			base = url
			if _, err := f.Fetch(context.Background(), url+"/c.json"); !errors.Is(err, oauth.ErrMetadata) {
				t.Fatalf("Fetch = %v, want ErrMetadata", err)
			}
		})
	}
}

func TestFetchRefusesANonJSONAnswerAndABadStatus(t *testing.T) {
	url, f := metadataServer(t, time.Second, 4096, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`{}`))
	})
	if _, err := f.Fetch(context.Background(), url+"/c.json"); !errors.Is(err, oauth.ErrMetadata) {
		t.Fatalf("html answer: %v", err)
	}
	url, f = metadataServer(t, time.Second, 4096, func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	if _, err := f.Fetch(context.Background(), url+"/c.json"); !errors.Is(err, oauth.ErrMetadata) {
		t.Fatalf("404 answer: %v", err)
	}
}

func TestFetchRefusesAnOversizedDocument(t *testing.T) {
	url, f := metadataServer(t, time.Second, 512, serveJSON(`{"client_id":"x","pad":"`+strings.Repeat("a", 2000)+`"}`))
	_, err := f.Fetch(context.Background(), url+"/c.json")
	if !errors.Is(err, oauth.ErrMetadata) || !strings.Contains(err.Error(), "more than 512 bytes") {
		t.Fatalf("Fetch = %v", err)
	}
}

func TestFetchGivesUpOnASlowServer(t *testing.T) {
	release := make(chan struct{})
	url, f := metadataServer(t, 150*time.Millisecond, 4096, func(_ http.ResponseWriter, _ *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	start := time.Now()
	_, err := f.Fetch(context.Background(), url+"/c.json")
	if !errors.Is(err, oauth.ErrMetadata) {
		t.Fatalf("Fetch = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Fetch took %s", time.Since(start))
	}
}

func TestFetchGivesUpOnAServerThatDripsItsBody(t *testing.T) {
	url, f := metadataServer(t, 300*time.Millisecond, 4096, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		for i := 0; i < 50 && r.Context().Err() == nil; i++ {
			_, _ = w.Write([]byte(" "))
			fl.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})
	start := time.Now()
	if _, err := f.Fetch(context.Background(), url+"/c.json"); !errors.Is(err, oauth.ErrMetadata) {
		t.Fatalf("Fetch = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Fetch took %s", time.Since(start))
	}
}

func TestFetchDoesNotFollowARedirect(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(target.Close)
	url, f := metadataServer(t, time.Second, 4096, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/c.json", http.StatusFound)
	})
	if _, err := f.Fetch(context.Background(), url+"/c.json"); !errors.Is(err, oauth.ErrMetadata) {
		t.Fatalf("Fetch = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

func TestFetchRefusesURLsThatAreNotAcceptable(t *testing.T) {
	f := oauth.NewMetadataFetcher()
	for u, why := range map[string]string{
		"http://app.example/client.json":  "must be an https URL",
		"ftp://app.example/client.json":   "must be an https URL",
		"file:///etc/passwd":              "must be an https URL",
		"//app.example/c.json":            "must be an https URL",
		"app.example/c.json":              "must be an https URL",
		"":                                "must be an https URL",
		"https://app.example":             "needs a path",
		"https://app.example/":            "needs a path",
		"https://app.example/c.json#frag": "fragment",
		"https://user@app.example/c.json": "user information",
		"https://app.example/a/../c.json": "dot segments",
		"https://app.example/a/%2e%2e/c":  "dot segments",
		"https:///c.json":                 "no host",
		"https://app.example/c j.json":    "characters a URL cannot hold",
	} {
		_, err := f.Fetch(context.Background(), u)
		if !errors.Is(err, oauth.ErrMetadata) || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: Fetch = %v, want ErrMetadata mentioning %q", u, err, why)
		}
	}
}

// The fetcher the server uses refuses hosts that are private, loopback or
// link-local, even when something answers there. The IP literals are
// connected to directly, so no name resolution is involved.
func TestFetchNeverConnectsToAPrivateLoopbackOrLinkLocalAddress(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)
	f := oauth.NewMetadataFetcher()
	loopbackURL := srv.URL + "/c.json"
	for name, u := range map[string]string{
		"loopback server that answers": loopbackURL,
		"loopback v4":                  "https://127.0.0.1:9/c.json",
		"loopback v6":                  "https://[::1]:9/c.json",
		"localhost":                    "https://localhost:9/c.json",
		"private 10/8":                 "https://10.1.2.3/c.json",
		"private 172.16/12":            "https://172.16.0.1/c.json",
		"private 192.168/16":           "https://192.168.1.1/c.json",
		"link-local metadata service":  "https://169.254.169.254/latest/meta-data",
		"link-local v6":                "https://[fe80::1]/c.json",
		"unique local v6":              "https://[fd00::1]/c.json",
		"unspecified":                  "https://0.0.0.0/c.json",
		"carrier-grade NAT":            "https://100.64.0.1/c.json",
		"v4 mapped into v6 loopback":   "https://[::ffff:127.0.0.1]/c.json",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.Fetch(context.Background(), u)
			if !errors.Is(err, oauth.ErrMetadata) {
				t.Fatalf("Fetch = %v, want ErrMetadata", err)
			}
			if !strings.Contains(err.Error(), "does not connect to") {
				t.Fatalf("refused for another reason than the address guard: %v", err)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatal("the server on loopback was reached")
	}
}

func TestBlockedAddresses(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "fe80::1", "fc00::1", "0.0.0.0", "224.0.0.1", "100.64.0.1", "::ffff:10.0.0.1", "2001:db8::1", "255.255.255.255"} {
		if !oauth.Blocked(a) {
			t.Errorf("%s is not blocked", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"} {
		if oauth.Blocked(a) {
			t.Errorf("%s is blocked", a)
		}
	}
}
