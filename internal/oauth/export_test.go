package oauth

import (
	"crypto/tls"
	"syscall"
	"time"
)

// SetClock replaces the store's clock, for a test that moves time.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// NewMetadataFetcherFor is a fetcher with no dial guard and the given TLS
// settings, limits and timeout, so a test can reach a server on loopback.
func NewMetadataFetcherFor(timeout time.Duration, limit int64, tlsConfig *tls.Config) *MetadataFetcher {
	allow := func(string, string, syscall.RawConn) error { return nil }
	return newMetadataFetcher(timeout, limit, allow, tlsConfig)
}
