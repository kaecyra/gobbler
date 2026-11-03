// Package oauth serves gobbler's OAuth 2.1 authorization server over HTTP
// (ADR-00034); the rules and storage are in internal/oauth.
//
// Public, served without a session (ADR-00029, ADR-00034): the discovery
// documents, client registration and the token endpoint. The authorization
// endpoint needs a session: a person who is not signed in is sent to sign-in
// and returns to it, and the consent page names the client and the host the
// browser returns to.
//
// Browser clients are out of scope: the token and registration endpoints are
// POSTs without CORS headers, and the server's cross-origin protection
// (ADR-00016) refuses a browser's cross-origin request to them.
package oauth
