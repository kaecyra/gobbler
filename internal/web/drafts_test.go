package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/store"
)

// memberCtx is the context of a request by email, a member of a household
// already seeded, with that household on it.
func memberCtx(t *testing.T, hs *household.Store, email string) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	h, err := hs.ForEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	return household.WithContext(ctx, h)
}

// ADR-00049, through the real wiring: every signed-in page's nav counts the
// member's own household's drafts waiting for review, re-imports included, and
// shows no badge when there are none. Another household's drafts are never
// counted.
func TestNavCountsOnlyTheHouseholdsWaitingDrafts(t *testing.T) {
	d := testDB(t)
	r := testRouter(t, devConfig(), Deps{DB: d})
	for _, email := range []string{auth.DevEmail, auth.DevOtherEmail} {
		if rec := do(r, http.MethodGet, "/", email); rec.Code != http.StatusOK {
			t.Fatalf("seed %s: %d", email, rec.Code)
		}
	}
	badge := func(email string) string {
		t.Helper()
		body := do(r, http.MethodGet, "/recipes", email).Body.String()
		_, after, found := strings.Cut(body, `data-testid="waiting-drafts">`)
		if !found {
			return ""
		}
		n, _, _ := strings.Cut(after, "<")
		return n
	}
	if got := badge(auth.DevEmail); got != "" {
		t.Errorf("badge with no drafts = %q, want none", got)
	}

	st, hs := store.New(d), household.New(d)
	hold := func(email string) {
		t.Helper()
		if _, err := st.ProposeDraft(memberCtx(t, hs, email), store.DraftInput{
			Document:   []byte(`{"name":{"v":"Pie"},"components":[]}`),
			Source:     store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("pie")},
			Provenance: store.Provenance{Person: email},
		}); err != nil {
			t.Fatal(err)
		}
	}
	hold(auth.DevEmail)
	hold(auth.DevEmail)
	hold(auth.DevOtherEmail)
	if got := badge(auth.DevEmail); got != "2" {
		t.Errorf("dev's badge = %q, want 2", got)
	}
	if got := badge(auth.DevOtherEmail); got != "1" {
		t.Errorf("other's badge = %q, want 1, their own draft alone", got)
	}
}
