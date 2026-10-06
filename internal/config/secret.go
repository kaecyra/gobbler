package config

import (
	"fmt"
	"io"
	"log/slog"
)

const redacted = "[REDACTED]"

// Secret is a credential read from the environment. Every fmt verb (through
// Format), slog and the text and JSON encoders yield a placeholder; the value
// is reachable only through Reveal, so a logged Config cannot leak a key.
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

// GoString is the %#v form.
func (s Secret) GoString() string { return `config.Secret("` + s.String() + `")` }

// Format renders the redacted form for every verb and flag. Without it fmt
// consults String only for %v %s %x %X %q and reflects into the unexported
// value for the rest (%d, %t, ...).
func (s Secret) Format(f fmt.State, verb rune) {
	out := s.String()
	if verb == 'v' && f.Flag('#') {
		out = s.GoString()
	}
	// fmt.Formatter has no error return, and fmt's own verb handlers drop
	// write errors the same way; a failed write to the caller's sink cannot
	// leak the secret.
	_, _ = io.WriteString(f, out)
}

// LogValue redacts the secret under slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalText redacts the secret under encoding/json and other text encoders.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }
