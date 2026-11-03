package profile

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

type fakeHouseholds struct {
	byEmail map[string]household.Household
	err     error
}

func (f fakeHouseholds) ForEmail(_ context.Context, email string) (household.Household, error) {
	if f.err != nil {
		return household.Household{}, f.err
	}
	h, ok := f.byEmail[email]
	if !ok {
		return household.Household{}, household.ErrNotFound
	}
	return h, nil
}

// ADR-00032 obligation 1: the visibility rule is asked with the right relation
// for a viewer in the same household and for one who is not.
func TestRelationOfTheViewerToThePerson(t *testing.T) {
	alpha := household.Household{ID: 1, Name: "Alpha"}
	beta := household.Household{ID: 2, Name: "Beta"}
	hs := fakeHouseholds{byEmail: map[string]household.Household{
		"ann@example.com": alpha, "bob@example.com": alpha, "cy@example.com": beta,
	}}
	cases := []struct {
		name            string
		viewer, person  string
		viewerHousehold *household.Household
		want            domain.Relation
	}{
		{"self", "ann@example.com", "ann@example.com", &alpha, domain.Self},
		{"self without a household", "stranger@example.com", "stranger@example.com", nil, domain.Self},
		{"same household", "ann@example.com", "bob@example.com", &alpha, domain.SameHousehold},
		{"another household", "ann@example.com", "cy@example.com", &alpha, domain.Other},
		{"a person with no household", "ann@example.com", "stranger@example.com", &alpha, domain.Other},
		{"a viewer with no household", "stranger@example.com", "ann@example.com", nil, domain.Other},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.viewerHousehold != nil {
				ctx = household.WithContext(ctx, *tc.viewerHousehold)
			}
			h := &handler{Deps{Households: hs}}
			got, err := h.relation(ctx, tc.viewer, tc.person)
			if err != nil || got != tc.want {
				t.Errorf("relation = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// A household lookup that fails is a failure, not "not the same household": it
// must not quietly decide what a viewer may see.
func TestRelationReportsAFailingHouseholdLookup(t *testing.T) {
	boom := errors.New("database is locked")
	h := &handler{Deps{Households: fakeHouseholds{err: boom}}}
	ctx := household.WithContext(context.Background(), household.Household{ID: 1})
	if _, err := h.relation(ctx, "ann@example.com", "bob@example.com"); !errors.Is(err, boom) {
		t.Errorf("relation error = %v, want the lookup's error", err)
	}
	// Self needs no lookup, so a failing store does not break one's own page.
	if rel, err := h.relation(ctx, "ann@example.com", "Ann@Example.com"); err != nil || rel != domain.Self {
		t.Errorf("own profile with a failing store = %q, %v", rel, err)
	}
}

// The form that sets the name is filled for the person themself only.
func TestViewFillsTheNameFormForSelfOnly(t *testing.T) {
	p := domain.Profile{Email: "ann@example.com", Address: "addr", DisplayName: "Ann B"}
	own := newView(subject{Profile: p, Viewer: p.Email, Relation: domain.Self})
	if !own.Self || own.OwnName != "Ann B" || own.DefaultName != "ann" {
		t.Errorf("own view = %+v", own)
	}
	for _, rel := range []domain.Relation{domain.SameHousehold, domain.Other} {
		v := newView(subject{Profile: p, Viewer: "bob@example.com", Relation: rel})
		if v.Self || v.OwnName != "" || v.DefaultName != "" {
			t.Errorf("%s view carries the owner's form: %+v", rel, v)
		}
		if v.Shown.Name != "Ann B" {
			t.Errorf("%s view shows name %q", rel, v.Shown.Name)
		}
	}
	if got := newView(subject{Profile: p, Relation: "unknown"}).title(); got != "Profile" {
		t.Errorf("a view with the name hidden is titled %q", got)
	}
}

// site is the profile routes behind the real identity middleware (the dev
// bypass, which reads the identity from a header), over a real database, so the
// handlers run as they do in production. Package web's tests cover the same
// routes behind the whole router.
type site struct {
	h http.Handler
}

func newSite(t *testing.T, hs Households) *site {
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
	r.Mount(Pattern, Routes(Deps{Profiles: domain.New(d), Households: hs, DB: d}))
	return &site{h: r}
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

func TestRoutesServeOwnProfileAndKeepOthersOut(t *testing.T) {
	s := newSite(t, fakeHouseholds{})
	rec := s.do(http.MethodGet, "/profile", "ann@example.com", nil)
	path := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(path, "/profile/") {
		t.Fatalf("GET /profile = %d to %q", rec.Code, path)
	}
	if rec := s.do(http.MethodGet, path, "ann@example.com", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `action="`+path+`/name"`) {
		t.Fatalf("own profile = %d:\n%s", rec.Code, rec.Body)
	}
	if rec := s.do(http.MethodPost, path+"/name", "ann@example.com", url.Values{"name": {"Ann B"}}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("rename = %d to %q", rec.Code, rec.Header().Get("Location"))
	}

	other := s.do(http.MethodGet, path, "bob@example.com", nil)
	if other.Code != http.StatusOK || !strings.Contains(other.Body.String(), "Ann B") || strings.Contains(other.Body.String(), `action="`+path+`/name"`) {
		t.Errorf("another person's view = %d:\n%s", other.Code, other.Body)
	}
	if rec := s.do(http.MethodPost, path+"/name", "bob@example.com", url.Values{"name": {"Hijacked"}}); rec.Code != http.StatusNotFound {
		t.Errorf("another person renaming = %d, want 404", rec.Code)
	}
	if rec := s.do(http.MethodGet, "/profile/"+strings.Repeat("A", 22), "ann@example.com", nil); rec.Code != http.StatusNotFound {
		t.Errorf("a guessed address = %d, want 404", rec.Code)
	}
	if rec := s.do(http.MethodPost, path+"/name", "ann@example.com", url.Values{"name": {strings.Repeat("x", 61)}}); rec.Code != http.StatusBadRequest {
		t.Errorf("a too long name = %d, want 400", rec.Code)
	}
}
