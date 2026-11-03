package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/oauth/oauthdb"
)

const (
	// ResourcePath is where the MCP endpoint lives. Every token is bound to the
	// resource URL it makes (ResourceURL), as its audience.
	ResourcePath = "/mcp"

	// AccessTokenPrefix starts every access token, so /mcp can tell one from an
	// API key (ADR-00031) and a leaked one is recognisable.
	AccessTokenPrefix = "goa_"
	refreshPrefix     = "gor_"
	codePrefix        = "goc_"
	clientIDPrefix    = "gobc_"

	// secretBytes is the size of a secret before encoding: 256 bits.
	secretBytes = 32

	// CodeLifetime is how long an authorization code can be exchanged.
	CodeLifetime = 5 * time.Minute
	// AccessLifetime is how long an access token lasts.
	AccessLifetime = time.Hour
	// RefreshLifetime is how long a refresh token lasts. Each rotation issues a
	// new one, so a client in use never meets it.
	RefreshLifetime = 30 * 24 * time.Hour

	// timeLayout is fixed width, so text comparison orders timestamps.
	timeLayout = "2006-01-02T15:04:05.000000000Z"
)

var encoding = base64.RawURLEncoding

// ResourceURL is the audience of every token: the MCP endpoint under the
// public base URL (no trailing slash).
func ResourceURL(baseURL string) string { return strings.TrimRight(baseURL, "/") + ResourcePath }

// Store holds codes, grants and tokens. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	q    *oauthdb.Queries
	now  func() time.Time
	rand io.Reader
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store {
	return &Store{db: d, q: oauthdb.New(d), now: time.Now, rand: rand.Reader}
}

// hash is the only form of a secret the database sees.
func hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

func stamp(t time.Time) string { return t.UTC().Format(timeLayout) }

// draw returns a new random secret that starts with prefix. It is never logged.
func (s *Store) draw(prefix string) (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return "", fmt.Errorf("draw oauth secret: %w", err)
	}
	return prefix + encoding.EncodeToString(raw), nil
}

// AuthRequest is an approved authorization request: who approved it, for which
// client and where the browser returns.
type AuthRequest struct {
	Owner         string
	ClientID      string
	ClientName    string
	RedirectURI   string
	CodeChallenge string
	Resource      string
}

// IssueCode records the approval and returns the authorization code, to be
// sent to the client's redirect URI and exchanged once. The code exists nowhere
// else and must never be logged.
func (s *Store) IssueCode(ctx context.Context, r AuthRequest) (string, error) {
	owner := strings.ToLower(strings.TrimSpace(r.Owner))
	switch {
	case owner == "":
		return "", errors.New("issue oauth code: no owner")
	case r.ClientID == "" || r.RedirectURI == "" || r.Resource == "":
		return "", errors.New("issue oauth code: client, redirect URI and resource are required")
	case !ValidChallenge(r.CodeChallenge):
		return "", refuse(CodeInvalidRequest, "code_challenge must be an S256 challenge")
	}
	code, err := s.draw(codePrefix)
	if err != nil {
		return "", err
	}
	now := s.now()
	err = db.WithTx(ctx, s.db, s.q, func(q *oauthdb.Queries, _ *sql.Tx) error {
		if err := q.PruneCodes(ctx, stamp(now)); err != nil {
			return fmt.Errorf("prune codes: %w", err)
		}
		return q.InsertCode(ctx, oauthdb.InsertCodeParams{
			CodeHash: hash(code), OwnerEmail: owner, ClientID: r.ClientID, ClientName: cleanName(r.ClientName),
			RedirectUri: r.RedirectURI, CodeChallenge: r.CodeChallenge, Resource: r.Resource,
			CreatedAt: stamp(now), ExpiresAt: stamp(now.Add(CodeLifetime)),
		})
	})
	if err != nil {
		return "", fmt.Errorf("issue oauth code: %w", err)
	}
	return code, nil
}

// Tokens is what the token endpoint returns.
type Tokens struct {
	Access  string
	Refresh string
	// ExpiresIn is how long the access token lasts.
	ExpiresIn time.Duration
}

