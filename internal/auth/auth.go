// Package auth establishes the identity of every request and puts the verified
// email in the request context. It is the only place identity is established;
// handlers read it through EmailFromContext and never parse a token or a
// cookie themselves.
//
// The identity comes from one of three sources, chosen at start-up:
//
//   - the development bypass, on a loopback listener only (ADR-00003);
//   - the sign-in session cookie, when sign-in is configured (ADR-00030);
//   - otherwise the Cloudflare Access JWT (ADR-00003), until it is removed.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/session"
)

const (
	// HeaderName carries the Access JWT.
	HeaderName = "Cf-Access-Jwt-Assertion"
	// CookieName is the fallback when the header is absent.
	CookieName = "CF_Authorization"
	// SessionCookie carries the sign-in session token (ADR-00030). The
	// __Host- prefix makes the browser refuse it unless it is Secure, has
	// Path=/ and names no domain, so no other host can set or read it.
	SessionCookie = "__Host-gobbler-session"
	// DevEmail is the default identity in dev-bypass mode.
	DevEmail = "dev@localhost"
	// DevOtherEmail is a second seeded identity in dev-bypass mode, a member
	// of a household DevEmail is not, so tests can act as a member of another
	// household.
	DevOtherEmail = "other@localhost"
	// DevIdentityHeader overrides the dev-bypass identity for one request, so
	// tests can act as several members and non-members. It is read only when
	// the bypass is on, which is valid only on a loopback listener; with
	// Access verification on it is ignored like any other header.
	DevIdentityHeader = "X-Gobbler-Dev-Identity"

	defaultMinRefresh = time.Minute
)

type ctxKey struct{}

// EmailFromContext returns the verified user email Middleware stored on the
// request context. ok is false outside an authenticated request.
func EmailFromContext(ctx context.Context) (email string, ok bool) {
	email, ok = ctx.Value(ctxKey{}).(string)
	return email, ok && email != ""
}

// SessionLookup finds the email of the live session a cookie token names.
// *session.Store satisfies it. A token that names no live session is
// session.ErrNoSession; any other error is a failure to look.
type SessionLookup interface {
	Lookup(ctx context.Context, token string) (string, error)
}

// Authenticator establishes the identity of a request from the source New
// chose: the dev bypass, a sign-in session, or an Access token for one team
// and AUD tag.
type Authenticator struct {
	bypass bool
	// sessions is set when identity comes from the sign-in session cookie.
	sessions SessionLookup
	// signIn is the path an unauthenticated full-page GET is sent to, with
	// sessions.
	signIn string
	issuer string
	aud    string
	keys   *keySet
	now    func() time.Time
	log    *slog.Logger
}

// Option adjusts an Authenticator. WithLogger and WithSessions are exported:
// the certs URL, client, clock and refresh interval are fixed by production
// code and adjustable only from this package's tests.
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

// WithSessions supplies the session store that identity comes from when
// sign-in is configured, and the sign-in path a full-page GET without a
// session is redirected to. New requires it exactly then.
func WithSessions(s SessionLookup, signInPath string) Option {
	return func(a *Authenticator, _ *keySet) { a.sessions, a.signIn = s, signInPath }
}

// New builds an Authenticator from the validated configuration. With
// Server.DevAuthBypass it refuses a non-loopback listen address (ADR-00003)
// even though config.Load already does, so a hand-built Config cannot open
// the app. The bypass wins over everything else: it stays as it is
// (ADR-00030), so on a bypass listener a session is never consulted. Without
// it, a configured sign-in (config.OIDC.Configured) makes the session cookie
// the identity and requires WithSessions; otherwise Access is.
func New(cfg config.Config, opts ...Option) (*Authenticator, error) {
	a := &Authenticator{now: time.Now, log: slog.Default()}
	k := &keySet{client: http.DefaultClient, now: time.Now, minRefresh: defaultMinRefresh}
	for _, o := range opts {
		o(a, k)
	}

	switch {
	case cfg.Server.DevAuthBypass:
		if !config.IsLoopbackAddr(cfg.Server.ListenAddr) {
			return nil, fmt.Errorf("auth: dev bypass requires a loopback listen address, got %q", cfg.Server.ListenAddr)
		}
		a.bypass, a.sessions = true, nil
	case cfg.OIDC.Configured():
		if a.sessions == nil || a.signIn == "" {
			return nil, errors.New("auth: sign-in is configured but no session store was given")
		}
	default:
		a.sessions = nil
		host := cfg.Access.TeamDomain
		if host == "" || cfg.Access.AUD == "" {
			return nil, errors.New("auth: team domain and AUD tag are required")
		}
		if strings.ContainsAny(host, "/?#@ ") {
			return nil, fmt.Errorf("auth: team domain %q must be a bare host name", host)
		}
		a.issuer = "https://" + host
		a.aud = cfg.Access.AUD
		if k.url == "" {
			k.url = a.issuer + "/cdn-cgi/access/certs"
		}
	}
	a.keys = k
	if a.bypass {
		a.log.Warn("dev auth bypass is on: requests are not verified", "identity", DevEmail)
	}
	return a, nil
}

