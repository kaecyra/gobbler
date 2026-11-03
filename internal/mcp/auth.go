package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/oauth"
)

// ErrInvalidCredential is returned (wrapped) by a Verifier for a credential it
// does not accept: unknown, expired or revoked. Any other error is a failure to
// check, and is not a refusal.
var ErrInvalidCredential = errors.New("invalid credential")

const (
	// KindAPIKey is the Kind of a credential that is an API key (ADR-00031).
	KindAPIKey = "api_key"
	// KindOAuth is the Kind of a credential that is an OAuth access token
	// (ADR-00034). Its ID is the grant the token belongs to, not the token.
	KindOAuth = "oauth_grant"
)

// Credential is who a verified bearer credential acts as. Exactly one of Email
// and HouseholdID is set: a personal credential acts as a person, whose
// household is decided by the membership check (ADR-00018), and a household
// credential acts for its household.
type Credential struct {
	// Kind says which kind of credential it is, and ID names it among that
	// kind's credentials. Neither is secret.
	Kind string
	ID   int64
	// Ref and CreatedAt, with the owner below, are the snapshot of the
	// credential taken when it was verified: a key's prefix, a grant's client
	// id, and when it was made. The write log records them so that a credential
	// revoked mid-request is still logged, and an id given to a later
	// credential never shows the old one's writes.
	Ref       string
	CreatedAt time.Time

	Email         string
	HouseholdID   int64
	HouseholdName string
}

// Subject names the credential for a rate limit or a log line, never the
// secret.
func (c Credential) Subject() string { return fmt.Sprintf("%s:%d", c.Kind, c.ID) }

// Verifier checks the secret of a bearer credential.
type Verifier interface {
	Verify(ctx context.Context, secret string) (Credential, error)
}

type credentialKey struct{}

// WithCredential returns ctx carrying c as the credential of the request.
func WithCredential(ctx context.Context, c Credential) context.Context {
	return context.WithValue(ctx, credentialKey{}, c)
}

// CredentialFrom returns the credential Bearer stored on the request context.
func CredentialFrom(ctx context.Context) (Credential, bool) {
	c, ok := ctx.Value(credentialKey{}).(Credential)
	return c, ok && c.Kind != ""
}

// Bearer authenticates a request by its Authorization: Bearer header alone. A
// request with no bearer credential, or one v does not accept, gets 401 with
// WWW-Authenticate: Bearer (see Unauthorized) and never a redirect to sign-in (ADR-00034). A
// failure to check is a 500, so a broken database does not read as a revoked
// key. The credential is stored on the request context.
func Bearer(v Verifier, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret, ok := bearerSecret(r)
			if !ok {
				Unauthorized(w, r)
				return
			}
			c, err := v.Verify(r.Context(), secret)
			switch {
			case errors.Is(err, ErrInvalidCredential):
				Unauthorized(w, r)
				return
			case err != nil:
				log.ErrorContext(r.Context(), "mcp credential check failed", "error", err.Error(), "path", r.URL.Path)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithCredential(r.Context(), c)))
		})
	}
}

// Site says where the endpoint is reached, so a refusal can point a client at
// the OAuth Protected Resource Metadata (RFC 9728) and a token's audience can
// be checked. Package web works it out for each request and puts it on the
// context with WithSite; package mcp must not import it.
type Site struct {
	// Origin is the public origin, with no trailing slash. The audience of a
	// token is the endpoint under it (oauth.ResourceURL).
	Origin string
	// MetadataURL is the Protected Resource Metadata of the endpoint.
	MetadataURL string
}

type siteKey struct{}

// WithSite returns ctx carrying s for Bearer and the verifiers behind it.
func WithSite(ctx context.Context, s Site) context.Context {
	return context.WithValue(ctx, siteKey{}, s)
}

// SiteFrom returns the Site on ctx; the zero Site when there is none.
func SiteFrom(ctx context.Context) Site {
	s, _ := ctx.Value(siteKey{}).(Site)
	return s
}

