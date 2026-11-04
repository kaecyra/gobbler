// Package ratelimit holds the leaky-bucket limiter and the client-address rule
// shared by the MCP endpoint, the public OAuth endpoints and the session list
// (ADR-00034, ADR-00036).
//
// The client-address rule lives here, and not in its own package, because the
// limiter is its main consumer and this package imports nothing of the
// application, so session and the OAuth handlers can both import it without a
// cycle.
package ratelimit
