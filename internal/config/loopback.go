package config

import (
	"net"
	"net/netip"
	"strings"
)

// IsLoopbackAddr reports whether a host:port listen address binds only to the
// loopback interface: localhost, a 127.0.0.0/8 address or ::1. A wildcard
// (":8080", "0.0.0.0:8080"), a public IP, any other hostname or a malformed
// address is not loopback. ADR-00003 uses it to gate the dev-auth bypass.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return ip.Unmap().IsLoopback()
}
