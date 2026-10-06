package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	fetchTimeout = 10 * time.Second
	maxJWKSBytes = 1 << 20
)

// keySet caches the team's public keys. It fetches on first use and again when
// a token names a kid it does not hold, but no more often than minRefresh, so
// a stream of bogus kids cannot turn into a stream of requests to Cloudflare.
type keySet struct {
	url        string
	client     *http.Client
	now        func() time.Time
	minRefresh time.Duration

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	lastAttempt time.Time
	attempted   bool
}

var errUnknownKey = errors.New("no signing key for kid")

// key returns the public key for kid, refreshing the cache if kid is unknown
// and the rate limit allows. A failed fetch is returned, never papered over.
func (s *keySet) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if k, ok := s.keys[kid]; ok {
		return k, nil
	}
	if s.attempted && s.now().Sub(s.lastAttempt) < s.minRefresh {
		return nil, errUnknownKey
	}
	s.attempted = true
	s.lastAttempt = s.now()
	keys, err := s.fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh signing keys: %w", err)
	}
	s.keys = keys
	if k, ok := s.keys[kid]; ok {
		return k, nil
	}
	return nil, errUnknownKey
}

func (s *keySet) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	// The fetch serves every waiting request and is rate-limited, so one
	// client hanging up must not abort it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", s.url, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only body; nothing to lose on close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get %s: status %d", s.url, resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBytes)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode key set: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaKey(k.N, k.E)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("key set holds no RSA keys")
	}
	return keys, nil
}

func rsaKey(n, e string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, errors.New("bad modulus")
	}
	eb, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, errors.New("bad exponent")
	}
	exp := new(big.Int).SetBytes(eb)
	if !exp.IsInt64() || exp.Int64() < 3 || exp.Int64() > 1<<31-1 {
		return nil, errors.New("bad exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(exp.Int64())}, nil
}
