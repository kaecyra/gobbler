package mail

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/settings"
)

// Integration is the name the mail settings have in the settings store.
const Integration = "mail"

// The mail fields in the settings store. Password is a secret; the others are
// plain values.
const (
	FieldHost     = "host"
	FieldPort     = "port"
	FieldUser     = "user"
	FieldFrom     = "from"
	FieldTimeout  = "timeout"
	FieldPassword = "password"
)

// DefaultTimeout is how long one send may take when no timeout is saved.
const DefaultTimeout = 20 * time.Second

// MaxTimeout bounds the saved timeout: a send runs inside the request that
// creates the invitation.
const MaxTimeout = 2 * time.Minute

// ErrNotConfigured means no mail is to be sent: no host is saved, or the
// saved password cannot be read. Nothing was attempted.
var ErrNotConfigured = errors.New("mail is not configured")

// errNoHost is the ordinary off state: nothing to log.
var errNoHost = fmt.Errorf("no host saved: %w", ErrNotConfigured)

// Settings are the mail settings in force.
type Settings struct {
	Host     string
	Port     int
	User     string
	Password string
	// From is the address mail is sent from, as the owner wrote it; it may
	// carry a display name.
	From    string
	Timeout time.Duration
}

// ImplicitTLS reports whether the connection is TLS from the first byte. The
// port decides (ADR-00055): 465 is, any other port is STARTTLS.
func (s Settings) ImplicitTLS() bool { return s.Port == 465 }

// Reader is what Load reads from the settings store. *settings.Store
// satisfies it.
type Reader interface {
	Value(ctx context.Context, f settings.Field) (string, error)
	Secret(ctx context.Context, f settings.Field) (string, error)
}

func field(name string) settings.Field { return settings.Field{Integration: Integration, Name: name} }

// Load reads the mail settings. It returns ErrNotConfigured, wrapped, when no
// host is saved or the password is unset or unreadable (ADR-00057: the
// integration is off). A host with another field missing or malformed is an
// error: the Deployment page does not save one, so it is a fault to be seen.
func Load(ctx context.Context, r Reader) (Settings, error) {
	host, err := r.Value(ctx, field(FieldHost))
	if errors.Is(err, settings.ErrUnset) {
		return Settings{}, errNoHost
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read mail host: %w", err)
	}
	s := Settings{Host: host, Timeout: DefaultTimeout}
	pass, err := r.Secret(ctx, field(FieldPassword))
	switch {
	case errors.Is(err, settings.ErrUnset), errors.Is(err, settings.ErrUnreadable):
		return Settings{}, fmt.Errorf("password unavailable: %w: %w", err, ErrNotConfigured)
	case err != nil:
		return Settings{}, fmt.Errorf("read mail password: %w", err)
	}
	s.Password = pass
	for _, f := range []struct {
		name string
		into *string
	}{{FieldUser, &s.User}, {FieldFrom, &s.From}} {
		if *f.into, err = r.Value(ctx, field(f.name)); err != nil {
			return Settings{}, fmt.Errorf("read mail %s: %w", f.name, err)
		}
	}
	rawPort, err := r.Value(ctx, field(FieldPort))
	if err != nil {
		return Settings{}, fmt.Errorf("read mail port: %w", err)
	}
	if s.Port, err = strconv.Atoi(rawPort); err != nil || s.Port < 1 || s.Port > 65535 {
		return Settings{}, fmt.Errorf("saved mail port %q is not a port number", rawPort)
	}
	rawTimeout, err := r.Value(ctx, field(FieldTimeout))
	switch {
	case errors.Is(err, settings.ErrUnset):
	case err != nil:
		return Settings{}, fmt.Errorf("read mail timeout: %w", err)
	default:
		if s.Timeout, err = time.ParseDuration(rawTimeout); err != nil || s.Timeout <= 0 {
			return Settings{}, fmt.Errorf("saved mail timeout %q is not a duration", rawTimeout)
		}
	}
	return s, nil
}

// Labels are what the Deployment page calls each field, for the message that
// names what is missing.
var Labels = map[string]string{
	FieldHost: "Host", FieldPort: "Port", FieldUser: "User", FieldFrom: "From address",
	FieldTimeout: "Timeout", FieldPassword: "Password",
}

// Validate checks the plain mail fields as the Deployment page received them
// (keyed by the Field names; blank is absent) and whether a password is set:
// saved, or typed in the same submit. It returns the problem to show, or ""
// when they can be saved. With no host there is nothing to check: mail is off.
// With a host, the port, user, from address and password are required
// (ADR-00055), and the first one missing is named.
func Validate(v map[string]string, passwordSet bool) string {
	host := strings.TrimSpace(v[FieldHost])
	if host == "" {
		return ""
	}
	if strings.ContainsAny(host, " \t/:@") || strings.ContainsFunc(host, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return "Host must be a name or address only, with no scheme, port or path."
	}
	for _, name := range []string{FieldPort, FieldUser, FieldFrom} {
		if strings.TrimSpace(v[name]) == "" {
			return Labels[name] + " is required when a host is set."
		}
	}
	if !passwordSet {
		return "Password is required when a host is set."
	}
	if p, err := strconv.Atoi(strings.TrimSpace(v[FieldPort])); err != nil || p < 1 || p > 65535 {
		return "Port must be a number from 1 to 65535."
	}
	if _, err := mail.ParseAddress(v[FieldFrom]); err != nil {
		return "From address is not an email address."
	}
	if t := strings.TrimSpace(v[FieldTimeout]); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil || d <= 0 || d > MaxTimeout {
			return "Timeout must be a duration such as 20s, up to " + MaxTimeout.String() + "."
		}
	}
	return ""
}
