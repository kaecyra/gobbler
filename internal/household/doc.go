// Package household is the tenancy store (ADR-00018): households, their
// members and the invitations that create both. An email belongs to at most
// one household, and nobody joins or starts a household without an invitation
// addressed to their email. The SQL is in queries/, generated into householddb
// (ADR-00014). It also carries the request's household on the context, which
// the web layer sets once the request's identity is verified and handlers
// read.
package household
