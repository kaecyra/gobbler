package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/operator"
	"github.com/kaecyra/gobbler/internal/web/settings"
)

const (
	operatorEmail  = "op@example.com"
	strangerEmail  = "stranger@example.com"
	notAMemberText = "You are not a member of a household"
)

type idleBackup struct{}

func (idleBackup) Status() backup.Status                      { return backup.Status{} }
func (idleBackup) Run(context.Context) (backup.Result, error) { return backup.Result{}, nil }

// tenantRouter is a dev-bypass router with the seeded dev households, an
// operator who belongs to no household, and a probe route that reports the
// household the request carries.
func tenantRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := devConfig()
	cfg.Operator = config.Operator{Emails: []string{operatorEmail, auth.DevOtherEmail}}
	r := testRouter(t, cfg, Deps{Backup: idleBackup{}})
	r.Get("/probe", func(w http.ResponseWriter, req *http.Request) {
		h, ok := household.FromContext(req.Context())
		if !ok {
			http.Error(w, "no household on the request", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(h.Name))
	})
	return r
}

func do(h http.Handler, method, path, email string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMemberRequestCarriesOnlyItsOwnHousehold(t *testing.T) {
	h := tenantRouter(t)
	for email, want := range map[string]string{auth.DevEmail: "Dev household", auth.DevOtherEmail: "Other household"} {
		rec := do(h, http.MethodGet, "/probe", email)
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: /probe = %d %q, want 200 %q", email, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestNonMemberReachesOnlyTheAllowedRoutes(t *testing.T) {
	h := tenantRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/probe"},
		{http.MethodGet, "/recipes"},
		{http.MethodGet, "/shopping"},
		{http.MethodGet, "/catalog"},
		{http.MethodGet, "/nope"},
		{http.MethodPost, "/ingest"},
		{http.MethodGet, "/invitations"}, // the prefix is "/invitations/"
	} {
		rec := do(h, tc.method, tc.path, strangerEmail)
		body := rec.Body.String()
		if rec.Code != http.StatusForbidden || !strings.Contains(body, notAMemberText) {
			t.Errorf("%s %s = %d, want 403 with the not-a-member page", tc.method, tc.path, rec.Code)
		}
		for _, leak := range []string{"Dev household", "Other household"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s %s leaked %q", tc.method, tc.path, leak)
			}
		}
	}
}

func TestNonMemberCanOpenHealthThePageAssetsAndInvitations(t *testing.T) {
	h := tenantRouter(t)
	if rec := do(h, http.MethodGet, HealthPath, strangerEmail); rec.Code != http.StatusOK {
		t.Errorf("health = %d, want 200", rec.Code)
	}
	rec := do(h, http.MethodGet, "/not-a-member", strangerEmail)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), notAMemberText) || !strings.Contains(rec.Body.String(), strangerEmail) {
		t.Errorf("/not-a-member = %d, want 200 with the page and the email", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/static/app.css", strangerEmail); rec.Code != http.StatusOK {
		t.Errorf("stylesheet = %d, want 200", rec.Code)
	}
	// The lock does not turn the invitation path away: a missing invitation is
	// a 404 page, not the not-a-member page.
	rec = do(h, http.MethodGet, layout.InvitationsPath+"/Zz000000", strangerEmail)
	if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), notAMemberText) {
		t.Errorf("invitation path was refused as a non-member: %d", rec.Code)
	}
}

func TestMemberIsSentAwayFromTheNotAMemberPage(t *testing.T) {
	rec := do(tenantRouter(t), http.MethodGet, "/not-a-member", auth.DevEmail)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Errorf("= %d -> %q, want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSettingsIsOperatorOnly(t *testing.T) {
	h := tenantRouter(t)
	cases := []struct {
		name, email, method, path string
		want                      int
	}{
		{"non-operator member", auth.DevEmail, http.MethodGet, "/operator/settings", http.StatusForbidden},
		{"non-operator member, slash", auth.DevEmail, http.MethodGet, "/operator/settings/", http.StatusForbidden},
		{"non-operator member, action", auth.DevEmail, http.MethodPost, "/operator/settings/backup", http.StatusForbidden},
		{"operator who is a member", auth.DevOtherEmail, http.MethodGet, "/operator/settings", http.StatusOK},
		{"operator with no household", operatorEmail, http.MethodGet, "/operator/settings", http.StatusOK},
		{"operator with no household, action", operatorEmail, http.MethodPost, "/operator/settings/backup", http.StatusSeeOther},
		{"non-member non-operator", strangerEmail, http.MethodGet, "/operator/settings", http.StatusForbidden},
		// The rule is applied to the escaped path as well as the decoded one.
		{"non-operator member, encoded", auth.DevEmail, http.MethodGet, "/%6Fperator/settings", http.StatusForbidden},
		{"operator with no household, encoded", operatorEmail, http.MethodGet, "/%6Fperator", http.StatusForbidden},
		{"non-member, encoded not-a-member page", strangerEmail, http.MethodGet, "/not-a-m%65mber", http.StatusForbidden},
	}
	for _, c := range cases {
		rec := do(h, c.method, c.path, c.email)
		if rec.Code != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, rec.Code, c.want)
		}
	}
	if body := do(h, http.MethodGet, "/operator/settings", auth.DevEmail).Body.String(); !strings.Contains(body, "operator") || strings.Contains(body, "Backup") {
		t.Errorf("refusal of a member should name the operator and show no settings:\n%s", body)
	}
	// The refusal carries the member's household, so its nav is a member's.
	if body := do(h, http.MethodGet, "/operator/settings", auth.DevEmail).Body.String(); !strings.Contains(body, `href="/recipes"`) {
		t.Errorf("refusal of a member should keep the member nav:\n%s", body)
	}
}

