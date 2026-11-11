package operator_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/mail"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web"
)

type recordingMailer struct {
	err   error
	calls []mail.InvitationEmail
}

func (m *recordingMailer) SendInvitation(_ context.Context, in mail.InvitationEmail) error {
	m.calls = append(m.calls, in)
	return m.err
}

// idleBackup is a backup job that has never run.
type idleBackup struct{}

func (idleBackup) Status() backup.Status { return backup.Status{} }
func (idleBackup) Run(context.Context) (backup.Result, error) {
	return backup.Result{}, errors.New("not used")
}

// Through the real router: an operator's invitation is emailed by the mailer
// the server was given, a failed send leaves it pending with the error on the
// page, and Resend sends again (ADR-00055).
func TestOperatorInviteIsEmailedAndResendable(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{
		Server:   config.Server{ListenAddr: "127.0.0.1:0", DataDir: t.TempDir(), DevAuthBypass: true},
		Operator: config.Operator{Emails: []string{auth.DevEmail}},
	}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg, auth.WithLogger(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	m := &recordingMailer{err: fmt.Errorf("recipient: %w: 550 no such user", mail.ErrRejected)}
	router, err := web.New(web.Deps{Config: cfg, DB: d, Auth: a, Mail: m, Backup: idleBackup{}, Avatars: profile.NewAvatars(ctx, profile.New(d), media.NewLocal(t.TempDir()), nil)})
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, target string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, "/operator/invitations", url.Values{"email": {"new@example.com"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("invite = %d: %s", rec.Code, rec.Body)
	}
	if len(m.calls) != 1 || m.calls[0].To != "new@example.com" || m.calls[0].Sender != auth.DevEmail || m.calls[0].Household != "" {
		t.Fatalf("sends = %+v, want one to new@example.com from the operator", m.calls)
	}
	body := do(http.MethodGet, "/operator/invitations", nil).Body.String()
	if !strings.Contains(body, "Email failed: "+mail.Reason(mail.ErrRejected)) {
		t.Fatalf("page does not show the failure:\n%s", body)
	}
	form := regexp.MustCompile(`action="(/operator/invitations/[A-Za-z0-9]+/resend)"`).FindStringSubmatch(body)
	if form == nil {
		t.Fatalf("no Resend form:\n%s", body)
	}

	m.err = nil
	if rec := do(http.MethodPost, form[1], nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("resend = %d: %s", rec.Code, rec.Body)
	}
	if len(m.calls) != 2 {
		t.Errorf("%d sends after resend, want 2", len(m.calls))
	}
	body = do(http.MethodGet, "/operator/invitations", nil).Body.String()
	if !strings.Contains(body, "Emailed ") || strings.Contains(body, "Email failed") || strings.Count(body, `data-testid="pending-invitation"`) != 1 {
		t.Errorf("after resend the page should show one emailed invitation:\n%s", body)
	}
}
