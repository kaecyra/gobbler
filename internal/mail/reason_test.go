package mail

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReasonIsChosenByKindOfFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"connect", fmt.Errorf("connect to smtp.secret-host.example:587: %w: dial tcp: refused", ErrConnect), "could not connect to the mail server, or it timed out"},
		{"tls", fmt.Errorf("TLS: %w: x509: certificate is valid for a, not b", ErrTLS), "the mail server's secure connection or certificate was refused"},
		{"no starttls", fmt.Errorf("%w: %w", ErrNoSTARTTLS, ErrTLS), "the mail server's secure connection or certificate was refused"},
		{"auth", fmt.Errorf("sign in: %w: 535 bad user mailer-prod", ErrAuth), "the mail server refused the sign-in"},
		{"rejected", fmt.Errorf("recipient: %w: 550 no such user", ErrRejected), "the mail server refused the sender or recipient"},
		{"rate", ErrRateLimited, "sending limit reached; try again later"},
		{"off", fmt.Errorf("no host saved: %w", ErrNotConfigured), "email is not set up on this deployment"},
		{"base url", fmt.Errorf("link: %w", ErrNoBaseURL), "the site's public address is not set, so the link cannot be written"},
		{"unclassified", errors.New("something odd with secret-host"), "the email could not be sent"},
	} {
		if got := Reason(tc.err); got != tc.want {
			t.Errorf("%s: Reason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A real send failing against each kind of server wraps the matching kind, and
// the reason shown never carries the raw error's text.
func TestRealFailuresAreClassifiedAndTheirTextNeverReachesTheReason(t *testing.T) {
	badCert, _ := pki(t, []string{"mail.other.example"}, nil)
	good, goodRoots := pki(t, nil, loopback)

	cases := []struct {
		name   string
		s      *fakeServer
		roots  *x509.CertPool
		want   error
		reason string
	}{
		{"no starttls", &fakeServer{cert: good}, goodRoots, ErrTLS, "the mail server's secure connection or certificate was refused"},
		{"untrusted certificate", &fakeServer{starttls: true, cert: badCert}, goodRoots, ErrTLS, "the mail server's secure connection or certificate was refused"},
		{"sign-in refused", &fakeServer{starttls: true, cert: good, rejectAuth: true}, goodRoots, ErrAuth, "the mail server refused the sign-in"},
		{"recipient refused", &fakeServer{starttls: true, cert: good, rejectRcpt: true}, goodRoots, ErrRejected, "the mail server refused the sender or recipient"},
		{"silent server", &fakeServer{silent: true, cert: good}, goodRoots, ErrConnect, "could not connect to the mail server, or it timed out"},
	}
	for _, tc := range cases {
		s := startServer(t, tc.s)
		cfg := testSettings(s, "127.0.0.1")
		cfg.Timeout = 300 * time.Millisecond
		err := deliver(context.Background(), cfg, false, tc.roots, "cy@example.com", []byte("x"))
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want kind %v", tc.name, err, tc.want)
			continue
		}
		reason := Reason(err)
		if reason != tc.reason {
			t.Errorf("%s: reason = %q, want %q", tc.name, reason, tc.reason)
		}
		// The raw error holds the address, the user or the server's reply; the
		// reason holds none of it.
		for _, leak := range []string{"127.0.0.1", "mailbox-secret", "bad user", "x509", "dial tcp"} {
			if strings.Contains(reason, leak) {
				t.Errorf("%s: reason %q contains raw text %q", tc.name, reason, leak)
			}
		}
	}
}

func TestUnreachableServerIsAConnectFailure(t *testing.T) {
	err := Send(context.Background(), Settings{Host: "127.0.0.1", Port: 1, From: "g@example.com", Timeout: time.Second}, "cy@example.com", []byte("x"))
	if !errors.Is(err, ErrConnect) {
		t.Errorf("err = %v, want ErrConnect", err)
	}
}
