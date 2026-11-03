package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/oauth/oauthdb"
)

const (
	// MaxClientNameRunes is the longest client name kept.
	MaxClientNameRunes = 100
	// MaxRedirectURIs is the most redirect URIs one client may register.
	MaxRedirectURIs = 10
	// MaxClients is how many self-registered clients are kept. The registration
	// endpoint is public, so the table needs a ceiling.
	MaxClients = 1000
	// ClientRetention is how long a self-registered client that never got a
	// grant is kept.
	ClientRetention = 24 * time.Hour

	authMethodNone = "none"
)

// ErrUnknownClient is returned (wrapped) when a client id names no registered
// client.
var ErrUnknownClient = errors.New("unknown client")

// Client is an OAuth client as the authorization endpoint needs it.
type Client struct {
	ID           string
	Name         string
	RedirectURIs []string
	// CreatedAt is when a registered client registered; it is zero for a
	// metadata-document client, which is not stored.
	CreatedAt time.Time
}

// AllowsRedirect reports whether uri is one of the client's redirect URIs
// (MatchRedirectURI).
func (c Client) AllowsRedirect(uri string) bool { return MatchRedirectURI(c.RedirectURIs, uri) }

// ClientMetadata is what a client says about itself, in a registration request
// (RFC 7591) or a Client ID Metadata Document. Only public clients are served,
// so a method that needs a secret is refused.
type ClientMetadata struct {
	Name          string
	RedirectURIs  []string
	AuthMethod    string
	GrantTypes    []string
	ResponseTypes []string
}

// Normalize checks the metadata and returns it with a clean name. Its error is
// an *Error with CodeInvalidClientMetadata or CodeInvalidRedirectURI, written
// for the client.
func (m ClientMetadata) Normalize() (ClientMetadata, error) {
	m.Name = cleanName(m.Name)
	if m.Name == "" {
		m.Name = "An unnamed application"
	}
	if len(m.RedirectURIs) == 0 || len(m.RedirectURIs) > MaxRedirectURIs {
		return ClientMetadata{}, refuse(CodeInvalidRedirectURI, fmt.Sprintf("give between 1 and %d redirect URIs", MaxRedirectURIs))
	}
	for _, u := range m.RedirectURIs {
		if err := ValidateRedirectURI(u); err != nil {
			return ClientMetadata{}, err
		}
	}
	if m.AuthMethod != "" && m.AuthMethod != authMethodNone {
		return ClientMetadata{}, refuse(CodeInvalidClientMetadata, `token_endpoint_auth_method must be "none": gobbler serves public clients that use PKCE`)
	}
	for _, g := range m.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return ClientMetadata{}, refuse(CodeInvalidClientMetadata, "grant_types may hold only authorization_code and refresh_token")
		}
	}
	for _, r := range m.ResponseTypes {
		if r != "code" {
			return ClientMetadata{}, refuse(CodeInvalidClientMetadata, `response_types may hold only "code"`)
		}
	}
	m.AuthMethod = authMethodNone
	return m, nil
}

// cleanName trims a client's name, drops control characters and cuts it to
// MaxClientNameRunes. The name is the client's own word, shown on the consent
// page, so it is only ever rendered escaped.
func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxClientNameRunes {
		name = string([]rune(name)[:MaxClientNameRunes])
	}
	return name
}

// RegisterClient stores a client that registered itself (RFC 7591) and returns
// it with its new id. It refuses a registration past MaxClients once clients
// that never got a grant are pruned.
func (s *Store) RegisterClient(ctx context.Context, m ClientMetadata) (Client, error) {
	m, err := m.Normalize()
	if err != nil {
		return Client{}, err
	}
	id, err := s.draw(clientIDPrefix)
	if err != nil {
		return Client{}, err
	}
	uris, err := json.Marshal(m.RedirectURIs)
	if err != nil {
		return Client{}, fmt.Errorf("encode redirect URIs: %w", err)
	}
	now := s.now()
	full := false
	err = db.WithTx(ctx, s.db, s.q, func(q *oauthdb.Queries, _ *sql.Tx) error {
		if _, err := q.PruneClients(ctx, stamp(now.Add(-ClientRetention))); err != nil {
			return fmt.Errorf("prune clients: %w", err)
		}
		n, err := q.CountClients(ctx)
		if err != nil {
			return fmt.Errorf("count clients: %w", err)
		}
		if n >= MaxClients {
			full = true
			return nil
		}
		return q.InsertClient(ctx, oauthdb.InsertClientParams{ClientID: id, ClientName: m.Name, RedirectUris: string(uris), CreatedAt: stamp(now)})
	})
	if err != nil {
		return Client{}, fmt.Errorf("register client: %w", err)
	}
	if full {
		return Client{}, refuse(CodeTemporarilyBusy, "gobbler is not taking new client registrations right now")
	}
	// The stamp is stored at nanosecond precision, so read it back the same way.
	created, err := time.Parse(timeLayout, stamp(now))
	if err != nil {
		return Client{}, fmt.Errorf("parse registration time: %w", err)
	}
	return Client{ID: id, Name: m.Name, RedirectURIs: slices.Clone(m.RedirectURIs), CreatedAt: created}, nil
}

// Client returns the registered client with the id, or ErrUnknownClient.
func (s *Store) Client(ctx context.Context, id string) (Client, error) {
	row, err := s.q.GetClient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, fmt.Errorf("%w: %q", ErrUnknownClient, truncate(id, 80))
	}
	if err != nil {
		return Client{}, fmt.Errorf("get client: %w", err)
	}
	c := Client{ID: row.ClientID, Name: row.ClientName}
	if c.CreatedAt, err = time.Parse(timeLayout, row.CreatedAt); err != nil {
		return Client{}, fmt.Errorf("parse registration time of client %s: %w", row.ClientID, err)
	}
	if err := json.Unmarshal([]byte(row.RedirectUris), &c.RedirectURIs); err != nil {
		return Client{}, fmt.Errorf("decode redirect URIs of client %s: %w", row.ClientID, err)
	}
	return c, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
