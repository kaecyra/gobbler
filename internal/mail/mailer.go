package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/ratelimit"
)

// ErrRateLimited means the sender is past their rate (ADR-00055). Nothing was
// sent and no connection was made.
var ErrRateLimited = errors.New("too many emails sent recently; try again later")

// Mailer sends invitation emails with the settings saved on the Deployment
// page, held to a leaky bucket per sender.
type Mailer struct {
	settings Reader
	limiter  *ratelimit.Limiter
	baseURL  string
	log      *slog.Logger
	now      func() time.Time
	send     func(ctx context.Context, s Settings, to string, data []byte) error
}

// NewMailer returns a Mailer. baseURL is the origin the site is reached at
// (config PublicBaseURL), the start of every link in an email. limiter is the
// shared ratelimit.Limiter (ADR-00036), keyed by the sender's verified email.
func NewMailer(r Reader, limiter *ratelimit.Limiter, baseURL string, log *slog.Logger) *Mailer {
	if log == nil {
		log = slog.Default()
	}
	return &Mailer{settings: r, limiter: limiter, baseURL: strings.TrimRight(baseURL, "/"), log: log, now: time.Now, send: Send}
}

// InvitationEmail describes one invitation to email.
type InvitationEmail struct {
	// Sender is the verified email of the person pressing Invite or Resend;
	// it keys the rate limit.
	Sender string
	// InvitedBy is who issued the invitation.
	InvitedBy string
	// To is the invited address.
	To string
	// Household is the household a join invitation leads to, empty for a
	// household invitation.
	Household string
	// Code names the invitation in its link.
	Code    string
	Expires time.Time
}

// SendInvitation emails the invitation. It returns ErrNotConfigured, wrapped,
// when mail is off, and ErrRateLimited when the sender is past their rate; in
// neither case is a connection made. Any other error is a failed send, with a
// message fit to store and show.
func (m *Mailer) SendInvitation(ctx context.Context, in InvitationEmail) error {
	s, err := m.load(ctx)
	if err != nil {
		return err
	}
	if ok, _ := m.limiter.Allow(in.Sender); !ok {
		m.log.WarnContext(ctx, "invitation email refused: sender over the mail rate", "sender", in.Sender)
		return ErrRateLimited
	}
	if m.baseURL == "" {
		return errors.New("the public base URL is not set, so the invitation link cannot be written")
	}
	msg, err := Invitation{
		To: in.To, InvitedBy: in.InvitedBy, Household: in.Household,
		Link: m.baseURL + "/invitations/" + in.Code, Expires: in.Expires,
	}.Message(s.From, m.now())
	if err != nil {
		return err
	}
	data, err := msg.Bytes()
	if err != nil {
		return fmt.Errorf("build invitation email: %w", err)
	}
	return m.send(ctx, s, in.To, data)
}

// load loads the settings, logging when saved mail settings leave mail
// off. Having no host is the ordinary off state and is not logged.
func (m *Mailer) load(ctx context.Context) (Settings, error) {
	s, err := Load(ctx, m.settings)
	if err != nil && errors.Is(err, ErrNotConfigured) && !errors.Is(err, errNoHost) {
		m.log.WarnContext(ctx, "mail host is saved but mail is off", "error", err.Error())
	}
	return s, err
}
