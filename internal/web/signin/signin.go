// Package signin signs people in over OpenID Connect (ADR-00030). The app is a
// client of one issuer, named by configuration: it learns the issuer's
// endpoints and keys from its discovery document, runs the authorization-code
// flow with PKCE, state and nonce, verifies the ID token, and starts a session
// for the verified, lower-cased email. Nothing here names a provider.
//
// It serves three routes: sign-in (layout.SignInPath), the issuer's callback
// (layout.CallbackPath) and sign-out (layout.SignOutPath). Reading the session
// cookie on later requests is package auth's job.
//
// The state, nonce, PKCE verifier and return path of a sign-in in progress
// live in a short-lived cookie, not on the server. The browser that started the
// sign-in is the only party that needs them, they are worthless once the
// callback has run, and a server-side table would be one more thing to write
// and sweep on a public, unauthenticated route. The cookie is __Host-
// prefixed, HttpOnly and SameSite=Lax, so no other host or script can set or
// read it, and the issuer's redirect back, a top-level GET, still carries it.
// It holds nothing that would be a secret from that browser, so it is neither
// signed nor encrypted, and the return path is checked again when it is read.
package signin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// PendingCookie carries a sign-in in progress from SignIn to Callback.
const PendingCookie = "__Host-gobbler-signin"

// pendingLifetime is how long a sign-in may take at the issuer.
const pendingLifetime = 10 * time.Minute

// issuerTimeout bounds every request to the issuer: discovery, keys and the
// token exchange.
const issuerTimeout = 10 * time.Second

// Sessions is what sign-in needs from the session store. *session.Store
// satisfies it.
type Sessions interface {
	Create(ctx context.Context, email string) (string, error)
	Delete(ctx context.Context, token string) error
}

// Settings name the issuer and this app's client registration with it
// (config.OIDC).
type Settings struct {
	// Issuer is the issuer URL exactly as configured; discovery and every ID
	// token must name it character for character.
	Issuer   string
	ClientID string
	// ClientSecret authenticates the token request. It is never logged.
	ClientSecret string
	// PublicBaseURL is the origin the site is reached at; the redirect URI is
	// it plus layout.CallbackPath.
	PublicBaseURL string
	// Lifetime is how long a session lasts (config.Session.Lifetime), and so
	// the session cookie's Max-Age.
	Lifetime time.Duration
}