// Unauthorized answers 401 for a request with no credential the endpoint
// accepts. Its WWW-Authenticate names the Protected Resource Metadata when the
// request's Site knows it (RFC 9728 section 5.1), which is how an OAuth client
// finds the authorization server.
func Unauthorized(w http.ResponseWriter, r *http.Request) {
	challenge := "Bearer"
	if u := SiteFrom(r.Context()).MetadataURL; u != "" {
		challenge += fmt.Sprintf(" resource_metadata=%q", u)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	http.Error(w, "a valid API key or access token is required", http.StatusUnauthorized)
}

// bearerSecret returns the secret of an "Authorization: Bearer <secret>"
// header.
func bearerSecret(r *http.Request) (string, bool) {
	scheme, secret, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	secret = strings.TrimSpace(secret)
	if !found || !strings.EqualFold(scheme, "Bearer") || secret == "" || strings.ContainsAny(secret, " \t") {
		return "", false
	}
	return secret, true
}

// KeyLookup is what KeyVerifier calls on the key store; *apikey.Store
// satisfies it.
type KeyLookup interface {
	Lookup(ctx context.Context, secret string) (apikey.Principal, error)
}

// KeyVerifier accepts API keys (ADR-00031).
type KeyVerifier struct{ Keys KeyLookup }

// Verify implements Verifier.
func (k KeyVerifier) Verify(ctx context.Context, secret string) (Credential, error) {
	p, err := k.Keys.Lookup(ctx, secret)
	if errors.Is(err, apikey.ErrUnknownKey) {
		return Credential{}, fmt.Errorf("%w: api key", ErrInvalidCredential)
	}
	if err != nil {
		return Credential{}, err
	}
	return Credential{Kind: KindAPIKey, ID: p.KeyID, Ref: p.Prefix, CreatedAt: p.CreatedAt, Email: p.Email, HouseholdID: p.HouseholdID, HouseholdName: p.HouseholdName}, nil
}

// TokenLookup is what OAuthVerifier calls on the OAuth store; *oauth.Store
// satisfies it.
type TokenLookup interface {
	Verify(ctx context.Context, token, resource string) (oauth.Principal, error)
}

// OAuthVerifier accepts OAuth access tokens (ADR-00034) and hands every other
// secret to Next, so one endpoint takes both. A token is told from a key by its
// prefix. It is held to the audience of the request's Site: with no origin
// known it accepts no token, since it cannot say what the token was meant for.
type OAuthVerifier struct {
	Tokens TokenLookup
	Next   Verifier
}

// Verify implements Verifier.
func (o OAuthVerifier) Verify(ctx context.Context, secret string) (Credential, error) {
	if !strings.HasPrefix(secret, oauth.AccessTokenPrefix) {
		return o.Next.Verify(ctx, secret)
	}
	origin := SiteFrom(ctx).Origin
	if origin == "" {
		return Credential{}, fmt.Errorf("%w: access token with no known audience", ErrInvalidCredential)
	}
	p, err := o.Tokens.Verify(ctx, secret, oauth.ResourceURL(origin))
	if errors.Is(err, oauth.ErrInvalidToken) {
		return Credential{}, fmt.Errorf("%w: %w", ErrInvalidCredential, err)
	}
	if err != nil {
		return Credential{}, err
	}
	return Credential{Kind: KindOAuth, ID: p.GrantID, Ref: p.ClientID, CreatedAt: p.GrantCreatedAt, Email: p.Email}, nil
}

// Audit records the writes credentials make (ADR-00031 obligation 5).
type Audit interface {
	RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error
}

// KeyAudit records the writes of API keys in the key store's write log. A
// credential of another kind needs its own Audit.
type KeyAudit struct {
	Keys interface {
		RecordWrite(ctx context.Context, p apikey.Principal, householdID int64, tool, target string) error
	}
}

// RecordWrite implements Audit.
func (a KeyAudit) RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error {
	if c.Kind != KindAPIKey {
		return fmt.Errorf("record write: no write log for %s credentials", c.Kind)
	}
	p := apikey.Principal{KeyID: c.ID, Prefix: c.Ref, CreatedAt: c.CreatedAt, Email: c.Email, HouseholdID: c.HouseholdID, HouseholdName: c.HouseholdName}
	return a.Keys.RecordWrite(ctx, p, householdID, tool, target)
}

// GrantAudit records the writes of OAuth grants in the OAuth store's write log.
type GrantAudit struct {
	Grants interface {
		RecordWrite(ctx context.Context, p oauth.Principal, householdID int64, tool, target string) error
	}
}

// RecordWrite implements Audit.
func (a GrantAudit) RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error {
	if c.Kind != KindOAuth {
		return fmt.Errorf("record write: no grant log for %s credentials", c.Kind)
	}
	p := oauth.Principal{GrantID: c.ID, Email: c.Email, ClientID: c.Ref, GrantCreatedAt: c.CreatedAt}
	return a.Grants.RecordWrite(ctx, p, householdID, tool, target)
}

// AuditByKind sends each write to the log of the credential's kind. A kind with
// no log is an error, never a dropped record.
type AuditByKind map[string]Audit

// RecordWrite implements Audit.
func (a AuditByKind) RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error {
	l, ok := a[c.Kind]
	if !ok {
		return fmt.Errorf("record write: no write log for %s credentials", c.Kind)
	}
	return l.RecordWrite(ctx, c, householdID, tool, target)
}
