package recipeview

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

var proposedBy = store.Provenance{
	Person: "dev@localhost", Tool: "propose_recipe_edit", Reason: "A richer cake.",
	Credential: &store.Credential{Kind: "api_key", ID: 2, Ref: "gobk_unit", Owner: "dev@localhost"},
}

// stored saves testRecipe and an agent's proposal built by edit, returning the
// recipe's id and a way to request pages of it.
func stored(t *testing.T, edit func(*recipe.Recipe)) (id int64, token string, get func(method, path string, form url.Values) *httptest.ResponseRecorder, rs *store.Store, ctx context.Context) {
	t.Helper()
	ctx, rs, cs := storeEnv(t)
	id, err := rs.Create(ctx, testRecipe(t))
	if err != nil {
		t.Fatal(err)
	}
	next, err := rs.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	edit(&next)
	if token, err = rs.ProposeEdit(ctx, id, 1, next, proposedBy); err != nil {
		t.Fatal(err)
	}
	return id, token, func(method, path string, form url.Values) *httptest.ResponseRecorder {
		var req *http.Request
		if form != nil {
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		return serveRequest(t, rs, cs, req.WithContext(ctx))
	}, rs, ctx
}

func TestPageMarksAPendingProposal(t *testing.T) {
	id, token, get, _, _ := stored(t, func(r *recipe.Recipe) {
		r.Name = "Richer cake"
		r.Components[0].Lines = r.Components[0].Lines[:2] // drops three lines
		r.Components[0].Lines[1].Amount.Qty = quantity.Exact(quantity.Int(2))
		r.Components[0].Steps[0].Text = "Mix everything well."
		r.Components[0].Steps = append(r.Components[0].Steps, recipe.Step{ID: "new-1", Text: "Cool it."})
		r.Components = append(r.Components, recipe.Component{Name: "Frosting", Lines: r.Components[0].Lines[:1], Steps: []recipe.Step{{ID: "new-2", Text: "Whip."}}})
		r.Equipment = nil
	})
	rec := get(http.MethodGet, fmt.Sprintf("/recipes/%d", id), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	mustContain(t, body,
		`data-testid="proposal-banner"`, "A richer cake.", "API key gobk_unit (dev@localhost)",
		fmt.Sprintf(`href="/recipes/%d/edit?proposal=%s"`, id, token),
		fmt.Sprintf(`action="/recipes/%d/proposal/reject"`, id), `id="pmarks"`,
		"Richer cake", `data-testid="proposal-line-new"`, `data-testid="proposal-line-removed"`,
		`data-testid="proposal-step-changed"`, "Mix everything well.", `data-testid="proposal-step-added"`, "Cool it.",
		`data-testid="proposal-part-added"`, "New part: Frosting", "Equipment")
	// The page is still the live recipe.
	mustContain(t, body, "Sponge &lt;cake&gt;", "Mix everything.")
}

func TestPageOfAStaleProposalOffersRejectOnly(t *testing.T) {
	id, _, get, rs, ctx := stored(t, func(r *recipe.Recipe) { r.Name = "Agent cake" })
	cur, err := rs.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Name = "Person cake"
	if err := rs.UpdateIfVersion(ctx, id, 1, cur); err != nil {
		t.Fatal(err)
	}
	body := get(http.MethodGet, fmt.Sprintf("/recipes/%d", id), nil).Body.String()
	mustContain(t, body, `data-testid="proposal-stale"`, "out of date", `data-testid="proposal-reject"`, "Person cake")
	mustNotContain(t, body, `data-testid="proposal-review"`, `id="pmarks"`, `data-testid="proposal-field"`, "Agent cake")
}

func TestRejectTurnsTheProposalDownAndRefusesWhatIsNotTheRecipesPending(t *testing.T) {
	id, token, get, rs, ctx := stored(t, func(r *recipe.Recipe) { r.Name = "Agent cake" })
	path := fmt.Sprintf("/recipes/%d/proposal/reject", id)
	if rec := get(http.MethodPost, path, url.Values{"proposal": {"no-such-token"}}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown token: status %d, want 404", rec.Code)
	}
	if rec := get(http.MethodPost, path, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no token: status %d, want 400", rec.Code)
	}
	if rec := get(http.MethodPost, "/recipes/999/proposal/reject", url.Values{"proposal": {token}}); rec.Code != http.StatusNotFound {
		t.Errorf("another recipe's id: status %d, want 404", rec.Code)
	}
	rec := get(http.MethodPost, path, url.Values{"proposal": {token}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != fmt.Sprintf("/recipes/%d", id) {
		t.Fatalf("reject: status %d to %q, want 303 to the recipe", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(http.MethodPost, path, url.Values{"proposal": {token}}); rec.Code != http.StatusConflict {
		t.Errorf("rejecting twice: status %d, want 409", rec.Code)
	}
	if p, err := rs.GetProposal(ctx, token); err != nil || p.State != store.ProposalRejected {
		t.Errorf("proposal = %+v (%v), want rejected", p.State, err)
	}
	if cur, _ := rs.Get(ctx, id); cur.Name != "Sponge <cake>" {
		t.Errorf("recipe name = %q, want it unchanged", cur.Name)
	}
	mustNotContain(t, get(http.MethodGet, fmt.Sprintf("/recipes/%d", id), nil).Body.String(), `data-testid="proposal-banner"`)
}
