package household

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	domain "github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The household keys section (ADR-00031) behind the identity middleware and a
// membership middleware like the router's.

type site struct {
	h    http.Handler
	hs   *domain.Store
	keys *apikey.Store
}

func newSite(t *testing.T) *site {
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
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	hs, keys := domain.New(d), apikey.New(d)
	hs.OnMemberLeave(keys.RevokeForMember)
	r := chi.NewRouter()
	r.Use(a.Middleware(layout.IsPublic))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			email, _ := auth.EmailFromContext(r.Context())
			hh, err := hs.ForEmail(r.Context(), email)
			if err != nil {
				http.Error(w, "not a member", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(domain.WithContext(r.Context(), hh)))
		})
	})
	r.Mount(Pattern, MemberRoutes(Deps{Households: hs, Keys: keys}))
	return &site{h: r, hs: hs, keys: keys}
}

func (s *site) do(method, target, email string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

// household makes a household of the given members.
func (s *site) household(t *testing.T, name string, members ...string) {
	t.Helper()
	ctx := context.Background()
	h, err := s.hs.Create(ctx, name, members[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members[1:] {
		code, err := s.hs.Invite(ctx, domain.Invite{Kind: domain.KindJoin, Email: m, HouseholdID: h.ID, IssuedBy: members[0]})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.hs.Accept(ctx, code, m, ""); err != nil {
			t.Fatal(err)
		}
	}
}

var secretOnPage = regexp.MustCompile(`value="(gob_[A-Za-z0-9_-]+)"`)
var revokeForm = regexp.MustCompile(`action="(/household/keys/\d+/revoke)"`)

func (s *site) makeKey(t *testing.T, email, name, lifetime string) string {
	t.Helper()
	rec := s.do(http.MethodPost, "/household/keys", email, url.Values{"name": {name}, "lifetime": {lifetime}})
	m := secretOnPage.FindStringSubmatch(rec.Body.String())
	if rec.Code != http.StatusOK || m == nil {
		t.Fatalf("creating key %q = %d:\n%s", name, rec.Code, rec.Body)
	}
	return m[1]
}

func TestAnyMemberMakesAHouseholdKeyWhoseSecretIsShownOnce(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com")

	rec := s.do(http.MethodPost, "/household/keys", "abe@example.com", url.Values{"name": {"Kitchen bot"}, "lifetime": {"1y"}})
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	m := secretOnPage.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("no secret in the creating response:\n%s", rec.Body)
	}
	for _, who := range []string{"ann@example.com", "abe@example.com"} {
		body := s.do(http.MethodGet, "/household", who, nil).Body.String()
		if strings.Contains(body, m[1]) {
			t.Errorf("the household page shows the secret to %s", who)
		}
		for _, want := range []string{"Kitchen bot", m[1][:12], "made by abe@example.com", "Expires ", "Never used"} {
			if !strings.Contains(body, want) {
				t.Errorf("household page for %s missing %q", who, want)
			}
		}
	}
}

func TestHouseholdKeyCanHaveNoExpiryAndRefusesBadInput(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	s.makeKey(t, "ann@example.com", "forever", "never")
	if body := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String(); !strings.Contains(body, "No expiry") {
		t.Errorf("a key with no expiry is not listed as one")
	}
	for name, form := range map[string]url.Values{
		"blank name":       {"name": {""}, "lifetime": {"90d"}},
		"unknown lifetime": {"name": {"x"}, "lifetime": {"soon"}},
	} {
		if rec := s.do(http.MethodPost, "/household/keys", "ann@example.com", form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, rec.Code)
		}
	}
}

func TestAnyMemberRevokesAHouseholdKey(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com")
	s.makeKey(t, "abe@example.com", "doomed", "30d")
	path := revokeForm.FindStringSubmatch(s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String())
	if path == nil {
		t.Fatal("no revoke form")
	}
	if rec := s.do(http.MethodPost, path[1], "ann@example.com", nil); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/household" {
		t.Fatalf("revoke = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if body := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String(); strings.Contains(body, "doomed") || !strings.Contains(body, "This household has no keys.") {
		t.Errorf("revoked key still listed:\n%s", body)
	}
	if rec := s.do(http.MethodPost, "/household/keys/xyz/revoke", "ann@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoke of key xyz = %d, want 404", rec.Code)
	}
}

// A member of another household sees none of the household's keys and cannot
// revoke one; a non-member is refused by the membership check.
func TestAnotherHouseholdCannotSeeOrRevokeTheKeys(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	s.household(t, "Beta", "bob@example.com")
	s.makeKey(t, "ann@example.com", "alpha only", "30d")
	path := revokeForm.FindStringSubmatch(s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String())[1]

	if body := s.do(http.MethodGet, "/household", "bob@example.com", nil).Body.String(); strings.Contains(body, "alpha only") {
		t.Error("Beta sees Alpha's key")
	}
	if rec := s.do(http.MethodPost, path, "bob@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("Beta revoking Alpha's key = %d, want 404", rec.Code)
	}
	if rec := s.do(http.MethodPost, path, "nobody@example.com", nil); rec.Code != http.StatusForbidden {
		t.Errorf("a non-member revoking = %d, want 403", rec.Code)
	}
	if body := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String(); !strings.Contains(body, "alpha only") {
		t.Error("Alpha's key did not survive")
	}
	// A key Beta makes is Beta's.
	s.makeKey(t, "bob@example.com", "beta only", "30d")
	if body := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String(); strings.Contains(body, "beta only") {
		t.Error("Alpha sees Beta's key")
	}
}

// Through the page: a member removed by another loses the household keys they
// made, and leaving does the same (ADR-00031 obligation 3).
func TestRemovingOrLeavingThroughThePageRevokesTheKeys(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com", "ada@example.com")
	s.makeKey(t, "abe@example.com", "abe made", "never")
	s.makeKey(t, "ada@example.com", "ada made", "never")
	s.makeKey(t, "ann@example.com", "ann made", "never")

	if rec := s.do(http.MethodPost, "/household/members/remove", "ann@example.com", url.Values{"email": {"abe@example.com"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove = %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, "/household/leave", "ada@example.com", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("leave = %d", rec.Code)
	}
	body := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String()
	if strings.Contains(body, "abe made") || strings.Contains(body, "ada made") || !strings.Contains(body, "ann made") {
		t.Errorf("after removal and leaving, the household keys are:\n%s", body)
	}
}

// ADR-00031 obligation 6.
func TestMakingAHouseholdKeyLogsNoSecret(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	secret := s.makeKey(t, "ann@example.com", "logged?", "never")
	s.do(http.MethodPost, "/household/keys", "ann@example.com", url.Values{"name": {""}, "lifetime": {"never"}})
	s.do(http.MethodPost, "/household/keys/999/revoke", "ann@example.com", nil)
	s.do(http.MethodPost, "/household/keys", "nobody@example.com", url.Values{"name": {"x"}, "lifetime": {"never"}})

	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), secret[4:]) {
		t.Errorf("a log line contains the secret:\n%s", logs.String())
	}
	if logs.Len() == 0 {
		t.Error("nothing was logged, so the check proves nothing")
	}
}
