package ingest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
)

// The reason an agent gave for revising a draft shows on the review screen as
// a page-level callout, and only when there is one (ADR-00048).
func TestReviewScreenShowsTheAgentsReason(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	if body := f.do(http.MethodGet, "/"+token, nil).Body.String(); strings.Contains(body, `data-testid="revision-reason"`) {
		t.Fatal("a draft no agent revised shows a revision reason")
	}

	ctx := f.ctx(t)
	rec := store.New(f.db)
	p, err := rec.GetProposal(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.ReviseDraft(ctx, token, p.Document, store.DraftRevision(p.Document), "split the sauce into its own part"); err != nil {
		t.Fatal(err)
	}
	res := f.do(http.MethodGet, "/"+token, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("review = %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, `data-testid="revision-reason"`) || !strings.Contains(body, "split the sauce into its own part") {
		t.Errorf("the review screen does not show the agent's reason:\n%s", body)
	}
}
