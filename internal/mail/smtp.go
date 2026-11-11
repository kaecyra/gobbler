package mail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
)

// ErrNoSTARTTLS means the server on a STARTTLS port did not offer STARTTLS.
// Nothing was sent to it: no credentials and no message (ADR-00055).
var ErrNoSTARTTLS = errors.New("the mail server does not offer STARTTLS; refusing to send without encryption")

// Send delivers the message data (a complete RFC 5322 message, from
// Message.Bytes) from the envelope sender to one recipient, using the saved
// settings. The connection is encrypted before anything else is sent and the
// server's certificate is verified against the host.
func Send(ctx context.Context, s Settings, to string, data []byte) error {
	return deliver(ctx, s, s.ImplicitTLS(), nil, to, data)
}

// deliver is Send with the TLS mode and the trusted roots made explicit, so
// tests can run it against a server whose certificate they issued. nil roots
// means the system's. There is no way to skip verification.
func deliver(ctx context.Context, s Settings, implicit bool, roots *x509.CertPool, to string, data []byte) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("mail from address %q: %w", s.From, err)
	}
	if s.Timeout <= 0 {
		return errors.New("mail timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	tlsCfg := &tls.Config{ServerName: s.Host, RootCAs: roots, MinVersion: tls.VersionTLS12}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to %s: %w: %w", addr, ErrConnect, err)
	}
	// One deadline for the whole exchange, and a closed connection when the
	// caller gives up, so no step waits on a silent server.
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set deadline on %s: %w: %w", addr, ErrConnect, err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if implicit {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("TLS to %s: %w: %w", addr, ErrTLS, err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("greeting from %s: %w: %w", addr, ErrConnect, err)
	}
	defer func() { _ = c.Close() }()
	if !implicit {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%w: %w", ErrNoSTARTTLS, ErrTLS)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS with %s: %w: %w", addr, ErrTLS, err)
		}
	}
	// PlainAuth itself refuses an unencrypted connection to any host but
	// localhost; the connection is encrypted by here in every case.
	if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
		return fmt.Errorf("sign in to %s: %w: %w", addr, ErrAuth, err)
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("sender %s: %w: %w", from.Address, ErrRejected, err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("recipient: %w: %w", ErrRejected, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("start message: %w: %w", ErrRejected, err)
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return fmt.Errorf("write message: %w: %w", ErrConnect, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish message: %w: %w", ErrRejected, err)
	}
	// The server has accepted the message. A failed QUIT cannot undo that, and
	// reporting it would make a resend send the message twice; the deferred
	// Close ends the connection either way.
	_ = c.Quit()
	return nil
}
