package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
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
		{http.MethodGet, "/invitations/../recipes"},
		{http.MethodGet, "/static/../recipes"},
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
	// Acceptance is another node's route. Until it exists the path is simply
	// unrouted; what matters here is that the lock does not turn it away.
	rec = do(h, http.MethodGet, InvitationsPrefix+"1", strangerEmail)
	if strings.Contains(rec.Body.String(), notAMemberText) {
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
		{"non-operator member", auth.DevEmail, http.MethodGet, "/settings", http.StatusForbidden},
		{"non-operator member, slash", auth.DevEmail, http.MethodGet, "/settings/", http.StatusForbidden},
		{"non-operator member, action", auth.DevEmail, http.MethodPost, "/settings/backup", http.StatusForbidden},
		{"non-operator member, dot segment", auth.DevEmail, http.MethodPost, "/settings/./backup", http.StatusForbidden},
		{"operator who is a member", auth.DevOtherEmail, http.MethodGet, "/settings", http.StatusOK},
		{"operator with no household", operatorEmail, http.MethodGet, "/settings", http.StatusOK},
		{"operator with no household, action", operatorEmail, http.MethodPost, "/settings/backup", http.StatusSeeOther},
		{"non-member non-operator", strangerEmail, http.MethodGet, "/settings", http.StatusForbidden},
	}
	for _, c := range cases {
		rec := do(h, c.method, c.path, c.email)
		if rec.Code != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, rec.Code, c.want)
		}
	}
	if body := do(h, http.MethodGet, "/settings", auth.DevEmail).Body.String(); !strings.Contains(body, "operator") || strings.Contains(body, "Backup") {
		t.Errorf("refusal of a member should name the operator and show no settings:\n%s", body)
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
		t.Fatal("a deployment behind Access got the dev member")
	}
}
