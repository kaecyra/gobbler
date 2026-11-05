// Package mcp is gobbler's Model Context Protocol server (ADR-00034): Streamable
// HTTP at /mcp, built on the official Go SDK, with tools for recipes, recipe
// imports and shopping lists. There is no stdio server.
//
// A request reaches a tool in this order. The web layer skips the session for
// the path, authenticates the bearer credential with Bearer, and applies the
// ADR-00018 membership check to whoever the credential belongs to, so the tool
// runs with the credential and the household on its context. Routes then holds
// the credential to its rate limit and serves the SDK handler.
//
// Credentials are checked behind Verifier, sized to what Bearer calls, so a new
// kind of credential adds a verifier and touches no tool. KeyVerifier checks API
// keys (ADR-00031). OAuthVerifier checks OAuth access tokens (ADR-00034) against
// the audience of the request's Site and hands any other secret to a verifier
// behind it. A refusal is 401 with the Site's Protected Resource Metadata in
// WWW-Authenticate (Unauthorized).
//
// Tools that create a recipe only ever produce a draft that waits on the review
// screen for a person (ADR-00006). A tool that changes an existing recipe
// produces a proposal that waits in the recipe's editor for a person
// (ADR-00035). Shopping-list changes apply at
// once. Every write records the credential that made it (Audit): AuditByKind sends
// a key's to the key log and a token's to the grant log.
package mcp
