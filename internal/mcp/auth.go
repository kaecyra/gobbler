package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kaecyra/gobbler/internal/apikey"
)

// ErrInvalidCredential is returned (wrapped) by a Verifier for a credential it
// does not accept: unknown, expired or revoked. Any other error is a failure to
// check, and is not a refusal.
var ErrInvalidCredential = errors.New("invalid credential")

// KindAPIKey is the Kind of a credential that is an API key (ADR-00031).
const KindAPIKey = "api_key"

// Credential is who a verified bearer credential acts as. Exactly one of Email
// and HouseholdID is set: a personal credential acts as a person, whose
// household is decided by the membership check (ADR-00018), and a household
// credential acts for its household.
type Credential struct {
	// Kind says which kind of credential it is, and ID names it among that
	// kind's credentials. Neither is secret.
	Kind string
	ID   int64

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
// WWW-Authenticate: Bearer and never a redirect to sign-in (ADR-00034). A
// failure to check is a 500, so a broken database does not read as a revoked
// key. The credential is stored on the request context.
func Bearer(v Verifier, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			secret, ok := bearerSecret(r)
			if !ok {
				unauthorized(w)
				return
			}
			c, err := v.Verify(r.Context(), secret)
			switch {
			case errors.Is(err, ErrInvalidCredential):
				unauthorized(w)
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

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "a valid API key is required", http.StatusUnauthorized)
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
	return Credential{Kind: KindAPIKey, ID: p.KeyID, Email: p.Email, HouseholdID: p.HouseholdID, HouseholdName: p.HouseholdName}, nil
}

// Audit records the writes credentials make (ADR-00031 obligation 5).
type Audit interface {
	RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error
}

// KeyAudit records the writes of API keys in the key store's write log. A
// credential of another kind needs its own Audit.
type KeyAudit struct {
	Keys interface {
		RecordWrite(ctx context.Context, keyID, householdID int64, tool, target string) error
	}
}

// RecordWrite implements Audit.
func (a KeyAudit) RecordWrite(ctx context.Context, c Credential, householdID int64, tool, target string) error {
	if c.Kind != KindAPIKey {
		return fmt.Errorf("record write: no write log for %s credentials", c.Kind)
	}
	return a.Keys.RecordWrite(ctx, c.ID, householdID, tool, target)
}
