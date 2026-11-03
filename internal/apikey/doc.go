// Package apikey is the API key store (ADR-00031): personal keys, which act as
// one person, and household keys, which act for a household. A key is a random
// secret shown once, when it is created; the database holds only its SHA-256,
// a short non-secret prefix and a name to tell keys apart. The SQL is in
// queries/, generated into apikeydb (ADR-00014).
//
// This package manages keys. No route accepts one until the MCP endpoint
// exists (ADR-00034), so there is no lookup by secret yet.
package apikey
