// Package credential names the kinds of bearer credential a proposal or write
// log records, so the packages that issue them and the packages that word them
// for display share one spelling.
package credential

const (
	// KindAPIKey is the Kind of a credential that is an API key (ADR-00031).
	KindAPIKey = "api_key"
	// KindOAuth is the Kind of a credential that is an OAuth access token
	// (ADR-00034). Its ID is the grant the token belongs to, not the token.
	KindOAuth = "oauth_grant"
)