// Service runs sign-in and sign-out. It is safe for concurrent use.
type Service struct {
	settings Settings
	sessions Sessions
	client   *http.Client
	log      *slog.Logger

	// mu guards the discovered issuer, which is fetched on first use.
	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// Option adjusts a Service.
type Option func(*Service)

// WithLogger sets the logger; the default is slog.Default().
func WithLogger(l *slog.Logger) Option { return func(s *Service) { s.log = l } }

// WithHTTPClient sets the client used to reach the issuer.
func WithHTTPClient(c *http.Client) Option { return func(s *Service) { s.client = c } }

// New returns a Service. It does not contact the issuer: discovery runs on the
// first sign-in and is retried until it succeeds, so an issuer that is down at
// start-up costs sign-in, not the health check, the public pages or the
// sessions that already exist.
func New(st Settings, sessions Sessions, opts ...Option) (*Service, error) {
	switch {
	case st.Issuer == "" || st.ClientID == "" || st.PublicBaseURL == "":
		return nil, errors.New("signin: issuer, client ID and public base URL are required")
	case st.Lifetime <= 0:
		return nil, fmt.Errorf("signin: session lifetime %s must be positive", st.Lifetime)
	case sessions == nil:
		return nil, errors.New("signin: a session store is required")
	}
	s := &Service{settings: st, sessions: sessions, client: &http.Client{Timeout: issuerTimeout}, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// discover returns the OAuth client and ID token verifier for the issuer,
// fetching its discovery document the first time. A failure is not cached.
func (s *Service) discover() (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oauth != nil {
		return s.oauth, s.verifier, nil
	}
	// Not the request's context: the provider keeps it to fetch rotated keys
	// long after this request has ended. The client's timeout bounds it.
	p, err := oidc.NewProvider(s.issuerContext(context.Background()), s.settings.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("discover issuer %s: %w", s.settings.Issuer, err)
	}
	s.oauth = &oauth2.Config{
		ClientID:     s.settings.ClientID,
		ClientSecret: s.settings.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  s.settings.PublicBaseURL + layout.CallbackPath,
		Scopes:       []string{oidc.ScopeOpenID, "email"},
	}
	s.verifier = p.Verifier(&oidc.Config{ClientID: s.settings.ClientID})
	return s.oauth, s.verifier, nil
}

// issuerContext makes ctx carry the client every issuer request uses.
func (s *Service) issuerContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, s.client)
}

// pending is a sign-in in progress, as PendingCookie carries it.
type pending struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"r"`
}

// SignIn starts a sign-in: it remembers where to return to (the next query
// parameter, if it is a path on this site) and sends the browser to the
// issuer.
func (s *Service) SignIn(w http.ResponseWriter, r *http.Request) {
	oc, _, err := s.discover()
	if err != nil {
		s.log.ErrorContext(r.Context(), "sign-in unavailable", "error", err.Error())
		s.refuse(w, r, http.StatusBadGateway, "Sign-in is unavailable right now. Try again in a minute.")
		return
	}
	p := pending{State: rand.Text(), Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier(), Next: LocalPath(r.URL.Query().Get("next"))}
	b, err := json.Marshal(p)
	if err != nil {
		layout.WriteError(w, r, fmt.Errorf("encode sign-in state: %w", err))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: PendingCookie, Value: base64.RawURLEncoding.EncodeToString(b), Path: "/",
		MaxAge: int(pendingLifetime / time.Second), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, oc.AuthCodeURL(p.State, oidc.Nonce(p.Nonce), oauth2.S256ChallengeOption(p.Verifier)), http.StatusSeeOther)
}

// Callback finishes a sign-in: it checks the state, exchanges the code with
// the PKCE verifier, verifies the ID token and its nonce, requires a verified
// email, starts a session and returns to the page the sign-in was for. No
// failure logs the code or any token.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	// One use only, whatever happens next.
	http.SetCookie(w, expired(PendingCookie))
	p, err := readPending(r)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err, "This sign-in has expired. Sign in again.")
		return
	}
	q := r.URL.Query()
	if !equal(q.Get("state"), p.State) {
		s.fail(w, r, http.StatusBadRequest, errors.New("state does not match"), "This sign-in has expired. Sign in again.")
		return
	}
	if e := q.Get("error"); e != "" {
		// The issuer's error code is a short ASCII word, such as access_denied.
		s.fail(w, r, http.StatusForbidden, fmt.Errorf("issuer refused: %.40q", e), "Sign-in was cancelled or refused.")
		return
	}
	code := q.Get("code")
	if code == "" {
		s.fail(w, r, http.StatusBadRequest, errors.New("no code"), "Sign-in failed. Sign in again.")
		return
	}
	email, status, err := s.exchange(r.Context(), code, p)
	if err != nil {
		msg := "Sign-in failed. Sign in again."
		if status == http.StatusForbidden {
			msg = "Your email address is not verified with your sign-in account."
		}
		s.fail(w, r, status, err, msg)
		return
	}

	// Signing in again replaces this browser's previous session.
	if c, err := r.Cookie(auth.SessionCookie); err == nil && c.Value != "" {
		if err := s.sessions.Delete(r.Context(), c.Value); err != nil {
			layout.WriteError(w, r, err)
			return
		}
	}
	token, err := s.sessions.Create(r.Context(), email)
	if err != nil {
		layout.WriteError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: token, Path: "/", MaxAge: int(s.settings.Lifetime / time.Second),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
	s.log.InfoContext(r.Context(), "signed in", "email", email)
	http.Redirect(w, r, LocalPath(p.Next), http.StatusSeeOther)
}

// exchange trades the code for tokens and returns the verified email. The
// status is what to answer when it fails: 502 when the issuer could not be
// asked, 403 for an unverified email, 400 for anything else wrong with the
// answer.
func (s *Service) exchange(ctx context.Context, code string, p pending) (string, int, error) {
	oc, verifier, err := s.discover()
	if err != nil {
		return "", http.StatusBadGateway, err
	}
	ctx = s.issuerContext(ctx)
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(p.Verifier))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			// Only the error code: the body is the issuer's to fill.
			return "", http.StatusBadRequest, fmt.Errorf("token endpoint refused the code: %.40q", re.ErrorCode)
		}
		return "", http.StatusBadGateway, errors.New("token endpoint unreachable")
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", http.StatusBadRequest, errors.New("token response has no ID token")
	}
	// Signature, issuer, audience and expiry. go-oidc's errors name the claim
	// that failed and never quote the token.
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		return "", http.StatusBadRequest, fmt.Errorf("ID token: %w", err)
	}
	if !equal(idt.Nonce, p.Nonce) {
		return "", http.StatusBadRequest, errors.New("ID token nonce does not match")
	}
	var claims struct {
		Email string `json:"email"`
		// Any JSON value: only the boolean true is accepted (ADR-00030).
		EmailVerified any `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil {
		return "", http.StatusBadRequest, fmt.Errorf("ID token claims: %w", err)
	}
	if v, ok := claims.EmailVerified.(bool); !ok || !v {
		return "", http.StatusForbidden, errors.New("email is not verified")
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return "", http.StatusBadRequest, errors.New("ID token email is not a bare address")
	}
	return email, http.StatusOK, nil
}

