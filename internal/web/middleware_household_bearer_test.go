package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/signin"
)

// The MCP endpoint's admission (ADR-00031, ADR-00034): a bearer credential and
// the membership check of its owner, never the session.

const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// bearerSite is the production router (no bypass, sign-in configured) with the
// settings the MCP endpoint needs.
type bearerSite struct {
	r        *chi.Mux
	db       *sql.DB
	sessions *session.Store
}

func newBearerSite(t *testing.T) *bearerSite {
	t.Helper()
	s := &bearerSite{db: testDB(t)}
	cfg := prodConfig()
	cfg.Ingestion = config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "test"}
	cfg.MCP = config.MCP{Rate: 100, Burst: 100}
	var err error
	if s.sessions, err = session.New(s.db, time.Hour); err != nil {
		t.Fatal(err)
	}
	a, err := auth.New(cfg, auth.WithSessions(s.sessions, layout.SignInPath), auth.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	si, err := signin.New(signin.Settings{Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, PublicBaseURL: cfg.OIDC.PublicBaseURL, Lifetime: time.Hour}, s.sessions)
	if err != nil {
		t.Fatal(err)
	}
	s.r, err = newRouter(Deps{Config: cfg, DB: s.db, Avatars: testAvatars(t, s.db), Auth: a, SignIn: si, Sessions: s.sessions, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *bearerSite) do(method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	s.r.ServeHTTP(rec, r)
	return rec
}

func (s *bearerSite) mcp(secret string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initialize))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	s.r.ServeHTTP(rec, r)
	return rec
}