// ExchangeRequest is a token request that trades a code.
type ExchangeRequest struct {
	Code        string
	ClientID    string
	RedirectURI string
	Verifier    string
	Resource    string
}

// ExchangeCode trades an authorization code for a new grant and its tokens. It
// refuses, with an *Error, a code that is unknown, expired, issued to another
// client or redirect URI, or whose PKCE verifier or resource does not match. A
// code that was already exchanged revokes the grant it made. Any refusal after
// the code is found uses the code up.
func (s *Store) ExchangeCode(ctx context.Context, r ExchangeRequest) (Tokens, error) {
	switch {
	case r.Code == "" || r.ClientID == "" || r.RedirectURI == "":
		return Tokens{}, refuse(CodeInvalidRequest, "code, client_id and redirect_uri are required")
	case r.Verifier == "":
		return Tokens{}, refuse(CodeInvalidRequest, "code_verifier is required")
	case r.Resource == "":
		return Tokens{}, refuse(CodeInvalidRequest, "resource is required")
	}
	var toks Tokens
	var refusal error
	now := s.now()
	err := db.WithTx(ctx, s.db, s.q, func(q *oauthdb.Queries, _ *sql.Tx) error {
		h := hash(r.Code)
		c, err := q.GetCode(ctx, h)
		if errors.Is(err, sql.ErrNoRows) {
			refusal = refuse(CodeInvalidGrant, "the code is unknown, expired or already used")
			return nil
		}
		if err != nil {
			return fmt.Errorf("get code: %w", err)
		}
		if c.GrantID.Valid {
			// A code used twice: whoever holds the first grant may be the
			// thief, so the grant goes (OAuth 2.1 section 4.1.3).
			if _, err := q.DeleteGrant(ctx, c.GrantID.Int64); err != nil {
				return fmt.Errorf("revoke grant %d of a reused code: %w", c.GrantID.Int64, err)
			}
			refusal = refuse(CodeInvalidGrant, "the code is unknown, expired or already used")
			return nil
		}
		// One try only: from here the code is spent whatever happens.
		spend := func(e error) error {
			refusal = e
			return q.DeleteCode(ctx, h)
		}
		expires, err := time.Parse(timeLayout, c.ExpiresAt)
		if err != nil {
			return fmt.Errorf("parse expiry of a code: %w", err)
		}
		switch {
		case !expires.After(now):
			return spend(refuse(CodeInvalidGrant, "the code is unknown, expired or already used"))
		case c.ClientID != r.ClientID:
			return spend(refuse(CodeInvalidGrant, "the code was issued to another client"))
		case c.RedirectUri != r.RedirectURI:
			return spend(refuse(CodeInvalidGrant, "redirect_uri does not match the authorization request"))
		case !VerifyPKCE(c.CodeChallenge, r.Verifier):
			return spend(refuse(CodeInvalidGrant, "code_verifier does not match the code_challenge"))
		case c.Resource != r.Resource:
			return spend(refuse(CodeInvalidTarget, "resource does not match the authorization request"))
		}
		gid, err := q.InsertGrant(ctx, oauthdb.InsertGrantParams{
			OwnerEmail: c.OwnerEmail, ClientID: c.ClientID, ClientName: c.ClientName,
			ReturnHost: ReturnHost(c.RedirectUri), Resource: c.Resource, CreatedAt: stamp(now),
		})
		if err != nil {
			return fmt.Errorf("insert grant: %w", err)
		}
		if err := q.MarkCodeUsed(ctx, oauthdb.MarkCodeUsedParams{GrantID: sql.NullInt64{Int64: gid, Valid: true}, CodeHash: h}); err != nil {
			return fmt.Errorf("mark code used: %w", err)
		}
		toks, err = s.issue(ctx, q, gid, now)
		return err
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("exchange oauth code: %w", err)
	}
	if refusal != nil {
		return Tokens{}, refusal
	}
	return toks, nil
}

// issue adds a new access token and refresh token to the grant.
func (s *Store) issue(ctx context.Context, q *oauthdb.Queries, grantID int64, now time.Time) (Tokens, error) {
	access, err := s.draw(AccessTokenPrefix)
	if err != nil {
		return Tokens{}, err
	}
	refresh, err := s.draw(refreshPrefix)
	if err != nil {
		return Tokens{}, err
	}
	for _, t := range []struct {
		kind, secret string
		life         time.Duration
	}{{"access", access, AccessLifetime}, {"refresh", refresh, RefreshLifetime}} {
		err := q.InsertToken(ctx, oauthdb.InsertTokenParams{
			GrantID: grantID, Kind: t.kind, TokenHash: hash(t.secret), ExpiresAt: stamp(now.Add(t.life)),
		})
		if err != nil {
			return Tokens{}, fmt.Errorf("insert %s token: %w", t.kind, err)
		}
	}
	return Tokens{Access: access, Refresh: refresh, ExpiresIn: AccessLifetime}, nil
}

// RefreshRequest is a token request that trades a refresh token.
type RefreshRequest struct {
	Token    string
	ClientID string
	// Resource is optional. When it is given it must be the grant's.
	Resource string
}

// Refresh trades a refresh token for a new access token and a new refresh
// token, and retires the one presented. A token that was already traded revokes
// the whole grant, since two parties hold it (ADR-00034).
func (s *Store) Refresh(ctx context.Context, r RefreshRequest) (Tokens, error) {
	if r.Token == "" || r.ClientID == "" {
		return Tokens{}, refuse(CodeInvalidRequest, "refresh_token and client_id are required")
	}
	var toks Tokens
	var refusal error
	now := s.now()
	err := db.WithTx(ctx, s.db, s.q, func(q *oauthdb.Queries, _ *sql.Tx) error {
		t, err := q.GetRefreshToken(ctx, hash(r.Token))
		if errors.Is(err, sql.ErrNoRows) {
			refusal = refuse(CodeInvalidGrant, "the refresh token is unknown, expired or revoked")
			return nil
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if t.UsedAt.Valid {
			if _, err := q.DeleteGrant(ctx, t.GrantID); err != nil {
				return fmt.Errorf("revoke grant %d of a reused refresh token: %w", t.GrantID, err)
			}
			refusal = refuse(CodeInvalidGrant, "the refresh token is unknown, expired or revoked")
			return nil
		}
		expires, err := time.Parse(timeLayout, t.ExpiresAt)
		if err != nil {
			return fmt.Errorf("parse expiry of a refresh token: %w", err)
		}
		switch {
		case !expires.After(now):
			refusal = refuse(CodeInvalidGrant, "the refresh token is unknown, expired or revoked")
			return nil
		case t.ClientID != r.ClientID:
			refusal = refuse(CodeInvalidGrant, "the refresh token was issued to another client")
			return nil
		case r.Resource != "" && r.Resource != t.Resource:
			refusal = refuse(CodeInvalidTarget, "resource does not match the grant")
			return nil
		}
		if err := q.MarkTokenUsed(ctx, oauthdb.MarkTokenUsedParams{UsedAt: sql.NullString{String: stamp(now), Valid: true}, ID: t.ID}); err != nil {
			return fmt.Errorf("retire refresh token: %w", err)
		}
		if err := q.PruneTokens(ctx, oauthdb.PruneTokensParams{GrantID: t.GrantID, Now: stamp(now)}); err != nil {
			return fmt.Errorf("prune tokens: %w", err)
		}
		toks, err = s.issue(ctx, q, t.GrantID, now)
		return err
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("refresh oauth token: %w", err)
	}
	if refusal != nil {
		return Tokens{}, refusal
	}
	return toks, nil
}

// Principal is who an access token acts as.
type Principal struct {
	GrantID  int64
	Email    string
	ClientID string
	// GrantCreatedAt, with the client and owner, tells this grant from a later
	// one that was given its id; the write log is read by all of them (Writes).
	GrantCreatedAt time.Time
}

// Verify checks an access token for the resource and returns who it acts as.
// This is the verifier /mcp uses. resource is the audience /mcp expects
// (ResourceURL), so a token bound to another server is refused. Every refusal
// is ErrInvalidToken, wrapped with a reason that is safe to log. Admission to a
// household stays with the caller (ADR-00018, ADR-00034).
func (s *Store) Verify(ctx context.Context, token, resource string) (Principal, error) {
	if !strings.HasPrefix(token, AccessTokenPrefix) {
		return Principal{}, fmt.Errorf("%w: not an access token", ErrInvalidToken)
	}
	row, err := s.q.GetAccessToken(ctx, hash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, fmt.Errorf("%w: unknown or revoked", ErrInvalidToken)
	}
	if err != nil {
		return Principal{}, fmt.Errorf("verify access token: %w", err)
	}
	expires, err := time.Parse(timeLayout, row.ExpiresAt)
	if err != nil {
		return Principal{}, fmt.Errorf("parse expiry of an access token: %w", err)
	}
	switch {
	case !expires.After(s.now()):
		return Principal{}, fmt.Errorf("%w: expired", ErrInvalidToken)
	case resource == "" || row.Resource != resource:
		return Principal{}, fmt.Errorf("%w: issued for another audience", ErrInvalidToken)
	}
	created, err := time.Parse(timeLayout, row.GrantCreatedAt)
	if err != nil {
		return Principal{}, fmt.Errorf("parse creation time of oauth grant %d: %w", row.GrantID, err)
	}
	return Principal{GrantID: row.GrantID, Email: row.OwnerEmail, ClientID: row.ClientID, GrantCreatedAt: created}, nil
}

// Grant is a person's approval of a client, as the profile lists it.
type Grant struct {
	ID         int64
	ClientID   string
	ClientName string
	// ReturnHost is where the client's redirect URI sends the browser.
	ReturnHost string
	CreatedAt  time.Time
}

// ListGrants returns the person's grants, oldest first.
func (s *Store) ListGrants(ctx context.Context, email string) ([]Grant, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	rows, err := s.q.ListGrants(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("list oauth grants of %s: %w", e, err)
	}
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		created, err := time.Parse(timeLayout, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse created time of oauth grant %d: %w", r.ID, err)
		}
		out = append(out, Grant{ID: r.ID, ClientID: r.ClientID, ClientName: r.ClientName, ReturnHost: r.ReturnHost, CreatedAt: created})
	}
	return out, nil
}

// CreatedText is the date the grant was approved.
func (g Grant) CreatedText() string { return g.CreatedAt.UTC().Format("2006-01-02") }

// RevokeGrant deletes the person's own grant, and with it every token it made.
// Someone else's grant and one that does not exist are both ErrNotFound.
func (s *Store) RevokeGrant(ctx context.Context, email string, id int64) error {
	e := strings.ToLower(strings.TrimSpace(email))
	var n int64
	err := db.WithTx(ctx, s.db, s.q, func(q *oauthdb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.RevokeGrant(ctx, oauthdb.RevokeGrantParams{ID: id, OwnerEmail: e})
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke oauth grant %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: grant %d", ErrNotFound, id)
	}
	return nil
}

// RevokeForMember is the hook that makes removing a member, or their leaving,
// revoke their grants at once (ADR-00034), beside their keys (ADR-00031). The
// router registers it with household.Store.OnMemberLeave, so it runs inside the
// transaction that removes the member and a failure here rolls the removal
// back. Codes the person approved but the client has not exchanged go too.
func (s *Store) RevokeForMember(ctx context.Context, tx *sql.Tx, email string) error {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return errors.New("revoke oauth grants: empty email")
	}
	q := s.q.WithTx(tx)
	if _, err := q.RevokeMemberGrants(ctx, e); err != nil {
		return fmt.Errorf("revoke oauth grants of %s: %w", e, err)
	}
	if _, err := q.RevokeMemberCodes(ctx, e); err != nil {
		return fmt.Errorf("revoke oauth codes of %s: %w", e, err)
	}
	return nil
}
