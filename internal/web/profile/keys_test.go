package profile

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The personal keys section (ADR-00031).

var secretOnPage = regexp.MustCompile(`value="(gob_[A-Za-z0-9_-]+)"`)

// ownPath returns the profile path of email.
func ownPath(t *testing.T, s *site, email string) string {
	t.Helper()
	rec := s.do(http.MethodGet, "/profile", email, nil)
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /profile = %d", rec.Code)
	}
	return loc
}

// makeKey creates a key and returns its secret as the page showed it.
func makeKey(t *testing.T, s *site, email, path, name, lifetime string) string {
	t.Helper()
	rec := s.do(http.MethodPost, path+"/keys", email, url.Values{"name": {name}, "lifetime": {lifetime}})
	m := secretOnPage.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		t.Fatalf("creating key %q = %d:\n%s", name, rec.Code, rec.Body)
	}
	return m[1]
}

// ADR-00031 obligation 1: the secret is in the response that created it and on
// no later page.
func TestPersonalKeySecretIsShownOnceAndNeverAgain(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")

	rec := s.do(http.MethodPost, path+"/keys", "ann@example.com", url.Values{"name": {"Laptop <agent>"}, "lifetime": {"90d"}})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create = %d, Cache-Control %q; want 200 and no-store", rec.Code, rec.Header().Get("Cache-Control"))
	}
	m := secretOnPage.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("the creating response has no secret:\n%s", rec.Body)
	}
	secret := m[1]
	if !strings.Contains(rec.Body.String(), "Laptop &lt;agent&gt;") {
		t.Errorf("the name is not escaped on the secret page")
	}

	for _, target := range []string{path, path + "/keys"} {
		body := s.do(http.MethodGet, target, "ann@example.com", nil).Body.String()
		if strings.Contains(body, secret) {
			t.Errorf("GET %s shows the secret again", target)
		}
	}
	list := s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String()
	for _, want := range []string{"Laptop &lt;agent&gt;", secret[:12], "Expires ", "Never used"} {
		if !strings.Contains(list, want) {
			t.Errorf("keys page missing %q:\n%s", want, list)
		}
	}
}

func TestPersonalKeyCanHaveNoExpiryAndRefusesBadInput(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")
	makeKey(t, s, "ann@example.com", path, "forever", "never")
	if body := s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String(); !strings.Contains(body, "No expiry") {
		t.Errorf("a key with no expiry is not listed as one:\n%s", body)
	}
	for name, form := range map[string]url.Values{
		"blank name":       {"name": {"  "}, "lifetime": {"90d"}},
		"unknown lifetime": {"name": {"x"}, "lifetime": {"always"}},
		"no lifetime":      {"name": {"x"}},
		"long name":        {"name": {strings.Repeat("x", 61)}, "lifetime": {"90d"}},
	} {
		if rec := s.do(http.MethodPost, path+"/keys", "ann@example.com", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, rec.Code)
		}
	}
}

func TestPersonalKeyIsRevokedFromItsPage(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")
	makeKey(t, s, "ann@example.com", path, "doomed", "30d")
	revoke := regexp.MustCompile(`action="(` + regexp.QuoteMeta(path) + `/keys/\d+/revoke)"`).FindStringSubmatch(
		s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String())
	if revoke == nil {
		t.Fatal("no revoke form on the keys page")
	}
	if rec := s.do(http.MethodPost, revoke[1], "ann@example.com", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/keys" {
		t.Fatalf("revoke = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if body := s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String(); strings.Contains(body, "doomed") {
		t.Errorf("a revoked key is still listed:\n%s", body)
	}
	if rec := s.do(http.MethodPost, revoke[1], "ann@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoking twice = %d, want 404", rec.Code)
	}
}

// ADR-00032 and ADR-00031: another person, member or not, finds nothing on
// your keys routes and cannot revoke your key.
func TestAnotherPersonCannotSeeOrRevokeYourKeys(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")
	makeKey(t, s, "ann@example.com", path, "ann private", "30d")
	idPath := regexp.MustCompile(`action="(/profile/[^"]+/keys/\d+/revoke)"`).FindStringSubmatch(
		s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String())[1]

	for _, who := range []string{"bob@example.com", "stranger@example.com"} {
		if rec := s.do(http.MethodGet, path+"/keys", who, nil); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "ann private") {
			t.Errorf("%s reading ann's keys = %d", who, rec.Code)
		}
		if rec := s.do(http.MethodPost, path+"/keys", who, url.Values{"name": {"x"}, "lifetime": {"30d"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s making a key on ann's profile = %d, want 404", who, rec.Code)
		}
		if rec := s.do(http.MethodPost, idPath, who, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s revoking ann's key = %d, want 404", who, rec.Code)
		}
		// Even through their own profile address the key is not theirs.
		own := ownPath(t, s, who)
		if rec := s.do(http.MethodPost, own+idPath[strings.Index(idPath, "/keys"):], who, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s revoking ann's key through their own profile = %d, want 404", who, rec.Code)
		}
		if body := s.do(http.MethodGet, own+"/keys", who, nil).Body.String(); strings.Contains(body, "ann private") {
			t.Errorf("%s sees ann's key on their own page", who)
		}
	}
	if body := s.do(http.MethodGet, path+"/keys", "ann@example.com", nil).Body.String(); !strings.Contains(body, "ann private") {
		t.Error("ann's key did not survive the attempts")
	}
	// Another person's profile page does not advertise the section.
	if body := s.do(http.MethodGet, path, "bob@example.com", nil).Body.String(); strings.Contains(body, "API keys") {
		t.Error("another person's view of the profile shows the keys section")
	}
}

func TestKeyRoutesRefuseANonNumericID(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")
	if rec := s.do(http.MethodPost, path+"/keys/abc/revoke", "ann@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoke of key abc = %d, want 404", rec.Code)
	}
}

// ADR-00031 obligation 6: making a key, and failing to, log no secret.
func TestMakingAKeyLogsNoSecret(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := newSite(t, fakeHouseholds{})
	path := ownPath(t, s, "ann@example.com")
	secret := makeKey(t, s, "ann@example.com", path, "logged?", "never")
	s.do(http.MethodPost, path+"/keys", "ann@example.com", url.Values{"name": {""}, "lifetime": {"never"}})
	s.do(http.MethodPost, path+"/keys/999/revoke", "ann@example.com", nil)
	s.do(http.MethodPost, path+"/keys", "bob@example.com", url.Values{"name": {"x"}, "lifetime": {"never"}})

	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), secret[4:]) {
		t.Errorf("a log line contains the secret:\n%s", logs.String())
	}
	if logs.Len() == 0 {
		t.Error("nothing was logged, so the check proves nothing")
	}
}
