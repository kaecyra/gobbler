package oauth

import (
	"crypto/tls"
	"net/netip"
	"time"
)

// SetClock replaces the store's clock, for a test that moves time.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// NewMetadataFetcherFor is a fetcher with no dial guard and the given TLS
// settings, limits and timeout, so a test can reach a server on loopback.
func NewMetadataFetcherFor(timeout time.Duration, limit int64, tlsConfig *tls.Config) *MetadataFetcher {
	return newMetadataFetcher(timeout, limit, nil, tlsConfig)
}

// Blocked reports blockedAddr for an address given as text.
func Blocked(s string) bool { return blockedAddr(netip.MustParseAddr(s)) }
