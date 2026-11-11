package settings

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/mail"
	store "github.com/kaecyra/gobbler/internal/settings"
)

func mailHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	_, s, _ := integrationsFixture(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{MailSection()}}})
	return h, s
}

func postValues(h http.Handler, htmx bool, kv ...string) *httptest.ResponseRecorder {
	form := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		form.Set(kv[i], kv[i+1])
	}
	req := httptest.NewRequest(http.MethodPost, "/values/mail", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func value(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	v, err := s.Value(context.Background(), store.Field{Integration: mail.Integration, Name: name})
	if errors.Is(err, store.ErrUnset) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

var allMail = []string{
	mail.FieldHost, "smtp.example.com", mail.FieldPort, "587", mail.FieldUser, "u",
	mail.FieldFrom, "Gobbler <g@example.com>", mail.FieldTimeout, "30s",
}

func TestMailSectionShowsItsFieldsAndThePasswordControl(t *testing.T) {
	h, _ := mailHandler(t)
	body := page(h)
	for _, want := range []string{`data-testid="value-host"`, `data-testid="value-port"`, `data-testid="value-user"`, `data-testid="value-from"`, `data-testid="value-timeout"`, `data-testid="secret-mail-password"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

// ADR-00055 obligation 6: a host without the rest is refused, and the message
// says which field is missing.
func TestAHostWithoutTheRestIsRefusedAndNamesTheMissingField(t *testing.T) {
	h, s := mailHandler(t)
	post(h, "/secrets/mail/password", false, "pw")
	for _, tc := range []struct{ missing, label string }{
		{mail.FieldPort, "Port"}, {mail.FieldUser, "User"}, {mail.FieldFrom, "From address"},
	} {
		var kv []string
		for i := 0; i < len(allMail); i += 2 {
			if allMail[i] != tc.missing {
				kv = append(kv, allMail[i], allMail[i+1])
			}
		}
		rec := postValues(h, false, kv...)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tc.label+" is required when a host is set.") {
			t.Errorf("without %s: %d\n%s", tc.missing, rec.Code, rec.Body)
		}
		if got := value(t, s, mail.FieldHost); got != "" {
			t.Errorf("without %s: host %q was saved", tc.missing, got)
		}
	}
}

func TestAHostWithNoPasswordSavedIsRefused(t *testing.T) {
	h, s := mailHandler(t)
	rec := postValues(h, true, allMail...)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Password is required when a host is set") {
		t.Fatalf("%d\n%s", rec.Code, rec.Body)
	}
	if value(t, s, mail.FieldHost) != "" {
		t.Error("the host was saved with no password")
	}
	// What was typed is still in the form.
	if !strings.Contains(rec.Body.String(), `value="smtp.example.com"`) {
		t.Errorf("the refused form lost what was typed:\n%s", rec.Body)
	}
}

func TestCompleteMailSettingsSaveAndShow(t *testing.T) {
	h, s := mailHandler(t)
	post(h, "/secrets/mail/password", false, "pw")
	rec := postValues(h, false, allMail...)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("save = %d to %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	for i := 0; i < len(allMail); i += 2 {
		if got := value(t, s, allMail[i]); got != allMail[i+1] {
			t.Errorf("%s = %q, want %q", allMail[i], got, allMail[i+1])
		}
	}
	// The mailer can read what was saved.
	if cfg, err := mail.Load(context.Background(), s); err != nil || cfg.Host != "smtp.example.com" || cfg.Port != 587 || cfg.Password != "pw" {
		t.Errorf("mail.Load = %+v, %v", cfg, err)
	}
	if body := page(h); !strings.Contains(body, `value="smtp.example.com"`) || !strings.Contains(body, `value="587"`) {
		t.Errorf("page does not show the saved values:\n%s", body)
	}
}

func TestBlankHostTurnsMailOff(t *testing.T) {
	h, s := mailHandler(t)
	post(h, "/secrets/mail/password", false, "pw")
	postValues(h, false, allMail...)
	rec := postValues(h, false, mail.FieldHost, "", mail.FieldPort, "587")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clearing = %d:\n%s", rec.Code, rec.Body)
	}
	if _, err := mail.Load(context.Background(), s); !errors.Is(err, mail.ErrNotConfigured) {
		t.Errorf("mail.Load after blanking the host = %v, want ErrNotConfigured", err)
	}
	if value(t, s, mail.FieldUser) != "" || value(t, s, mail.FieldPort) != "587" {
		t.Error("a blank field was not cleared or a given one was not kept")
	}
}

func TestMailFieldsAreValidated(t *testing.T) {
	h, _ := mailHandler(t)
	post(h, "/secrets/mail/password", false, "pw")
	for name, change := range map[string][2]string{
		"url as host":      {mail.FieldHost, "https://smtp.example.com"},
		"port not number":  {mail.FieldPort, "smtp"},
		"from not address": {mail.FieldFrom, "nope"},
		"timeout not time": {mail.FieldTimeout, "soon"},
	} {
		kv := append([]string{}, allMail...)
		for i := 0; i < len(kv); i += 2 {
			if kv[i] == change[0] {
				kv[i+1] = change[1]
			}
		}
		if rec := postValues(h, false, kv...); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422", name, rec.Code)
		}
	}
}

func TestValuesForAnUnregisteredIntegrationIs404(t *testing.T) {
	h, _ := mailHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/values/nope", strings.NewReader("x=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("= %d, want 404", rec.Code)
	}
	// And a section with only secrets (the test section) has no values form.
	h2, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	req = httptest.NewRequest(http.MethodPost, "/values/mail", strings.NewReader("x=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("secrets-only section = %d, want 404", rec.Code)
	}
}
