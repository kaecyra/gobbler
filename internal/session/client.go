package session

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// maxUserAgent bounds what is stored of a user agent: the header is the
// client's to fill, and a session row should not carry an unbounded one.
const maxUserAgent = 512

// Client is what a session records about the browser that started it
// (ADR-00032). It is shown to the person whose session it is and nothing
// else: neither field is ever used to admit or refuse a request.
type Client struct {
	UserAgent string
	// IP is the client address, or empty when none could be read.
	IP string
}

// ClientOf reads the client of a request. The address is CF-Connecting-IP
// when it holds an IP address, and the connection's remote address otherwise.
// Anything that reaches the port directly can set that header to any value
// (ADR-00027), which is why the result is for display only.
func ClientOf(r *http.Request) Client {
	ua := r.UserAgent()
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	return Client{UserAgent: strings.ToValidUTF8(ua, ""), IP: clientIP(r)}
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		if a, err := netip.ParseAddr(v); err == nil {
			return a.Unmap().String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap().String()
	}
	return ""
}
