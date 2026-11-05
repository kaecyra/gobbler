package oauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/kaecyra/gobbler/internal/netguard"
)

const (
	// MetadataTimeout bounds one whole fetch of a client metadata document,
	// connecting, waiting and reading.
	MetadataTimeout = 5 * time.Second
	// MaxMetadataBytes bounds what a fetch reads. A real document is a few
	// hundred bytes.
	MaxMetadataBytes = 16 << 10
)

// ErrMetadata is returned (wrapped) when a client metadata document cannot be
// used: its URL is not acceptable, it cannot be fetched, or it is not valid.
// Its text is written for a log line; the consent flow shows the person only a
// fixed message.
var ErrMetadata = errors.New("client metadata document")

// IsMetadataURL reports whether a client_id is a Client ID Metadata Document
// URL (an https URL) rather than the id of a registered client.
func IsMetadataURL(clientID string) bool { return strings.HasPrefix(clientID, "https://") }

// ValidateMetadataURL refuses a client_id that may not be fetched: it must be
// an https URL with a host and a path, with no fragment, no user information
// and no dot segments (the Client ID Metadata Document draft). Whether the
// host is a place gobbler may reach is decided when connecting.
func ValidateMetadataURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Scheme != "https":
		return nil, fmt.Errorf("%w: the client_id must be an https URL", ErrMetadata)
	case u.Hostname() == "":
		return nil, fmt.Errorf("%w: the client_id has no host", ErrMetadata)
	case u.User != nil:
		return nil, fmt.Errorf("%w: the client_id must not carry user information", ErrMetadata)
	case strings.Contains(raw, "#"):
		return nil, fmt.Errorf("%w: the client_id must not have a fragment", ErrMetadata)
	case u.Path == "" || u.Path == "/":
		return nil, fmt.Errorf("%w: the client_id needs a path", ErrMetadata)
	case strings.ContainsAny(raw, " \t\r\n\\"):
		return nil, fmt.Errorf("%w: the client_id has characters a URL cannot hold", ErrMetadata)
	}
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		if seg == "." || seg == ".." || strings.EqualFold(seg, "%2e") || strings.EqualFold(seg, "%2e%2e") {
			return nil, fmt.Errorf("%w: the client_id must not have dot segments", ErrMetadata)
		}
	}
	return u, nil
}

// MetadataFetcher loads Client ID Metadata Documents. A document is named by
// an outsider, so a fetch is HTTPS only, never connects to a private, loopback
// or link-local address, never follows a redirect, and is bounded in size and
// time.
type MetadataFetcher struct {
	client *http.Client
	max    int64
}

// NewMetadataFetcher returns the fetcher the server uses.
func NewMetadataFetcher() *MetadataFetcher {
	return newMetadataFetcher(MetadataTimeout, MaxMetadataBytes, nil, nil)
}

// newMetadataFetcher builds a fetcher. dial replaces the address guard (nil
// keeps it) and tlsConfig is the TLS settings (nil for the system roots); tests
// replace them.
func newMetadataFetcher(timeout time.Duration, limit int64, dial func(network, address string, c syscall.RawConn) error, tlsConfig *tls.Config) *MetadataFetcher {
	return &MetadataFetcher{
		max: limit,
		// Zero redirects: a redirect could lead anywhere, and the document must
		// be served from the URL that names the client.
		client: netguard.NewClient(netguard.Options{
			Timeout: timeout, Redirects: 0, MaxHeaderBytes: 16 << 10, TLSConfig: tlsConfig, Dial: dial,
		}),
	}
}

// metadataDocument is the part of a document gobbler reads. Unknown fields are
// ignored, except the secret ones below.
type metadataDocument struct {
	ClientID      string   `json:"client_id"`
	ClientName    string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
	ClientSecret  *string  `json:"client_secret"`
	JWKS          any      `json:"jwks"`
	JWKSURI       *string  `json:"jwks_uri"`
}

// Fetch loads the document the client_id URL names and returns the client it
// describes. The document's own client_id must be the URL, exactly. Errors wrap
// ErrMetadata, or ErrInvalidClientMetadata and ErrInvalidRedirectURI for
// content gobbler refuses.
func (f *MetadataFetcher) Fetch(ctx context.Context, clientID string) (Client, error) {
	u, err := ValidateMetadataURL(clientID)
	if err != nil {
		return Client{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, f.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Client{}, fmt.Errorf("%w: build request: %w", ErrMetadata, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gobbler (client metadata fetch)")
	resp, err := f.client.Do(req)
	if err != nil {
		return Client{}, fmt.Errorf("%w: fetch %s: %w", ErrMetadata, u.Host, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Client{}, fmt.Errorf("%w: %s answered %d", ErrMetadata, u.Host, resp.StatusCode)
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
		return Client{}, fmt.Errorf("%w: %s did not answer with JSON", ErrMetadata, u.Host)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.max+1))
	if err != nil {
		return Client{}, fmt.Errorf("%w: read %s: %w", ErrMetadata, u.Host, err)
	}
	if int64(len(body)) > f.max {
		return Client{}, fmt.Errorf("%w: %s sent more than %d bytes", ErrMetadata, u.Host, f.max)
	}
	var doc metadataDocument
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil {
		return Client{}, fmt.Errorf("%w: %s sent invalid JSON: %w", ErrMetadata, u.Host, err)
	}
	if doc.ClientID != clientID {
		return Client{}, fmt.Errorf("%w: the document's client_id is not the URL it was fetched from", ErrMetadata)
	}
	if doc.ClientSecret != nil || doc.JWKS != nil || doc.JWKSURI != nil {
		return Client{}, fmt.Errorf("%w: %w", ErrMetadata, refuse(CodeInvalidClientMetadata, "a metadata document must not carry a client secret or keys"))
	}
	m, err := ClientMetadata{
		Name: doc.ClientName, RedirectURIs: doc.RedirectURIs, AuthMethod: doc.AuthMethod,
		GrantTypes: doc.GrantTypes, ResponseTypes: doc.ResponseTypes,
	}.Normalize()
	if err != nil {
		return Client{}, fmt.Errorf("%w: %w", ErrMetadata, err)
	}
	return Client{ID: clientID, Name: m.Name, RedirectURIs: m.RedirectURIs}, nil
}
