package oauth

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// maxRedirectURILen bounds one redirect URI.
const maxRedirectURILen = 2048

// deniedSchemes can run script or reach local files when a browser follows a
// redirect to them, so no client may register one.
var deniedSchemes = map[string]bool{
	"javascript": true, "data": true, "file": true, "vbscript": true, "about": true,
	"blob": true, "ftp": true, "ws": true, "wss": true, "chrome": true, "view-source": true,
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateRedirectURI refuses a redirect URI that a client may not register
// (OAuth 2.1 section 2.3.1, RFC 8252). It accepts an absolute https URL, an
// http URL on a loopback host, and the reverse-domain private-use scheme of a
// native app. It
// refuses a fragment, user information, plain http to any other host and the
// schemes that run script.
func ValidateRedirectURI(raw string) error {
	if raw == "" || len(raw) > maxRedirectURILen || strings.ContainsAny(raw, "\\ \t\r\n") {
		return refuse(CodeInvalidRedirectURI, "a redirect URI must be an absolute URI of at most 2048 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Opaque != "" {
		return refuse(CodeInvalidRedirectURI, "a redirect URI must be an absolute URI")
	}
	if strings.Contains(raw, "#") {
		return refuse(CodeInvalidRedirectURI, "a redirect URI must not have a fragment")
	}
	if u.User != nil {
		return refuse(CodeInvalidRedirectURI, "a redirect URI must not carry user information")
	}
	switch {
	case u.Scheme == "https":
		if u.Hostname() == "" {
			return refuse(CodeInvalidRedirectURI, "an https redirect URI needs a host")
		}
	case u.Scheme == "http":
		if !isLoopbackHost(u.Hostname()) {
			return refuse(CodeInvalidRedirectURI, "an http redirect URI is allowed only on a loopback address; use https")
		}
	case deniedSchemes[u.Scheme]:
		return refuse(CodeInvalidRedirectURI, fmt.Sprintf("the %s scheme is not allowed in a redirect URI", u.Scheme))
	default:
		// RFC 8252 section 7.1: a private-use scheme is a reverse domain name,
		// which keeps it from being one of the schemes other apps and the
		// system already handle (intent, tel, ms-settings).
		if !strings.Contains(u.Scheme, ".") {
			return refuse(CodeInvalidRedirectURI, "a private-use scheme must be a reverse domain name, such as com.example.app")
		}
		if u.Host == "" && u.Path == "" {
			return refuse(CodeInvalidRedirectURI, "a redirect URI with a private-use scheme needs a host or a path")
		}
	}
	return nil
}

// MatchRedirectURI reports whether requested is one of the registered URIs.
// The match is exact, except that a registered loopback http URI also matches
// the same URI on any port (RFC 8252 section 7.3), since a native app picks its
// port when it starts.
func MatchRedirectURI(registered []string, requested string) bool {
	for _, r := range registered {
		if r == requested {
			return true
		}
	}
	req, err := url.Parse(requested)
	if err != nil || req.Scheme != "http" || !isLoopbackHost(req.Hostname()) {
		return false
	}
	for _, r := range registered {
		reg, err := url.Parse(r)
		if err != nil || reg.Scheme != "http" || !isLoopbackHost(reg.Hostname()) {
			continue
		}
		if reg.Hostname() == req.Hostname() && reg.EscapedPath() == req.EscapedPath() && reg.RawQuery == req.RawQuery {
			return true
		}
	}
	return false
}

// ReturnHost is where a redirect URI sends the browser, as the consent page and
// the grants list name it: the host and port of an http or https URI, and the
// scheme with its host for a private-use one.
func ReturnHost(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		return u.Host
	}
	if u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return u.Scheme + ":" + u.Path
}
