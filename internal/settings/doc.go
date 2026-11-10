// Package settings stores a deployment's integration settings (ADR-00057): the
// mail and LLM settings an operator edits on the Deployment page. A setting is
// addressed by an integration name and a field name. Plain settings are kept
// as text. Secrets are sealed with AES-256-GCM under a key derived with
// HKDF-SHA256 from the environment's encryption key, with a fresh nonce per
// write and the integration and field names as associated data. A secret that
// cannot be opened (no key, or a key that does not match) is reported as
// ErrUnreadable, which a caller treats as unset but can tell apart from
// ErrUnset. The SQL is in queries/, generated into settingsdb (ADR-00014).
package settings