func (s *bearerSite) member(t *testing.T, name, email string) household.Household {
	t.Helper()
	h, err := household.New(s.db).Create(context.Background(), name, email)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestMCPWithoutAValidKeyIs401NotARedirect(t *testing.T) {
	s := newBearerSite(t)
	keys := apikey.New(s.db)
	s.member(t, "Smiths", "ann@example.com")
	expired, err := keys.CreatePersonal(context.Background(), "ann@example.com", "old", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	for name, secret := range map[string]string{"missing": "", "unknown": "gob_unknown", "expired": expired} {
		rec := s.mcp(secret, nil)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != metadataChallenge || rec.Header().Get("Location") != "" {
			t.Errorf("%s: = %d, WWW-Authenticate %q, Location %q; want 401 %s and no redirect", name, rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Header().Get("Location"), metadataChallenge)
		}
	}
	// A GET, which the session layer would redirect to sign-in, is the same.
	rec := s.do(http.MethodGet, "/mcp", nil)
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Location") != "" {
		t.Errorf("GET /mcp = %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestASessionCookieAloneNeverAuthenticatesMCP(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	token, err := s.sessions.Create(context.Background(), "ann@example.com", session.Client{})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: auth.SessionCookie, Value: token}
	// The cookie is good: it opens an HTML page of the same member.
	if rec := s.do(http.MethodGet, "/recipes", cookie); rec.Code != http.StatusOK {
		t.Fatalf("control: GET /recipes with the cookie = %d, want 200", rec.Code)
	}
	if rec := s.mcp("", cookie); rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") != metadataChallenge {
		t.Errorf("POST /mcp with a session cookie = %d (%q), want 401 %s", rec.Code, rec.Header().Get("WWW-Authenticate"), metadataChallenge)
	}
}

func TestAKeyDoesNotAuthenticateAnHTMLPage(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	secret, err := apikey.New(s.db).CreatePersonal(context.Background(), "ann@example.com", "k", 0)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/recipes", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	s.r.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("GET /recipes with only a key = %d %q, want the sign-in redirect", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAKeyWhoseOwnerIsNotAMemberIsRefused(t *testing.T) {
	s := newBearerSite(t)
	s.member(t, "Smiths", "ann@example.com")
	// The key exists and is live, but its owner belongs to no household.
	secret, err := apikey.New(s.db).CreatePersonal(context.Background(), "nobody@example.com", "k", 0)
	if err != nil {
		t.Fatal(err)
	}
	rec := s.mcp(secret, nil)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "not-a-member") {
		t.Errorf("= %d %q, want a plain 403", rec.Code, rec.Body.String())
	}
}

func TestMembersAndHouseholdsKeysAreAdmitted(t *testing.T) {
	s := newBearerSite(t)
	ctx := context.Background()
	h := s.member(t, "Smiths", "ann@example.com")
	keys := apikey.New(s.db)
	personal, _ := keys.CreatePersonal(ctx, "ann@example.com", "p", 0)
	shared, err := keys.CreateHousehold(ctx, h.ID, "ann@example.com", "h", 0)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]string{"personal": personal, "household": shared} {
		if rec := s.mcp(secret, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"serverInfo"`) {
			t.Errorf("%s key: initialize = %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestRevokingAMemberRefusesTheirKeyAtOnce(t *testing.T) {
	s := newBearerSite(t)
	ctx := context.Background()
	h := s.member(t, "Smiths", "ann@example.com")
	hs := household.New(s.db)
	hs.OnMemberLeave(apikey.New(s.db).RevokeForMember)
	code, err := hs.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: "bob@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Accept(ctx, code, "bob@example.com", ""); err != nil {
		t.Fatal(err)
	}
	secret, _ := apikey.New(s.db).CreatePersonal(ctx, "bob@example.com", "p", 0)
	if rec := s.mcp(secret, nil); rec.Code != http.StatusOK {
		t.Fatalf("before: %d", rec.Code)
	}
	if err := hs.Remove(ctx, h.ID, "ann@example.com", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if rec := s.mcp(secret, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after removal = %d, want 401", rec.Code)
	}
}

// /mcp reads household data, so it is never a public path (ADR-00029); it is
// the bearer endpoint, which Admits to a member alone like any private page.
func TestMCPIsABearerPathAndNotAPublicOne(t *testing.T) {
	for _, p := range []string{"/mcp", "/mcp/", "/mcp/x"} {
		if layout.IsPublic(p) || !layout.IsBearerPath(p) || layout.Admits(p, false, false) || !layout.Admits(p, true, false) {
			t.Errorf("%s: public=%v bearer=%v admits non-member=%v member=%v", p, layout.IsPublic(p), layout.IsBearerPath(p), layout.Admits(p, false, false), layout.Admits(p, true, false))
		}
	}
	for _, p := range []string{"/mcpx", "/m", "/recipes/mcp"} {
		if layout.IsBearerPath(p) {
			t.Errorf("%s is a bearer path", p)
		}
	}
}

// A draft an agent makes over MCP waits on the ingest review screen for the
// members of its household, and for nobody else (ADR-00006, ADR-00018,
// ADR-00034). The router shares one pending table between the two.
func TestAnMCPDraftOpensOnTheReviewScreenOfItsHouseholdOnly(t *testing.T) {
	cfg := devConfig()
	cfg.Ingestion = config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "test"}
	cfg.MCP = config.MCP{Rate: 100, Burst: 100}
	d := testDB(t)
	r := testRouter(t, cfg, Deps{DB: d})
	do(r, http.MethodGet, "/healthz", auth.DevEmail)
	// The dev households are seeded by the first request that needs them.
	if rec := do(r, http.MethodGet, "/", auth.DevEmail); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d", rec.Code)
	}
	secret, err := apikey.New(d).CreatePersonal(context.Background(), auth.DevEmail, "agent", 0)
	if err != nil {
		t.Fatal(err)
	}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_recipe_draft","arguments":{"name":"Agent flour toast","ingredients":["2 cups flour"],"steps":["Mix well."]}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(call))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out struct {
		Result struct {
			StructuredContent struct {
				ReviewPath string `json:"review_path"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !strings.HasPrefix(out.Result.StructuredContent.ReviewPath, "/ingest/") {
		t.Fatalf("create_recipe_draft = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	path := out.Result.StructuredContent.ReviewPath
	if rec := do(r, http.MethodGet, path, auth.DevEmail); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Agent flour toast") {
		t.Errorf("the draft's own household: GET %s = %d", path, rec.Code)
	}
	if rec := do(r, http.MethodGet, path, auth.DevOtherEmail); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "Agent flour toast") {
		t.Errorf("another household: GET %s = %d, want 404 without the draft", path, rec.Code)
	}
}
