package mail

import "errors"

// The kinds of failure a send can have. A send error wraps exactly one of them
// (alongside the underlying error, which is for the log only), so a caller
// classifies with errors.Is and never by the error's text.
var (
	// ErrConnect means the server could not be reached, dropped the
	// connection, or did not answer in time.
	ErrConnect = errors.New("could not connect to the mail server")
	// ErrTLS means encryption was refused or failed: no STARTTLS, a failed
	// handshake, or a certificate that did not verify.
	ErrTLS = errors.New("TLS to the mail server failed")
	// ErrAuth means the server refused the user and password.
	ErrAuth = errors.New("the mail server refused the sign-in")
	// ErrRejected means the server refused the sender, the recipient or the
	// message.
	ErrRejected = errors.New("the mail server refused the message")
	// ErrNoBaseURL means the site's public address is not set, so no link can
	// be written.
	ErrNoBaseURL = errors.New("the public base URL is not set")
)

// Reason is the short, fixed sentence stored on an invitation and shown to its
// household for a failed send (ADR-00055). It is chosen by the kind of failure
// and holds nothing from the error itself: the underlying text can carry the
// SMTP user, server names or the provider's reply, and goes to the log alone.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrNotConfigured):
		return "email is not set up on this deployment"
	case errors.Is(err, ErrRateLimited):
		return "sending limit reached; try again later"
	case errors.Is(err, ErrNoBaseURL):
		return "the site's public address is not set, so the link cannot be written"
	case errors.Is(err, ErrTLS):
		return "the mail server's secure connection or certificate was refused"
	case errors.Is(err, ErrAuth):
		return "the mail server refused the sign-in"
	case errors.Is(err, ErrRejected):
		return "the mail server refused the sender or recipient"
	case errors.Is(err, ErrConnect):
		return "could not connect to the mail server, or it timed out"
	}
	return "the email could not be sent"
}
