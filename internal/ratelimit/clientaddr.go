package ratelimit

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// forwardedHeader is the header cloudflared sets to the caller's address.
const forwardedHeader = "CF-Connecting-IP"

type addrKey struct{}

// PeerAddr is the address of the connection the request arrived on, or the
// zero Addr when it cannot be read.
func PeerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap().WithZone("")
}

// Resolve is the one client-address rule (ADR-00036): the request's peer
// address, unless the peer is inside one of trusted, in which case it is the
// CF-Connecting-IP header when that holds an IP address. With no trusted
// proxies the header is never read, because anything that reaches the port
// directly can send it with any value (ADR-00027). The zero Addr means no
// address could be read.
func Resolve(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := PeerAddr(r)
	if !peer.IsValid() {
		return peer
	}
	for _, p := range trusted {
		if p.Contains(peer) {
			if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(forwardedHeader))); err == nil {
				return a.Unmap().WithZone("")
			}
			break
		}
	}
	return peer
}

// ResolveClient is middleware that applies Resolve once and stores the result
// on the request, where ClientAddr reads it. Mount it before anything that
// asks for the client address.
func ResolveClient(trusted []netip.Prefix) func(http.Handler) http.Handler {
	trusted = append([]netip.Prefix(nil), trusted...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), addrKey{}, Resolve(r, trusted))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClientAddr is the client address ResolveClient stored on the request. Without
// that middleware it is the peer address, never the header.
func ClientAddr(r *http.Request) netip.Addr {
	if a, ok := r.Context().Value(addrKey{}).(netip.Addr); ok {
		return a
	}
	return PeerAddr(r)
}

// ClientKey is ClientAddr as a limiter key. A request with no readable address
// shares one key, so it is limited rather than exempt.
func ClientKey(r *http.Request) (string, bool) {
	a := ClientAddr(r)
	if !a.IsValid() {
		return "unknown", true
	}
	return a.String(), true
}
