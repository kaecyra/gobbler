// Package auth verifies the Cloudflare Access JWT on every request and puts
// the verified email in the request context (ADR-00003). It is the only place
// identity is established; handlers read it through EmailFromContext and never
// parse a token themselves.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
)

const (
	// HeaderName carries the Access JWT.
	HeaderName = "Cf-Access-Jwt-Assertion"
	// CookieName is the fallback when the header is absent.
	CookieName = "CF_Authorization"
	// DevEmail is the fixed identity in dev-bypass mode.
	DevEmail = "dev@localhost"

	defaultMinRefresh = time.Minute
)

type ctxKey struct{}

// EmailFromContext returns the verified user email Middleware stored on the
// request context. ok is false outside an authenticated request.
func EmailFromContext(ctx context.Context) (email string, ok bool) {
	email, ok = ctx.Value(ctxKey{}).(string)
	return email, ok && email != ""
}

// Authenticator verifies Access tokens for one team and AUD tag.
type Authenticator struct {
	bypass bool
	issuer string
	aud    string
	keys   *keySet
	now    func() time.Time
	log    *slog.Logger
}

// Option adjusts an Authenticator. Only WithLogger is exported: the certs URL,
// client, clock and refresh interval are fixed by production code and
// adjustable only from this package's tests.
type Option func(*Authenticator, *keySet)

// withCertsURL replaces the JWKS URL derived from the team domain.
func withCertsURL(u string) Option { return func(_ *Authenticator, k *keySet) { k.url = u } }

// withClock replaces the time source.
func withClock(now func() time.Time) Option {
	return func(a *Authenticator, k *keySet) { a.now, k.now = now, now }
}

// withMinRefresh sets the shortest gap between JWKS fetches.
func withMinRefresh(d time.Duration) Option {
	return func(_ *Authenticator, k *keySet) { k.minRefresh = d }
}

// WithLogger sets the logger; the default is slog.Default().
func WithLogger(l *slog.Logger) Option { return func(a *Authenticator, _ *keySet) { a.log = l } }

// New builds an Authenticator from the validated configuration. With
// Server.DevAuthBypass it refuses a non-loopback listen address (ADR-00003)
// even though config.Load already does, so a hand-built Config cannot open
// the app.
func New(cfg config.Config, opts ...Option) (*Authenticator, error) {
	a := &Authenticator{now: time.Now, log: slog.Default()}
	k := &keySet{client: http.DefaultClient, now: time.Now, minRefresh: defaultMinRefresh}

	if cfg.Server.DevAuthBypass {
		if !config.IsLoopbackAddr(cfg.Server.ListenAddr) {
			return nil, fmt.Errorf("auth: dev bypass requires a loopback listen address, got %q", cfg.Server.ListenAddr)
		}
		a.bypass = true
	} else {
		host := cfg.Access.TeamDomain
		if host == "" || cfg.Access.AUD == "" {
			return nil, errors.New("auth: team domain and AUD tag are required")
		}
		if strings.ContainsAny(host, "/?#@ ") {
			return nil, fmt.Errorf("auth: team domain %q must be a bare host name", host)
		}
		a.issuer = "https://" + host
		a.aud = cfg.Access.AUD
		k.url = a.issuer + "/cdn-cgi/access/certs"
	}
	for _, o := range opts {
		o(a, k)
	}
	a.keys = k
	if a.bypass {
		a.log.Warn("dev auth bypass is on: requests are not verified", "identity", DevEmail)
	}
	return a, nil
}

// Middleware returns net/http middleware that rejects, with 403, any request
// without a valid token. Requests whose URL path exactly equals one of exempt
// skip verification entirely; the router passes the health check here and
// nothing else (ADR-00003 obligation 2).
func (a *Authenticator) Middleware(exempt ...string) func(http.Handler) http.Handler {
	skip := make(map[string]struct{}, len(exempt))
	for _, p := range exempt {
		skip[p] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			email, err := a.authenticate(r)
			if err != nil {
				a.log.WarnContext(r.Context(), "access denied",
					"reason", err.Error(), "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, email)))
		})
	}
}

func (a *Authenticator) authenticate(r *http.Request) (string, error) {
	if a.bypass {
		return DevEmail, nil
	}
	token := r.Header.Get(HeaderName)
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		return "", reject("no token")
	}
	return a.verify(r.Context(), token)
}

// verify returns the email of a valid token. No error it returns contains any
// part of the token.
func (a *Authenticator) verify(ctx context.Context, token string) (string, error) {
	p, err := parseToken(token)
	if err != nil {
		return "", err
	}
	if p.header.Alg != "RS256" {
		return "", reject("unexpected algorithm")
	}
	if p.header.Kid == "" {
		return "", reject("no kid")
	}
	key, err := a.keys.key(ctx, p.header.Kid)
	switch {
	case errors.Is(err, errUnknownKey):
		return "", reject("unknown signing key")
	case err != nil:
		// Not the sender's fault, but still never admitted.
		return "", fmt.Errorf("cannot verify token: %w", err)
	}
	if err := p.verifySignature(key); err != nil {
		return "", err
	}
	return p.claims.check(a.issuer, a.aud, a.now())
}
