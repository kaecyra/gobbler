// Package shopping is the shopping service: every operation on shopping lists
// and the pantry, written once, for the web handlers and the MCP tools to call
// (ADR-00043). It is in-process. It is not a network API, it speaks neither
// HTTP nor JSON, and it reads no cookie or header; authentication stays at the
// adapters, which hand it a verified Caller.
//
// This package is the pilot of internal/api, so its shape is the pattern every
// later resource follows:
//
//   - Signature. A Service method takes the context first, then the Caller,
//     then the operation's own arguments: s.AddEntry(ctx, caller, listID, ...).
//     It returns domain values and domain errors, never a page or a status.
//   - Caller. The Caller type and the membership check are internal/api/caller,
//     shared by every resource; shopping re-exports them. A Caller is the verified person and the household they belong to.
//     An adapter builds it from what it has already verified: the session's
//     email and the membership middleware's household for the web, the
//     credential's email and household for MCP. Every method refuses a Caller
//     with ErrNotMember unless the person is a member of the household,
//     checked against the household store (Members), and runs in that
//     household, not whatever household the context happens to carry
//     (ADR-00018). A household API key has no person; its Caller sets
//     HouseholdKey and is admitted by its household alone (ADR-00031). The
//     stores below still scope every row by that household, so another
//     household's list stays ErrNotFound.
//   - Types. The package re-exports the domain types and errors its callers
//     need (View, ListInfo, ErrNotFound, ...) as aliases, so an adapter imports
//     this package and nothing from internal/shopping. A depguard rule in
//     .golangci.yml fails make lint when internal/web or internal/mcp imports
//     the shopping domain.
//   - Errors. A method returns the domain's sentinels (ErrNotFound, ErrInvalid,
//     ErrDuplicate, ErrUnknownReference, ErrNotMember), wrapped with context.
//     The web turns them into statuses through the one table in
//     internal/web/layout (statusTable); MCP turns them into tool errors in
//     internal/mcp/tools.go. A new sentinel is one row in each.
package shopping
