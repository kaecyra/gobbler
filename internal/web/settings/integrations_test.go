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
// section of their own over a real store and database: one secret and no
// plain fields, as YouTube has.
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

// postForm posts a form of name, value pairs to target.
func postForm(h http.Handler, target string, htmx bool, kv ...string) *httptest.ResponseRecorder {
	form := url.Values{}
	for i := 0; i < len(kv); i += 2 {
		form.Set(kv[i], kv[i+1])
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

// The secret is a field of its section's one form: a typed value replaces
// what is stored, a blank one keeps it, and Clear is its own post.
func TestSecretFieldEnterKeepReplaceClear(t *testing.T) {
	ctx := context.Background()
	h, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))

	body := page(h)
	if !strings.Contains(body, `data-testid="secret-state">Not set<`) || strings.Contains(body, ">Clear<") || strings.Contains(body, savedPlaceholder) {
		t.Fatalf("an unset secret should offer no Clear and no dots:\n%s", body)
	}
	if !strings.Contains(body, `data-testid="values-mail"`) || !strings.Contains(body, `name="password"`) {
		t.Fatalf("a secrets-only section should still have its form, with the secret in it:\n%s", body)
	}

	if rec := postForm(h, "/values/mail", false, "password", "hunter2"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("enter: %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, err := s.Secret(ctx, mailPassword); err != nil || got != "hunter2" {
		t.Fatalf("stored = %q, %v", got, err)
	}
	body = page(h)
	for _, want := range []string{" Saved</span>", `placeholder="` + savedPlaceholder + `"`, `form="clear-mail-password"`, `<form id="clear-mail-password"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("a saved secret should show %s:\n%s", want, body)
		}
	}
	// The Clear button is in the section's form; the form it submits is not,
	// since a form cannot hold another.
	values := body[strings.Index(body, `data-testid="values-mail"`):]
	values = values[:strings.Index(values, "</form>")]
	if !strings.Contains(values, `form="clear-mail-password"`) || strings.Contains(values, "<form") {
		t.Errorf("the Clear button should sit in the section's form, and no form inside it:\n%s", values)
	}

	postForm(h, "/values/mail", true, "password", "")
	if got, _ := s.Secret(ctx, mailPassword); got != "hunter2" {
		t.Fatalf("a blank submit changed the secret to %q", got)
	}
	postForm(h, "/values/mail", true, "password", "second")
	if got, _ := s.Secret(ctx, mailPassword); got != "second" {
		t.Fatalf("after replace = %q", got)
	}
	rec := postForm(h, "/secrets/mail/password/clear", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Not set") {
		t.Fatalf("clear via htmx: %d %s", rec.Code, rec.Body)
	}
	if st, _ := s.SecretState(ctx, mailPassword); st != store.StateUnset {
		t.Fatalf("after clear state = %v", st)
	}
}

// ADR-00057 obligation 5: no response from the page contains a saved value.
// The refused submits are covered per section: mail_test.go and llm_test.go.
func TestNoResponseContainsASavedSecret(t *testing.T) {
	const secret = "s3cr3t-Value-0123456789"
	h, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	responses := map[string]*httptest.ResponseRecorder{
		"save, no script":   postForm(h, "/values/mail", false, "password", secret),
		"save, htmx":        postForm(h, "/values/mail", true, "password", secret),
		"page":              serve(h, http.MethodGet, "/", false),
		"fragment (rename)": postForm(h, "/values/mail", true, "password", secret+"2"),
		"blank submit":      postForm(h, "/values/mail", true, "password", ""),
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
	if !strings.Contains(body, stateUnreadable) || !strings.Contains(body, ">Clear<") || strings.Contains(body, savedPlaceholder) {
		t.Fatalf("an unreadable secret should be marked and clearable, with no dots:\n%s", body)
	}
	postForm(h2, "/values/mail", true, "password", "new")
	if got, err := other.Secret(ctx, mailPassword); err != nil || got != "new" {
		t.Fatalf("replace unreadable = %q, %v", got, err)
	}
}

func TestSavingWithoutAKeyShowsWhyAndStoresNothing(t *testing.T) {
	h, s := integrationsHandler(t, nil)
	for _, htmx := range []bool{false, true} {
		rec := postForm(h, "/values/mail", htmx, "password", "x-typed-secret")
		if want := map[bool]int{false: http.StatusUnprocessableEntity, true: http.StatusOK}[htmx]; rec.Code != want {
			t.Errorf("htmx=%v: status %d, want %d", htmx, rec.Code, want)
		}
		if !strings.Contains(rec.Body.String(), "Password was not saved: the encryption key is missing") || !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Errorf("htmx=%v: no key message:\n%s", htmx, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "x-typed-secret") {
			t.Errorf("htmx=%v: the refusal echoes the typed secret", htmx)
		}
	}
	if st, _ := s.SecretState(context.Background(), mailPassword); st != store.StateUnset {
		t.Fatalf("state = %v after a refused save", st)
	}
}

func TestATooLongSecretIsRefused(t *testing.T) {
	h, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	rec := postForm(h, "/values/mail", true, "password", strings.Repeat("x", store.MaxValueBytes+1))
	if !strings.Contains(rec.Body.String(), "Password is too long.") {
		t.Fatalf("no message:\n%s", rec.Body)
	}
	if st, _ := s.SecretState(context.Background(), mailPassword); st != store.StateUnset {
		t.Fatalf("state = %v after a refused save", st)
	}
}

func TestUnregisteredTargetIs404(t *testing.T) {
	h, _ := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	// The last is the per-secret save route, which is gone: a secret saves
	// with its section.
	for _, target := range []string{"/values/nope", "/secrets/mail/nope/clear", "/secrets/nope/password/clear", "/secrets/mail/password"} {
		if rec := postForm(h, target, false, "password", "x"); rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", target, rec.Code)
		}
	}
}

// A secret placed after a field its section does not have is a wiring
// mistake: the page fails rather than drop the secret or guess a place.
func TestASecretAfterAnUnknownFieldFailsTheRender(t *testing.T) {
	_, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	bad := Section{Integration: "mail", Title: "Mail", Secrets: []SecretField{{Name: "password", Label: "Password", After: "nope"}}}
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{bad}}})
	if rec := serve(h, http.MethodGet, "/", false); rec.Code != http.StatusInternalServerError {
		t.Errorf("= %d, want 500", rec.Code)
	}
}

// failingStore cannot read a secret's state.
type failingStore struct{ IntegrationStore }

func (failingStore) SecretState(context.Context, store.Field) (store.State, error) {
	return store.StateUnset, errors.New("database is locked")
}

// A failed read is not an unset secret: the page answers 500 and never shows
// "Not set", which would invite overwriting a secret that may be stored.
func TestFailedStateReadIsNotShownAsNotSet(t *testing.T) {
	_, s := integrationsHandler(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	h := Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: failingStore{s}, Sections: []Section{testSection}}})
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"page":      serve(h, http.MethodGet, "/", false),
		"htmx save": postForm(h, "/values/mail", true, "password", "x"),
	} {
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Not set") || strings.Contains(rec.Body.String(), "database is locked") {
			t.Errorf("%s: shows a default or the raw error:\n%s", name, rec.Body)
		}
	}
}
