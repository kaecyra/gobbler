package households_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/mail"
	"github.com/kaecyra/gobbler/internal/ratelimit"
	"github.com/kaecyra/gobbler/internal/settings"
)

// fakeMailer stands in for mail.Mailer where a test needs to choose the
// outcome of a send.
type fakeMailer struct {
	err   error
	calls []mail.InvitationEmail
}

func (m *fakeMailer) SendInvitation(_ context.Context, in mail.InvitationEmail) error {
	m.calls = append(m.calls, in)
	return m.err
}

func withMailer(t *testing.T, f fixture, m households.Mailer) *households.Service {
	t.Helper()
	svc, err := households.New(households.Deps{Households: f.store, Operators: config.Operator{Emails: []string{rootEmail}}, Mailer: m})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func pendingOf(t *testing.T, svc *households.Service, c households.Caller) []households.Invitation {
	t.Helper()
	p, err := svc.Pending(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ADR-00055 obligation 6: with no host saved, an invitation is created and no
// send is attempted.
func TestInviteWithMailOffCreatesTheInvitationAndSendsNothing(t *testing.T) {
	f := newFixture(t)
	m := &fakeMailer{err: fmt.Errorf("no host saved: %w", mail.ErrNotConfigured)}
	svc := withMailer(t, f, m)
	code, err := svc.Invite(context.Background(), f.ann, newEmail)
	if err != nil || code == "" {
		t.Fatalf("Invite = %q, %v", code, err)
	}
	p := pendingOf(t, svc, f.ann)
	if len(p) != 1 || !p[0].EmailedAt.IsZero() || p[0].EmailError != "" {
		t.Errorf("pending = %+v, want one invitation never emailed and no error", p)
	}
}

func TestInviteEmailsTheLinkAndRecordsWhenItWentOut(t *testing.T) {
	f := newFixture(t)
	m := &fakeMailer{}
	svc := withMailer(t, f, m)
	code, err := svc.Invite(context.Background(), f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 1 {
		t.Fatalf("%d sends, want 1", len(m.calls))
	}
	got := m.calls[0]
	if got.Code != code || got.To != newEmail || got.Household != "Smiths" || got.InvitedBy != annEmail || got.Sender != annEmail {
		t.Errorf("send = %+v", got)
	}
	p := pendingOf(t, svc, f.ann)
	if len(p) != 1 || p[0].EmailedAt.IsZero() || p[0].EmailError != "" {
		t.Errorf("pending = %+v, want emailed with no error", p)
	}
}

// ADR-00055 obligation 8.
func TestAFailedSendLeavesTheInvitationWithItsErrorAndResendSendsAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := &fakeMailer{err: fmt.Errorf("recipient cy@example.com: %w: 550 mailbox unavailable", mail.ErrRejected)}
	svc := withMailer(t, f, m)
	code, err := svc.Invite(ctx, f.ann, newEmail)
	if err != nil || code == "" {
		t.Fatalf("a failed send failed the invite: %q, %v", code, err)
	}
	p := pendingOf(t, svc, f.ann)
	if len(p) != 1 || p[0].EmailError != mail.Reason(mail.ErrRejected) || !p[0].EmailedAt.IsZero() {
		t.Fatalf("pending = %+v, want the error stored", p)
	}

	m.err = nil
	if err := svc.Resend(ctx, f.ann, code); err != nil {
		t.Fatal(err)
	}
	p = pendingOf(t, svc, f.ann)
	if len(m.calls) != 2 || len(p) != 1 || p[0].Code != code {
		t.Fatalf("after resend: %d sends, pending %+v; want 2 sends and the same single invitation", len(m.calls), p)
	}
	if p[0].EmailedAt.IsZero() || p[0].EmailError != "" {
		t.Errorf("resend left %+v, want sent time set and error cleared", p[0])
	}
}

func TestResendIsForAPendingInvitationOfTheCallersHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := &fakeMailer{}
	svc := withMailer(t, f, m)
	code, err := svc.Invite(ctx, f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resend(ctx, f.jo, code); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("another household resending = %v, want ErrNotFound", err)
	}
	if err := svc.Revoke(ctx, f.ann, code); err != nil {
		t.Fatal(err)
	}
	if err := svc.Resend(ctx, f.ann, code); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("resending a revoked invitation = %v, want ErrNotFound", err)
	}
	if len(m.calls) != 1 {
		t.Errorf("%d sends, want only the first", len(m.calls))
	}
}

func TestResendWithMailOffSaysSoOnTheInvitation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := withMailer(t, f, nil)
	code, err := svc.Invite(ctx, f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resend(ctx, f.ann, code); err != nil {
		t.Fatal(err)
	}
	p := pendingOf(t, svc, f.ann)
	if len(p) != 1 || p[0].EmailError == "" || !p[0].EmailedAt.IsZero() {
		t.Errorf("pending = %+v, want an error saying email is not set up", p)
	}
}

func TestOperatorInviteAndResend(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := &fakeMailer{err: fmt.Errorf("connect: %w: i/o timeout", mail.ErrConnect)}
	svc := withMailer(t, f, m)
	code, err := svc.OperatorInvite(ctx, f.root, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 1 || m.calls[0].Household != "" || m.calls[0].Sender != rootEmail {
		t.Fatalf("sends = %+v, want one with no household from the operator", m.calls)
	}
	p, _ := svc.OperatorPending(ctx, f.root)
	if len(p) != 1 || p[0].EmailError != mail.Reason(mail.ErrConnect) {
		t.Fatalf("pending = %+v", p)
	}
	m.err = nil
	if err := svc.OperatorResend(ctx, f.root, code); err != nil {
		t.Fatal(err)
	}
	p, _ = svc.OperatorPending(ctx, f.root)
	if len(p) != 1 || p[0].EmailedAt.IsZero() || p[0].EmailError != "" {
		t.Errorf("after resend pending = %+v", p)
	}
	other := households.Caller{Person: "root2@example.com"}
	svc2, err := households.New(households.Deps{Households: f.store, Operators: config.Operator{Emails: []string{rootEmail, other.Person}}, Mailer: m})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc2.OperatorResend(ctx, other, code); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("another operator resending = %v, want ErrNotFound", err)
	}
}

// ADR-00055 obligation 9, through the real mailer, limiter and settings store:
// past the burst one sender's invite still creates the invitation with the
// refusal stored and no connection made, and another sender is unaffected.
func TestPastTheBurstTheInvitationIsCreatedWithTheRefusalAndNoConnection(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	ss, err := settings.New(d, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var conns atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			_ = c.Close() // the greeting never comes: the send fails
		}
	}()
	for field, v := range map[string]string{
		mail.FieldHost: "127.0.0.1", mail.FieldPort: fmt.Sprint(ln.Addr().(*net.TCPAddr).Port),
		mail.FieldUser: "u", mail.FieldFrom: "g@example.com", mail.FieldTimeout: "2s",
	} {
		if err := ss.SetValue(ctx, settings.Field{Integration: mail.Integration, Name: field}, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := ss.SetSecret(ctx, settings.Field{Integration: mail.Integration, Name: mail.FieldPassword}, "pw"); err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(0.0001, 2, time.Now)
	svc := withMailer(t, f, mail.NewMailer(ss, limiter, "https://gobbler.example.com", nil))

	for i := range 2 {
		if _, err := svc.Invite(ctx, f.ann, fmt.Sprintf("in%d@example.com", i)); err != nil {
			t.Fatal(err)
		}
	}
	if conns.Load() != 2 {
		t.Fatalf("%d connections for the first two invites, want 2", conns.Load())
	}
	code, err := svc.Invite(ctx, f.ann, "in2@example.com")
	if err != nil || code == "" {
		t.Fatalf("an invite past the burst failed: %q, %v", code, err)
	}
	if conns.Load() != 2 {
		t.Errorf("%d connections, want none for the refused send", conns.Load())
	}
	var refused households.Invitation
	for _, p := range pendingOf(t, svc, f.ann) {
		if p.Code == code {
			refused = p
		}
	}
	if refused.Code == "" || refused.EmailError != mail.Reason(mail.ErrRateLimited) {
		t.Errorf("refused invitation = %+v, want the rate refusal stored", refused)
	}
	if err := svc.Resend(ctx, f.ann, code); err != nil || conns.Load() != 2 {
		t.Errorf("resend past the burst = %v with %d connections; Resend draws from the same bucket", err, conns.Load())
	}

	// Another sender has a bucket of their own.
	if _, err := svc.Invite(ctx, f.jo, "joinvite@example.com"); err != nil {
		t.Fatal(err)
	}
	if conns.Load() != 3 {
		t.Errorf("%d connections after another sender's invite, want 3", conns.Load())
	}
}

// Every member of the household sees the stored error, so it holds a fixed
// sentence for the kind of failure and nothing of the raw error, which can
// carry the SMTP user, a server name or the provider's reply.
func TestTheStoredErrorNeverHoldsTheRawErrorText(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	raw := fmt.Errorf("sign in to smtp.secret-provider.example:587: %w: 535 5.7.8 user mailer-prod@corp rejected", mail.ErrAuth)
	svc := withMailer(t, f, &fakeMailer{err: raw})
	if _, err := svc.Invite(ctx, f.ann, newEmail); err != nil {
		t.Fatal(err)
	}
	p := pendingOf(t, svc, f.ann)
	if len(p) != 1 || p[0].EmailError != mail.Reason(mail.ErrAuth) {
		t.Fatalf("stored = %+v, want the fixed sign-in sentence", p)
	}
	for _, leak := range []string{"secret-provider", "mailer-prod", "535", "587"} {
		if strings.Contains(p[0].EmailError, leak) {
			t.Errorf("stored error %q contains raw text %q", p[0].EmailError, leak)
		}
	}
	// An error of no known kind is stored as the generic sentence, not its text.
	svc = withMailer(t, f, &fakeMailer{err: errors.New("panic: secret-provider exploded")})
	code, err := svc.Invite(ctx, f.ann, "other@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, inv := range pendingOf(t, svc, f.ann) {
		if inv.Code == code && (strings.Contains(inv.EmailError, "secret-provider") || inv.EmailError != mail.Reason(errors.New("x"))) {
			t.Errorf("unclassified error stored as %q", inv.EmailError)
		}
	}
}

// An invitation code is the credential that admits someone to a household
// (ADR-00028) and never reaches a log line.
func TestNoLogLineHoldsAnInvitationCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	svc, err := households.New(households.Deps{
		Households: f.store, Operators: config.Operator{Emails: []string{rootEmail}}, Logger: log,
		Mailer: &fakeMailer{err: fmt.Errorf("x: %w", mail.ErrConnect)},
	})
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.Invite(ctx, f.ann, newEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Resend(ctx, f.ann, code); err != nil {
		t.Fatal(err)
	}
	opCode, err := svc.OperatorInvite(ctx, f.root, "founder@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("nothing was logged; the test would pass vacuously")
	}
	for _, c := range []string{code, opCode} {
		if strings.Contains(buf.String(), c) {
			t.Errorf("a log line holds invitation code %s:\n%s", c, buf.String())
		}
	}
}