// SignOut ends the request's session, clears its cookie and goes home. It is
// a private route: a request without a live session never reaches it, and
// has nothing left to end, since an expired session's row is already gone
// (session.Store.Lookup).
func (s *Service) SignOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil && c.Value != "" {
		if err := s.sessions.Delete(r.Context(), c.Value); err != nil {
			layout.WriteError(w, r, err)
			return
		}
	}
	http.SetCookie(w, expired(auth.SessionCookie))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// fail logs why a sign-in failed and shows the person what to do. err must
// carry no code or token.
func (s *Service) fail(w http.ResponseWriter, r *http.Request, status int, err error, message string) {
	s.log.WarnContext(r.Context(), "sign-in failed", "reason", err.Error(), "status", status)
	s.refuse(w, r, status, message)
}

func (s *Service) refuse(w http.ResponseWriter, r *http.Request, status int, message string) {
	layout.Render(w, r, status, layout.ErrorPage(status, message))
}

// readPending decodes the request's PendingCookie.
func readPending(r *http.Request) (pending, error) {
	c, err := r.Cookie(PendingCookie)
	if err != nil {
		return pending{}, errors.New("no sign-in in progress")
	}
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return pending{}, errors.New("malformed sign-in cookie")
	}
	var p pending
	if err := json.Unmarshal(b, &p); err != nil || p.State == "" || p.Nonce == "" || p.Verifier == "" {
		return pending{}, errors.New("malformed sign-in cookie")
	}
	return p, nil
}

// expired returns a cookie that makes the browser drop the cookie name. The
// attributes must match the ones it was set with, or a __Host- cookie is not
// replaced.
func expired(name string) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}

// equal compares two secrets in constant time; an empty one never matches.
func equal(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// LocalPath returns next if it is a path on this site, and "/" otherwise
// (ADR-00030): the return target after sign-in must not be able to send
// anyone elsewhere. A local path starts with one "/", is not "//host" or
// "/\host" (which browsers read as another host), holds no control
// character, parses with no scheme or host, and is not sign-in itself. Any
// fragment is dropped.
func LocalPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsRune(next, '\\') ||
		strings.IndexFunc(next, unicode.IsControl) >= 0 {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" ||
		!strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	if u.Path == layout.SignInPath || u.Path == layout.CallbackPath {
		return "/"
	}
	u.Fragment, u.RawFragment = "", ""
	return u.RequestURI()
}
