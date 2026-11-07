package ingest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
)

// countingRecipes counts the drafts the handler holds. The stand-in recipe
// service checks no membership, so any refusal below comes from the ingestion
// service Routes builds.
type countingRecipes struct {
	*testRecipes
	added atomic.Int32
}

func (c *countingRecipes) AddDraft(ctx context.Context, cl apirecipes.Caller, d ingestcore.Draft, by apirecipes.Provenance) (string, error) {
	c.added.Add(1)
	return c.testRecipes.AddDraft(ctx, cl, d, by)
}

// asPerson signs in as email and puts h on the context, as the authentication
// and membership middleware do. The two need not agree: that is the case the
// ingestion service must refuse.
func asPerson(email string, h household.Household) func(http.Handler) http.Handler {
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		panic(err)
	}
	return func(next http.Handler) http.Handler {
		inner := a.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set(auth.DevIdentityHeader, email)
			inner.ServeHTTP(w, r)
		})
	}
}

// Routes sends every drafting call through the ingestion service bound to the
// request's Caller, so a request whose person is not a member of the household
// on its context drafts nothing, while a member's request does (ADR-00043,
// ADR-00018). Reverting the binding to an unchecked pipeline fails this.
func TestRoutesDraftOnlyForAMemberOfTheRequestsHousehold(t *testing.T) {
	f := newFixture(t)
	rs := &countingRecipes{testRecipes: f.rs}
	cat, err := apicatalog.New(apicatalog.Deps{Catalog: apicatalog.NewStore(f.db), Members: apihouseholds.NewStore(f.db)})
	if err != nil {
		t.Fatal(err)
	}
	routes := Routes(Deps{
		DB: f.db, Recipes: rs, Catalog: cat.Bind(caller.FromContext),
		Ingestion: config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t"},
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	post := func(mw func(http.Handler) http.Handler) *httptest.ResponseRecorder {
		req := newFormRequest("/", url.Values{"text": {"Apple pie\n2 tbsp butter"}})
		rec := httptest.NewRecorder()
		mw(routes).ServeHTTP(rec, req)
		return rec
	}

	// The other household's member, on household A's context.
	if rec := post(asPerson(auth.DevOtherEmail, householdA)); rec.Code != http.StatusForbidden {
		t.Errorf("non-member: status = %d, want 403; body: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if n := rs.added.Load(); n != 0 {
		t.Fatalf("a non-member's request held %d drafts", n)
	}

	if rec := post(asPerson(auth.DevEmail, householdA)); rec.Code != http.StatusSeeOther {
		t.Fatalf("member: status = %d, want 303; body: %s", rec.Code, rec.Body)
	}
	if n := rs.added.Load(); n != 1 {
		t.Errorf("a member's request held %d drafts, want 1", n)
	}
}