// Middleware returns net/http middleware that refuses any request without a
// verified identity, unless public reports that its URL path is public
// (ADR-00029). A public request is served either way: with the verified email
// when it carries valid proof, and with no identity when it carries none or
// proof that fails. A nil public makes no path public. The router passes the
// public-path list here; nothing else is exempt. public is checked against the
// raw path, so it only decides whether an identity is required. Admission
// stays with the membership middleware, which re-checks the path.
//
// How a private request without an identity is refused depends on the source.
// Under Access it is 403, as before. With sign-in (ADR-00030) a full-page GET
// is redirected to sign-in, which returns to the page afterwards, and any
// other request, including an HTMX one, is 401.
func (a *Authenticator) Middleware(public func(path string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			email, err := a.authenticate(r)
			switch {
			case err == nil:
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, email)))
			case public != nil && public(r.URL.Path):
				if !errors.Is(err, errNoToken) {
					// A public page is served regardless, so a bad token costs
					// the visitor nothing but their identity; log it all the same.
					a.log.InfoContext(r.Context(), "public request served without identity",
						"reason", err.Error(), "method", r.Method, "path", r.URL.Path)
				}
				next.ServeHTTP(w, r)
			case a.sessions != nil:
				a.signInRequired(w, r, err)
			default:
				a.log.WarnContext(r.Context(), "access denied",
					"reason", err.Error(), "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				http.Error(w, "forbidden", http.StatusForbidden)
			}
		})
	}
}

// signInRequired answers a private request that carries no live session.
// A failure to look the session up is not "signed out": it is a 500, so a
// broken database does not send everyone round the sign-in loop.
func (a *Authenticator) signInRequired(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, errNoToken) && !errors.Is(err, session.ErrNoSession) {
		a.log.ErrorContext(r.Context(), "session lookup failed",
			"error", err.Error(), "method", r.Method, "path", r.URL.Path)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet && r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, a.signIn+"?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	http.Error(w, "sign-in required", http.StatusUnauthorized)
}

func (a *Authenticator) authenticate(r *http.Request) (string, error) {
	if a.bypass {
		return devIdentity(r)
	}
	if a.sessions != nil {
		return a.sessionIdentity(r)
	}
	token := r.Header.Get(HeaderName)
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		return "", errNoToken
	}
	return a.verify(r.Context(), token)
}

// sessionIdentity returns the email of the session the request's cookie
// names. The store never puts the token in an error, so the error may be
// logged.
func (a *Authenticator) sessionIdentity(r *http.Request) (string, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return "", errNoToken
	}
	email, err := a.sessions.Lookup(r.Context(), c.Value)
	if err != nil {
		return "", fmt.Errorf("session: %w", err)
	}
	return email, nil
}

// devIdentity returns the identity of a bypass-mode request: DevEmail, or the
// address in DevIdentityHeader. A malformed override is refused rather than
// falling back to DevEmail, so a typo in a test cannot act as a member.
func devIdentity(r *http.Request) (string, error) {
	v := r.Header.Get(DevIdentityHeader)
	if v == "" {
		return DevEmail, nil
	}
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || v != strings.ToLower(v) {
		return "", reject("malformed dev identity")
	}
	return v, nil
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
