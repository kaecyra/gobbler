package settings

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	store "github.com/kaecyra/gobbler/internal/settings"
)

// The page lists whatever an integration registers. These tests register a
// section of their own over a real store and database, because no integration
// has yet.
var testSection = Section{
	Integration: "mail", Title: "Mail",
	Secrets: []SecretField{{Name: "password", Label: "Password"}},
}

var mailPassword = store.Field{Integration: "mail", Name: "password"}

func integrationsHandler(t *testing.T, key []byte) (http.Handler, *store.Store) {
	t.Helper()
	h, s, _ := integrationsFixture(t, key)
	return h, s
}

func integrationsFixture(t *testing.T, key []byte) (http.Handler, *store.Store, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	s, err := store.New(d, key)
	if err != nil {
		t.Fatal(err)
	}
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{testSection}}})
	return h, s, d
}

func post(h http.Handler, target string, htmx bool, value string) *httptest.ResponseRecorder {
	form := url.Values{}
	if value != "" {
		form.Set("value", value)
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func page(h http.Handler) string { return serve(h, http.MethodGet, "/", false).Body.String() }

func TestNoRegisteredSecretsShowNothingNew(t *testing.T) {
	body := serve(Routes(Deps{Backup: &fakeBackup{}}), http.MethodGet, "/", false).Body.String()
	for _, s := range []string{`data-testid="integrations"`, "secret-input", "Not set"} {
		if strings.Contains(body, s) {
			t.Errorf("page shows %q with no integration registered", s)
		}
	}
}

func TestSecretControlsEnterReplaceClear(t *testing.T) {
	ctx := context.Background()
	h, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))

	body := page(h)
	if !strings.Contains(body, "Not set") || strings.Contains(body, ">Clear<") {
		t.Fatalf("an unset secret should offer Save and no Clear:\n%s", body)
	}

	if rec := post(h, "/secrets/mail/password", false, "hunter2"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("enter: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, err := s.Secret(ctx, mailPassword); err != nil || got != "hunter2" {
		t.Fatalf("stored = %q, %v", got, err)
	}
	body = page(h)
	if !strings.Contains(body, "Set") || !strings.Contains(body, "Replace") || !strings.Contains(body, ">Clear<") {
		t.Fatalf("a set secret should offer Replace and Clear:\n%s", body)
	}

	post(h, "/secrets/mail/password", true, "second")
	if got, _ := s.Secret(ctx, mailPassword); got != "second" {
		t.Fatalf("after replace = %q", got)
	}
	rec := post(h, "/secrets/mail/password/clear", true, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Not set") {
		t.Fatalf("clear via htmx: %d %s", rec.Code, rec.Body)
	}
	if st, _ := s.SecretState(ctx, mailPassword); st != store.StateUnset {
		t.Fatalf("after clear state = %v", st)
	}
}

// ADR-00057 obligation 5: no response from the page contains a saved value.
func TestNoResponseContainsASavedSecret(t *testing.T) {
	const secret = "s3cr3t-Value-0123456789"
	h, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	responses := map[string]*httptest.ResponseRecorder{
		"save, no script":   post(h, "/secrets/mail/password", false, secret),
		"save, htmx":        post(h, "/secrets/mail/password", true, secret),
		"page":              serve(h, http.MethodGet, "/", false),
		"fragment (rename)": post(h, "/secrets/mail/password", true, secret+"2"),
	}
	for name, rec := range responses {
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Header().Get("Location"), secret) {
			t.Errorf("%s: the response holds the saved value:\n%s", name, rec.Body)
		}
	}
	if strings.Contains(page(h), "value=\"s3cr3t") {
		t.Error("the input is prefilled")
	}
}

func TestUnreadableSecretIsMarkedAndCanBeClearedOrReplaced(t *testing.T) {
	ctx := context.Background()
	_, s, d := integrationsFixture(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	if err := s.SetSecret(ctx, mailPassword, "old"); err != nil {
		t.Fatal(err)
	}
	// The same database under another key.
	other, err := store.New(d, bytes.Repeat([]byte{9}, store.KeyBytes))
	if err != nil {
		t.Fatal(err)
	}
	h2 := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: other, Sections: []Section{testSection}}})
	body := page(h2)
	if !strings.Contains(body, stateUnreadable) || !strings.Contains(body, ">Clear<") {
		t.Fatalf("an unreadable secret should be marked and clearable:\n%s", body)
	}
	post(h2, "/secrets/mail/password", true, "new")
	if got, err := other.Secret(ctx, mailPassword); err != nil || got != "new" {
		t.Fatalf("replace unreadable = %q, %v", got, err)
	}
}

func TestSavingWithoutAKeyShowsWhyAndStoresNothing(t *testing.T) {
	h, s := integrationsHandler(t, nil)
	for _, htmx := range []bool{false, true} {
		rec := post(h, "/secrets/mail/password", htmx, "x")
		if want := map[bool]int{false: http.StatusUnprocessableEntity, true: http.StatusOK}[htmx]; rec.Code != want {
			t.Errorf("htmx=%v: status %d, want %d", htmx, rec.Code, want)
		}
		if !strings.Contains(rec.Body.String(), "encryption key is missing") || !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Errorf("htmx=%v: no key message:\n%s", htmx, rec.Body)
		}
	}
	if st, _ := s.SecretState(context.Background(), mailPassword); st != store.StateUnset {
		t.Fatalf("state = %v after a refused save", st)
	}
}

func TestEmptyValueIsRefused(t *testing.T) {
	h, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	rec := post(h, "/secrets/mail/password", true, "")
	if !strings.Contains(rec.Body.String(), "Enter a value first.") {
		t.Fatalf("no message:\n%s", rec.Body)
	}
}

func TestUnregisteredFieldIs404(t *testing.T) {
	h, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	for _, target := range []string{"/secrets/mail/nope", "/secrets/nope/password", "/secrets/mail/nope/clear"} {
		if rec := post(h, target, false, "x"); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", target, rec.Code)
		}
	}
}

// failingStore cannot read a secret's state.
type failingStore struct{ SecretStore }

func (failingStore) SecretState(context.Context, store.Field) (store.State, error) {
	return store.StateUnset, errors.New("database is locked")
}

// A failed read is not an unset secret: the page answers 500 and never shows
// "Not set", which would invite overwriting a secret that may be stored.
func TestFailedStateReadIsNotShownAsNotSet(t *testing.T) {
	_, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: failingStore{s}, Sections: []Section{testSection}}})
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"page":         serve(h, http.MethodGet, "/", false),
		"htmx save":    post(h, "/secrets/mail/password", true, "x"),
		"refused save": post(h, "/secrets/mail/password", false, ""),
	} {
		if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusSeeOther {
			t.Errorf("%s: status %d, want 500", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Not set") || strings.Contains(rec.Body.String(), "database is locked") {
			t.Errorf("%s: shows a default or the raw error:\n%s", name, rec.Body)
		}
	}
	if rec := serve(h, http.MethodGet, "/", false); rec.Code != http.StatusInternalServerError {
		t.Errorf("page status %d, want 500", rec.Code)
	}
}
