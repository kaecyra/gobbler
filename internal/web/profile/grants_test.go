package profile

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// grantSite is newSite with its database exposed, which the grants tests need
// to make a grant. newSite is in a file this node does not own, and keeps the
// database to itself.
type grantSite struct {
	*site
	db *sql.DB
}

func newGrantSite(t *testing.T) *grantSite {
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
	r := chi.NewRouter()
	r.Use(a.Middleware(layout.IsPublic))
	r.Mount(Pattern, Routes(Deps{Profile: newService(t, d, fakeHouseholds{}, nil, nil)}))
	return &grantSite{site: &site{h: r}, db: d}
}

// The connected apps section (ADR-00034).

var grantRevokePath = regexp.MustCompile(`action="([^"]+/grants/\d+/revoke)"`)

// connect makes a grant for the person, as a client's token exchange does, and
// returns its access token.
func connect(t *testing.T, s *grantSite, email, name string) string {
	t.Helper()
	ctx := context.Background()
	st := oauth.New(s.db)
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	code, err := st.IssueCode(ctx, oauth.AuthRequest{
		Owner: email, ClientID: "gobc_x", ClientName: name, RedirectURI: "https://app.example/cb",
		CodeChallenge: oauth.Challenge(verifier), Resource: "https://gobbler.test/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	toks, err := st.ExchangeCode(ctx, oauth.ExchangeRequest{
		Code: code, ClientID: "gobc_x", RedirectURI: "https://app.example/cb", Verifier: verifier, Resource: "https://gobbler.test/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	return toks.Access
}

func TestGrantsAreListedOnTheProfileAndRevoked(t *testing.T) {
	s := newGrantSite(t)
	path := ownPath(t, s.site, "ann@example.com")
	if body := s.do(http.MethodGet, path, "ann@example.com", nil).Body.String(); !strings.Contains(body, `href="`+path+`/grants"`) {
		t.Fatalf("the profile page does not link to the connected apps:\n%s", body)
	}
	page := s.do(http.MethodGet, path+"/grants", "ann@example.com", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "No apps are connected.") {
		t.Fatalf("empty list = %d:\n%s", page.Code, page.Body)
	}

	access := connect(t, s, "ann@example.com", "Kitchen <agent>")
	body := s.do(http.MethodGet, path+"/grants", "ann@example.com", nil).Body.String()
	for _, want := range []string{"Kitchen &lt;agent&gt;", "app.example", "Revoke access for Kitchen &lt;agent&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("grants page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, access) {
		t.Error("the page shows a token")
	}
	m := grantRevokePath.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no revoke form:\n%s", body)
	}

	rec := s.do(http.MethodPost, m[1], "ann@example.com", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path+"/grants" {
		t.Fatalf("revoke = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, err := oauth.New(s.db).Verify(context.Background(), access, "https://gobbler.test/mcp"); err == nil {
		t.Fatal("the revoked grant's token still verifies")
	}
	if body := s.do(http.MethodGet, path+"/grants", "ann@example.com", nil).Body.String(); !strings.Contains(body, "No apps are connected.") {
		t.Errorf("the grant is still listed:\n%s", body)
	}
}

func TestGrantsAreSelfOnly(t *testing.T) {
	s := newGrantSite(t)
	annPath := ownPath(t, s.site, "ann@example.com")
	bobPath := ownPath(t, s.site, "bob@example.com")
	access := connect(t, s, "ann@example.com", "Ann's agent")
	m := grantRevokePath.FindStringSubmatch(s.do(http.MethodGet, annPath+"/grants", "ann@example.com", nil).Body.String())

	// Another person finds nothing at Ann's address.
	if rec := s.do(http.MethodGet, annPath+"/grants", "bob@example.com", nil); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Ann's agent") {
		t.Errorf("Bob reading Ann's grants = %d", rec.Code)
	}
	if rec := s.do(http.MethodPost, m[1], "bob@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("Bob revoking through Ann's address = %d", rec.Code)
	}
	// Through his own address the self check passes, so the owner refuses it.
	other := strings.Replace(m[1], annPath, bobPath, 1)
	if rec := s.do(http.MethodPost, other, "bob@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("Bob revoking Ann's grant through his own address = %d", rec.Code)
	}
	if _, err := oauth.New(s.db).Verify(context.Background(), access, "https://gobbler.test/mcp"); err != nil {
		t.Errorf("Ann's grant did not survive: %v", err)
	}
	// A section of someone else's profile does not show on their page either.
	if body := s.do(http.MethodGet, annPath, "bob@example.com", nil).Body.String(); strings.Contains(body, "Connected apps") {
		t.Error("Ann's connected apps section shows to Bob")
	}
	if rec := s.do(http.MethodPost, annPath+"/grants/abc/revoke", "ann@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoking a grant id that is not a number = %d", rec.Code)
	}
}
