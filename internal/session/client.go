package session

import (
	"net/http"
	"strings"

	"github.com/kaecyra/gobbler/internal/ratelimit"
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

// ClientOf reads the client of a request. The address is the client address
// of ratelimit.ClientAddr (ADR-00036): the connection's peer, or the
// CF-Connecting-IP header when the peer is a configured trusted proxy and the
// request went through ratelimit.ResolveClient. It is for display only.
func ClientOf(r *http.Request) Client {
	ua := r.UserAgent()
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	ip := ""
	if a := ratelimit.ClientAddr(r); a.IsValid() {
		ip = a.String()
	}
	return Client{UserAgent: strings.ToValidUTF8(ua, ""), IP: ip}
}
