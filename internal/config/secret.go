package config

import "log/slog"

const redacted = "[REDACTED]"

// Secret is a credential read from the environment. Every rendering of it
// (fmt verbs, slog, JSON, text) yields a placeholder; the value is reachable
// only through Reveal, so a logged Config cannot leak a key.
type Secret struct{ value string }

// Reveal returns the credential. Call it only at the point of use.
func (s Secret) Reveal() string { return s.value }

// IsSet reports whether a value was configured.
func (s Secret) IsSet() bool { return s.value != "" }

func (s Secret) String() string {
	if s.value == "" {
		return ""
	}
	return redacted
}

// GoString covers the %#v verb.
func (s Secret) GoString() string { return `config.Secret("` + s.String() + `")` }

// LogValue redacts the secret under slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalText redacts the secret under encoding/json and other text encoders.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }
