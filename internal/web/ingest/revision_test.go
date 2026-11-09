package ingest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
)

// The reason an agent gave for revising a draft is kept with the draft for
// MCP, and never printed on the review screen (#588).
func TestReviewScreenDoesNotPrintTheAgentsReason(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)

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
	if strings.Contains(body, "split the sauce into its own part") {
		t.Errorf("the review screen prints the agent's reason:\n%s", body)
	}
}
