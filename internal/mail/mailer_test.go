package mail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/ratelimit"
)

func testMailer(r Reader, burst int, calls *atomic.Int32, sendErr error) *Mailer {
	m := NewMailer(r, ratelimit.New(0.001, burst, time.Now), "https://gobbler.example.com/", slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.send = func(_ context.Context, _ Settings, _ string, data []byte) error {
		calls.Add(1)
		if !strings.Contains(string(data), "https://gobbler.example.com/invitations/Code1234") {
			return errors.New("link missing from the message")
		}
		return sendErr
	}
	return m
}

func email(sender string) InvitationEmail {
	return InvitationEmail{Sender: sender, InvitedBy: sender, To: "cy@example.com", Household: "Smiths", Code: "Code1234", Expires: time.Now().Add(time.Hour)}
}

func TestMailerWithNoHostAttemptsNothing(t *testing.T) {
	var calls atomic.Int32
	m := testMailer(fakeReader{}, 5, &calls, nil)
	if err := m.SendInvitation(context.Background(), email("ann@example.com")); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
	if calls.Load() != 0 {
		t.Error("a send was attempted with no host")
	}
}

func TestMailerSendsAndReportsFailure(t *testing.T) {
	var calls atomic.Int32
	if err := testMailer(full(), 5, &calls, nil).SendInvitation(context.Background(), email("ann@example.com")); err != nil || calls.Load() != 1 {
		t.Errorf("send = %v after %d calls", err, calls.Load())
	}
	boom := errors.New("550 mailbox unavailable")
	if err := testMailer(full(), 5, &calls, boom).SendInvitation(context.Background(), email("ann@example.com")); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the send error", err)
	}
}

// ADR-00055 obligation 9: per sender, past the burst nothing is attempted.
func TestMailerLimitsEachSenderAlone(t *testing.T) {
	var calls atomic.Int32
	m := testMailer(full(), 2, &calls, nil)
	ctx := context.Background()
	for i := range 2 {
		if err := m.SendInvitation(ctx, email("ann@example.com")); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := m.SendInvitation(ctx, email("ann@example.com")); !errors.Is(err, ErrRateLimited) {
		t.Errorf("third send = %v, want ErrRateLimited", err)
	}
	if calls.Load() != 2 {
		t.Errorf("%d sends were made, want 2", calls.Load())
	}
	if err := m.SendInvitation(ctx, email("bob@example.com")); err != nil {
		t.Errorf("another sender = %v, want sent", err)
	}
}

func TestMailerWithoutABaseURLFailsLoud(t *testing.T) {
	var calls atomic.Int32
	m := testMailer(full(), 5, &calls, nil)
	m.baseURL = ""
	if err := m.SendInvitation(context.Background(), email("ann@example.com")); err == nil || calls.Load() != 0 {
		t.Errorf("err = %v, calls = %d; want a failure and no attempt", err, calls.Load())
	}
}

func TestMessageHeadersCannotBeInjected(t *testing.T) {
	m, err := Invitation{
		To: "cy@example.com", InvitedBy: "ann@example.com", Household: "Evil\r\nBcc: x@example.com",
		Link: "https://g.example.com/invitations/a", Expires: time.Now(),
	}.Message("g@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, err := m.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(data), "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Errorf("a household name started a header:\n%s", head)
	}
}

// With no base URL no send can happen, so no attempt uses up the sender's
// bucket: once it is set, the sender's whole burst is still there.
func TestMissingBaseURLDoesNotSpendTheSendersBucket(t *testing.T) {
	var calls atomic.Int32
	m := testMailer(full(), 1, &calls, nil)
	good := m.baseURL
	m.baseURL = ""
	for range 3 {
		if err := m.SendInvitation(context.Background(), email("ann@example.com")); !errors.Is(err, ErrNoBaseURL) {
			t.Fatalf("err = %v, want ErrNoBaseURL", err)
		}
	}
	m.baseURL = good
	if err := m.SendInvitation(context.Background(), email("ann@example.com")); err != nil {
		t.Errorf("after the base URL was set: %v, want sent (the bucket was spent)", err)
	}
}
