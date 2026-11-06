package recipeview

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
)

type reimportFixture struct {
	id  int64
	rs  *store.Store
	ctx context.Context
	get func() string
}

// newReimportFixture stores testRecipe with the source URL (or none) and gives a
// way to request its page.
func newReimportFixture(t *testing.T, sourceURL string) *reimportFixture {
	t.Helper()
	ctx, rs, cs := storeEnv(t)
	r := testRecipe(t)
	r.SourceURL = sourceURL
	id, err := rs.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return &reimportFixture{id: id, rs: rs, ctx: ctx, get: func() string {
		rec := serveRequest(t, rs, cs, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/recipes/%d", id), nil).WithContext(ctx))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		return rec.Body.String()
	}}
}

func (f *reimportFixture) hold(t *testing.T, base int64) string {
	t.Helper()
	token, err := f.rs.ProposeDraft(f.ctx, store.DraftInput{
		Document:   []byte(fmt.Sprintf(`{"name":"Fresh","reimport":{"recipe":%d,"base_version":%d},"components":[]}`, f.id, base)),
		Source:     store.SnapshotInput{Kind: store.SnapshotHTML, Content: []byte("<html>again</html>")},
		Provenance: store.Provenance{Person: "dev@localhost", Reason: "Re-imported from example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// ADR-00042 obligation 9, unit side: the button posts to the ingest screen and
// only a recipe with a source URL has it.
func TestReimportButtonIsShownOnlyForARecipeWithASourceURL(t *testing.T) {
	f := newReimportFixture(t, "https://example.test/cake")
	mustContain(t, f.get(), `data-testid="reimport-button"`, "Re-import from source", fmt.Sprintf(`action="/ingest/reimport/%d"`, f.id), `method="post"`)
	mustNotContain(t, newReimportFixture(t, "").get(), "reimport-button", "Re-import from source")
}

// ADR-00042 obligation 12, unit side.
func TestPendingReimportShowsABannerWithAReviewLinkAndNoMarks(t *testing.T) {
	f := newReimportFixture(t, "https://example.test/cake")
	mustNotContain(t, f.get(), "reimport-banner")
	token := f.hold(t, 1)
	body := f.get()
	mustContain(t, body, `data-testid="reimport-banner"`, "A re-import is waiting for review", "example.test",
		`href="/ingest/`+token+`"`, `data-testid="reimport-review"`, `action="/ingest/`+token+`/cancel"`)
	mustNotContain(t, body, "reimport-stale", `data-testid="proposal-banner"`, `data-testid="proposal-fields"`, `data-testid="proposal-marks-toggle"`)
}

func TestStaleReimportSaysTheRecipeChangedAndOffersOnlyDiscard(t *testing.T) {
	f := newReimportFixture(t, "https://example.test/cake")
	token := f.hold(t, 1)
	r, err := f.rs.Get(f.ctx, f.id)
	if err != nil {
		t.Fatal(err)
	}
	r.Name = "Edited meanwhile"
	if err := f.rs.Update(f.ctx, f.id, r); err != nil {
		t.Fatal(err)
	}
	body := f.get()
	mustContain(t, body, `data-testid="reimport-stale"`, "The recipe changed since the re-import", `data-testid="reimport-discard"`, `action="/ingest/`+token+`/cancel"`)
	mustNotContain(t, body, `data-testid="reimport-review"`)
}

func TestAnAgentEditSupersedesTheBanner(t *testing.T) {
	f := newReimportFixture(t, "https://example.test/cake")
	f.hold(t, 1)
	next, _ := f.rs.Get(f.ctx, f.id)
	next.Name = "Agent's name"
	if _, err := f.rs.ProposeEdit(f.ctx, f.id, 1, next, proposedBy); err != nil {
		t.Fatal(err)
	}
	body := f.get()
	mustNotContain(t, body, "reimport-banner")
	mustContain(t, body, `data-testid="proposal-banner"`)
}
