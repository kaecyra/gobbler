// Package signintest is a fake OpenID Connect issuer for tests (ADR-00030
// obligation 6): sign-in is tested against an issuer the test serves itself,
// never a real provider. It serves discovery, an authorization endpoint that
// approves at once, a token endpoint that checks the client and PKCE, and its
// signing keys. Only tests import it.
package signintest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Client credentials the issuer accepts.
const (
	ClientID     = "gobbler-test-client"
	ClientSecret = "test-client-secret"
	// Email is the address the issuer signs in as unless Issuer.Email is set.
	Email = "Member@Example.com"
	kid   = "test-key"
)

var (
	keyOnce       sync.Once
	key, otherKey *rsa.PrivateKey
)

func keys() (*rsa.PrivateKey, *rsa.PrivateKey) {
	keyOnce.Do(func() {
		var err error
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if otherKey, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return key, otherKey
}

// grant is an authorization code the issuer has handed out.
type grant struct {
	challenge, nonce, redirect string
}

// Issuer is a running fake issuer. Its URL is the issuer identifier.
type Issuer struct {
	*httptest.Server

	mu sync.Mutex
	// Email is the email the next ID token carries.
	Email string
	// Claims, when set, edits the next ID token's claims before signing.
	Claims func(c map[string]any)
	// WrongKey signs ID tokens with a key the JWKS does not publish, under
	// the published key's ID.
	WrongKey bool
	// Issued records every code, access token and ID token handed out, so a
	// test can check none of them reached a log.
	Issued []string
	codes  map[string]grant
}

// New starts an issuer and stops it when the test ends.
func New(t *testing.T) *Issuer {
	t.Helper()
	keys()
	i := &Issuer{Email: Email, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", i.discovery)
	mux.HandleFunc("GET /authorize", i.authorize)
	mux.HandleFunc("POST /token", i.token)
	mux.HandleFunc("GET /jwks", i.jwks)
	i.Server = httptest.NewServer(mux)
	t.Cleanup(i.Close)
	return i
}

// Set edits the issuer's behaviour under its lock.
func (i *Issuer) Set(f func(i *Issuer)) {
	i.mu.Lock()
	defer i.mu.Unlock()
	f(i)
}

// Secrets returns everything the issuer has handed out.
func (i *Issuer) Secrets() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.Issued...)
}

func (i *Issuer) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                i.URL,
		"authorization_endpoint":                i.URL + "/authorize",
		"token_endpoint":                        i.URL + "/token",
		"jwks_uri":                              i.URL + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

// authorize approves at once and sends the browser back with a code.
func (i *Issuer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("nonce") == "" ||
		!strings.Contains(" "+q.Get("scope")+" ", " openid ") {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	code := "code-" + rand.Text()
	i.mu.Lock()
	i.codes[code] = grant{challenge: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri")}
	i.Issued = append(i.Issued, code)
	i.mu.Unlock()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// token exchanges a code for tokens, checking the client and the PKCE
// verifier as a real issuer does.
func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != ClientID || secret != ClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	code := r.PostForm.Get("code")
	g, found := i.codes[code]
	delete(i.codes, code) // one use only
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirect ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": i.URL, "aud": ClientID, "sub": "subject-1", "email": i.Email, "email_verified": true,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": g.nonce,
	}
	if i.Claims != nil {
		i.Claims(claims)
	}
	signer, _ := keys()
	if i.WrongKey {
		_, signer = keys()
	}
	idToken := sign(signer, claims)
	access := "access-" + rand.Text()
	i.Issued = append(i.Issued, idToken, access)
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": 3600, "id_token": idToken,
	})
}

func (i *Issuer) jwks(w http.ResponseWriter, _ *http.Request) {
	k, _ := keys()
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
		"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes()),
	}}})
}

// sign returns claims as an RS256 JWT signed by k under the issuer's key ID.
func sign(k *rsa.PrivateKey, claims map[string]any) string {
	h, err := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	if err != nil {
		panic(err)
	}
	c, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	signing := b64(h) + "." + b64(c)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status is sent; the client under test reports the broken body.
		return
	}
}

// Authorize follows the app's redirect to the issuer the way a browser does,
// and returns the callback URL the issuer sends the browser back to.
func (i *Issuer) Authorize(t *testing.T, authURL string) *url.URL {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d", res.StatusCode)
	}
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return back
}
