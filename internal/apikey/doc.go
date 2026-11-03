// Package apikey is the API key store (ADR-00031): personal keys, which act as
// one person, and household keys, which act for a household. A key is a random
// secret shown once, when it is created; the database holds only its SHA-256,
// a short non-secret prefix and a name to tell keys apart. The SQL is in
// queries/, generated into apikeydb (ADR-00014).
//
// Lookup finds the live key a secret names, for the MCP endpoint (ADR-00034),
// and RecordWrite logs the writes a key makes.
package apikey
