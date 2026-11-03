// Package oauth is gobbler's OAuth 2.1 authorization server for MCP clients
// (ADR-00034). It holds the rules and the storage; package web/oauth serves
// them over HTTP. The SQL is in queries/, generated into oauthdb (ADR-00014).
//
// A client is public: it proves itself with PKCE, not a secret, so gobbler
// stores and logs no client secret. It identifies itself by registering
// (RegisterClient, Dynamic Client Registration) or by naming a Client ID
// Metadata Document that MetadataFetcher loads.
//
// A person approves a client, which creates a grant: the client acting as that
// person, with the reach of a personal key. The grant's tokens are bound to one
// resource (the /mcp endpoint). Access tokens are short-lived. A refresh token
// is rotated on every use, and presenting a rotated one revokes the grant.
// Codes and tokens are random secrets of which the database keeps only the
// SHA-256, and no log line carries one.
//
// Verify is the token verifier that /mcp uses to admit a bearer token.
package oauth
