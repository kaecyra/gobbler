// Package netguard is the one place gobbler decides which addresses it will
// connect to when a URL it did not choose is fetched: the OAuth client metadata
// document, a recipe page and a recipe picture (ADR-00039, "Fetching").
//
// The rule is applied when the connection is dialled, after name resolution,
// so a name that resolves to a private address, or changes its answer between
// a check and the connection, cannot get through. Proxy environment variables
// are ignored so the guard sees the address actually dialled.
package netguard

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// MaxRedirects is how many redirects a client built for page and image
// fetches follows.
const MaxRedirects = 5

var (
	// ErrBlocked is returned (wrapped) when a connection is refused because the
	// address is not a public unicast one.
	ErrBlocked = errors.New("blocked address")
	// ErrRedirect is returned (wrapped) when a redirect is refused: too many
	// hops, or a hop that is not http or https.
	ErrRedirect = errors.New("refused redirect")
)

// blockedPrefixes are the ranges that are not private, loopback or link-local
// as net/netip counts them but still never name a public host: carrier-grade
// NAT, benchmarking, documentation, reserved and IPv6 transition ranges.
var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001:db8::/32", "fec0::/10", "2002::/16",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// Blocked reports whether gobbler must not connect to a: anything that is not a
// public unicast address. An IPv4 address inside an IPv6 one is judged as the
// IPv4 address.
func Blocked(a netip.Addr) bool { return blocked(a, false) }

// blocked is Blocked with an exception: loopback (127.0.0.0/8 and ::1) is
// admitted when allowLoopback is set. Every other range stays refused.
func blocked(a netip.Addr, allowLoopback bool) bool {
	a = a.Unmap().WithZone("")
	if allowLoopback && a.IsLoopback() {
		return false
	}
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Control is a net.Dialer Control hook. It runs for every connection, on the
// address about to be connected to, and refuses one that Blocked names.
func Control(network, address string, c syscall.RawConn) error {
	return control(network, address, c, false)
}

// controlAllowingLoopback is Control except that it admits loopback addresses.
func controlAllowingLoopback(network, address string, c syscall.RawConn) error {
	return control(network, address, c, true)
}

func control(_, address string, _ syscall.RawConn, allowLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: bad address %q", ErrBlocked, address)
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: connecting to a name that was not resolved", ErrBlocked)
	}
	if blocked(a, allowLoopback) {
		return fmt.Errorf("%w: the host resolves to an address gobbler does not connect to (%s)", ErrBlocked, a.Unmap())
	}
	return nil
}

// Options shape a guarded client. The zero value is a client with no timeout
// that follows no redirect.
type Options struct {
	// Timeout bounds one whole request, connecting, waiting and reading. Zero
	// means no client-side bound; the caller's context is then the only one.
	Timeout time.Duration
	// Redirects is how many redirects to follow. Each hop must be http or https
	// and passes the same address guard. Zero follows none: the redirect
	// response is returned as it is.
	Redirects int
	// MaxHeaderBytes bounds a response's headers. Zero means the net/http default.
	MaxHeaderBytes int64
	// TLSConfig is the TLS configuration; nil means the system roots.
	TLSConfig *tls.Config
	// AllowLoopback admits loopback addresses (127.0.0.0/8, ::1) and nothing
	// else the guard refuses. It is for a development server running with the
	// dev auth bypass (ADR-00040); the caller decides, this package reads no
	// environment. Dial, when set, takes precedence.
	AllowLoopback bool
	// Dial replaces Control as the dial guard. It exists so a test can reach a
	// server on loopback; production code leaves it nil.
	Dial func(network, address string, c syscall.RawConn) error
}

// NewClient returns an *http.Client that connects only to public addresses,
// ignores proxy environment variables and follows at most o.Redirects
// redirects.
func NewClient(o Options) *http.Client {
	control := o.Dial
	if control == nil {
		control = Control
		if o.AllowLoopback {
			control = controlAllowingLoopback
		}
	}
	d := &net.Dialer{Timeout: o.Timeout, Control: control}
	tr := &http.Transport{
		// No proxy: the guard must see the address of the host itself.
		Proxy:                  nil,
		DialContext:            d.DialContext,
		TLSClientConfig:        o.TLSConfig,
		TLSHandshakeTimeout:    o.Timeout,
		ResponseHeaderTimeout:  o.Timeout,
		MaxResponseHeaderBytes: o.MaxHeaderBytes,
		DisableKeepAlives:      true,
	}
	return &http.Client{Transport: tr, Timeout: o.Timeout, CheckRedirect: checkRedirect(o.Redirects)}
}

// checkRedirect follows up to limit redirects, each to an http or https URL.
func checkRedirect(limit int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if limit <= 0 {
			return http.ErrUseLastResponse
		}
		if len(via) > limit {
			return fmt.Errorf("%w: more than %d redirects", ErrRedirect, limit)
		}
		if s := req.URL.Scheme; s != "http" && s != "https" {
			return fmt.Errorf("%w: redirected to a %q URL, not http or https", ErrRedirect, s)
		}
		return nil
	}
}