func TestOperatorWithoutHouseholdIsOtherwiseANonMember(t *testing.T) {
	rec := do(tenantRouter(t), http.MethodGet, "/recipes", operatorEmail)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), notAMemberText) {
		t.Errorf("operator on /recipes = %d, want the not-a-member page", rec.Code)
	}
}

func TestMembershipFailsClosedWhenTheLookupFails(t *testing.T) {
	cfg := devConfig()
	d := testDB(t)
	r := testRouter(t, cfg, Deps{DB: d})
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	rec := do(r, http.MethodGet, "/", auth.DevEmail)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("= %d, want 500 rather than admitting or treating the person as a non-member", rec.Code)
	}
}

func TestMembershipRefusesARequestWithoutIdentity(t *testing.T) {
	var reached bool
	h := membership(nil, config.Operator{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/recipes", nil))
	if reached || rec.Code != http.StatusForbidden {
		t.Errorf("reached=%v code=%d, want a 403 that never reached the handler", reached, rec.Code)
	}
}

func TestDevHouseholdsAreNotSeededOutsideTheBypass(t *testing.T) {
	d := testDB(t)
	testRouter(t, prodConfig(), Deps{DB: d})
	if _, err := household.New(d).ForEmail(context.Background(), auth.DevEmail); err == nil {
		t.Fatal("a deployment with sign-in got the dev member")
	}
}

// dotSegmentPaths are spellings chi would dispatch into an /operator mount (it
// routes the raw path) but whose cleaned form names no operator page.
var dotSegmentPaths = []string{"/invitations/../recipes", "/static/../recipes", "/operator/probe/..", "/operator/probe/%2e%2e", "/operator/probe/%2E%2E", "/operator/probe/./5", "/operator/probe/%2e/5", "/operator/probe/.%2e", "/operator/probe/../../recipes", "/operator/../recipes"}

// probeRouter is tenantRouter plus an /operator/probe/{id} child, so a path that
// slips past the operator check is observable as a 200 instead of a 404.
func probeRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := devConfig()
	cfg.Operator = config.Operator{Emails: []string{operatorEmail}}
	r := testRouter(t, cfg, Deps{Backup: idleBackup{}})
	r.Get("/operator/probe/{id}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("operator child")) })
	return r
}

func TestDotSegmentsNeverReachAnOperatorChild(t *testing.T) {
	h := probeRouter(t)
	if rec := do(h, http.MethodGet, "/operator/probe/5", auth.DevEmail); rec.Code != http.StatusForbidden {
		t.Fatalf("control: member on /operator/probe/5 = %d, want 403", rec.Code)
	}
	for _, email := range []string{auth.DevEmail, strangerEmail, operatorEmail} {
		for _, p := range dotSegmentPaths {
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				rec := do(h, method, p, email)
				if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "operator child") {
					t.Errorf("%s %s %s = %d, want the 404 page", email, method, p, rec.Code)
				}
			}
		}
	}
}

const operatorLink = `href="/operator"`

func TestNavShowsOperatorToOperatorsOnly(t *testing.T) {
	h := tenantRouter(t)
	cases := []struct {
		name, email, path string
		want              bool
	}{
		{"non-operator member", auth.DevEmail, "/recipes", false},
		{"operator who is a member", auth.DevOtherEmail, "/recipes", true},
		{"operator with no household, on the operator page", operatorEmail, "/operator/settings", true},
	}
	for _, c := range cases {
		rec := do(h, http.MethodGet, c.path, c.email)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET %s = %d", c.name, c.path, rec.Code)
		}
		if got := strings.Contains(rec.Body.String(), operatorLink); got != c.want {
			t.Errorf("%s: nav has Operator link = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSettingsIsAnOperatorPath(t *testing.T) {
	if layout.Admits(settings.Pattern, true, false) {
		t.Errorf("layout.Admits opens the settings pattern %q to a member who is not an operator", settings.Pattern)
	}
}

// The deployment page moved under /operator; the old path sends a bookmark on.
// Only an operator is sent on, a household member or not.
func TestLegacySettingsRedirects(t *testing.T) {
	h := tenantRouter(t)
	for _, email := range []string{auth.DevOtherEmail, operatorEmail} {
		rec := do(h, http.MethodGet, settings.LegacyPattern, email)
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != settings.Pattern {
			t.Errorf("%s: = %d -> %q, want 301 to %s", email, rec.Code, rec.Header().Get("Location"), settings.Pattern)
		}
	}
	if rec := do(h, http.MethodGet, settings.LegacyPattern, auth.DevEmail); rec.Code != http.StatusForbidden {
		t.Errorf("non-operator member: = %d, want 403", rec.Code)
	}
}

// Every entry of the operator rail is a page an operator can open, and the
// overview is the operator's landing page.
func TestOperatorRailPagesAreServed(t *testing.T) {
	h := tenantRouter(t)
	for _, p := range layout.OperatorSectionPaths() {
		rec := do(h, http.MethodGet, p, operatorEmail)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-testid="operator-rail"`) {
			t.Errorf("GET %s as the operator = %d, want 200 with the operator rail", p, rec.Code)
		}
	}
}

func TestOperatorPagesAreOperatorPaths(t *testing.T) {
	if !slices.Contains(layout.OperatorPaths, operator.Pattern) {
		t.Errorf("layout.OperatorPaths %v lacks the operator pattern %q", layout.OperatorPaths, operator.Pattern)
	}
}
