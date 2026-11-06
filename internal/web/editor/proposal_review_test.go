package editor

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/store"
)

var reviewedBy = store.Provenance{
	Person: "dev@localhost", Tool: "propose_recipe_edit", Reason: "A better name.",
	Credential: &store.Credential{Kind: "api_key", ID: 4, Ref: "gobk_rev", Owner: "dev@localhost"},
}

// propose stores an agent's proposal to rename the recipe under test and
// returns its token and the form a reviewer's browser would post for it.
func (e *env) propose() (string, url.Values) {
	e.t.Helper()
	r, v := e.stored()
	r.Name = "Agent Loaf"
	token, err := e.store.ProposeEdit(e.ctx, e.id, v, r, reviewedBy)
	if err != nil {
		e.t.Fatal(err)
	}
	d := fromRecipe(r, e.names())
	d.Version, d.Proposal = v, token
	f := encode(d)
	f.Set(fieldProposal, token)
	return token, f
}

func (e *env) review(token string) result {
	return e.do(http.MethodGet, e.path()+"?proposal="+token, nil, nil)
}

func TestReviewOpensTheProposalWithASummaryAndSavesItAsAProposalRevision(t *testing.T) {
	e := newEnv(t)
	token, form := e.propose()
	res := e.review(token)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", res.Code, res.body())
	}
	for _, want := range []string{`data-testid="proposal-summary"`, "Agent Loaf", "A better name.", "API key gobk_rev", "Name: Test Loaf, now Agent Loaf", `name="proposal" value="` + token + `"`} {
		if !strings.Contains(res.body(), want) {
			t.Errorf("review page lacks %q", want)
		}
	}

	if res := e.post(form); res.Code != http.StatusSeeOther {
		t.Fatalf("save: status = %d\n%s", res.Code, res.body())
	}
	got, version := e.stored()
	if got.Name != "Agent Loaf" || version != 2 {
		t.Errorf("recipe = %q at version %d, want the proposal saved as version 2", got.Name, version)
	}
	revs := e.revisions()
	if len(revs) != 1 || revs[0].Proposal == nil || revs[0].Proposal.Token != token {
		t.Errorf("revisions = %+v, want one that refers to the proposal", revs)
	}
	if p, err := e.store.GetProposal(e.ctx, token); err != nil || p.State != store.ProposalAccepted {
		t.Errorf("proposal = %+v (%v), want accepted", p.State, err)
	}
	// A saved proposal cannot be opened again.
	if res := e.review(token); res.Code != http.StatusConflict {
		t.Errorf("reopening a saved proposal: status %d, want 409", res.Code)
	}
}

func TestReviewKeepsTheSummaryThroughAStructuralAction(t *testing.T) {
	e := newEnv(t)
	token, form := e.propose()
	form.Set(fieldAction, "add-step:0")
	res := e.post(form)
	if res.Code != http.StatusOK || !strings.Contains(res.body(), `data-testid="proposal-summary"`) || !strings.Contains(res.body(), `value="`+token+`"`) {
		t.Errorf("after an action: status %d, summary and token in the form: %v", res.Code, strings.Contains(res.body(), "proposal-summary"))
	}
}

func TestReviewOfAStaleProposalIsRefusedAndARacingSaveChangesNothing(t *testing.T) {
	e := newEnv(t)
	token, form := e.propose()
	// The reviewer has the form open at version 1 when someone saves the recipe.
	person, v := e.stored()
	person.Description = "Saved by a person."
	if err := e.store.UpdateIfVersion(e.ctx, e.id, v, person); err != nil {
		t.Fatal(err)
	}
	res := e.post(form)
	if !strings.Contains(res.body(), "out of date") {
		t.Errorf("racing save: status %d, body lacks the out-of-date message", res.Code)
	}
	if got, _ := e.stored(); got.Name != "Test Loaf" || got.Description != "Saved by a person." {
		t.Errorf("recipe = %q / %q, want the person's save untouched", got.Name, got.Description)
	}
	if len(e.revisions()) != 1 {
		t.Errorf("revisions = %d, want only the person's save", len(e.revisions()))
	}
	// Opening it for review is refused outright.
	res = e.review(token)
	if res.Code != http.StatusConflict || !strings.Contains(res.body(), "out of date") || strings.Contains(res.body(), `id="`+FormID+`"`) {
		t.Errorf("opening a stale proposal: status %d, want a 409 page with no form", res.Code)
	}
	// A save posted at the new version still names the stale proposal and is refused.
	form.Set(fieldVersion, strconv.FormatInt(v+1, 10))
	if res := e.post(form); res.Code != http.StatusConflict {
		t.Errorf("save of a stale proposal at the current version: status %d, want 409", res.Code)
	}
}

func TestReviewRefusesAProposalThatIsNotThisRecipesOrDoesNotExist(t *testing.T) {
	e := newEnv(t)
	if res := e.review("no-such-token"); res.Code != http.StatusNotFound {
		t.Errorf("unknown token: status %d, want 404", res.Code)
	}
	other, err := e.store.Create(e.ctx, e.fixture())
	if err != nil {
		t.Fatal(err)
	}
	r, _ := e.stored()
	r.Name = "Other"
	token, err := e.store.ProposeEdit(e.ctx, other, 1, r, reviewedBy)
	if err != nil {
		t.Fatal(err)
	}
	if res := e.review(token); res.Code != http.StatusNotFound {
		t.Errorf("another recipe's proposal: status %d, want 404", res.Code)
	}
}
