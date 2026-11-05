package mcp_test

import (
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type gotRecipe struct {
	ID         int64  `json:"id"`
	Version    int64  `json:"version"`
	Name       string `json:"name"`
	Components []struct {
		Name        string `json:"name"`
		Ingredients []struct {
			Key  string `json:"key"`
			Text string `json:"text"`
		} `json:"ingredients"`
		Steps []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"steps"`
	} `json:"components"`
}

type proposed struct {
	Token      string `json:"token"`
	ReviewPath string `json:"review_path"`
}

type proposalState struct {
	State          string `json:"state"`
	Stale          bool   `json:"stale"`
	RecipeID       int64  `json:"recipe_id"`
	BaseVersion    int64  `json:"base_version"`
	CurrentVersion int64  `json:"current_version"`
	ReviewPath     string `json:"review_path"`
	Reason         string `json:"reason"`
}

// editArgs is the arguments of propose_recipe_edit for the recipe r as
// get_recipe showed it, with its name changed.
func editArgs(r gotRecipe, name, reason string) map[string]any {
	comps := []map[string]any{}
	for _, c := range r.Components {
		lines, steps := []map[string]any{}, []map[string]any{}
		for _, l := range c.Ingredients {
			lines = append(lines, map[string]any{"key": l.Key, "text": l.Text})
		}
		for _, s := range c.Steps {
			steps = append(steps, map[string]any{"id": s.ID, "text": s.Text})
		}
		comps = append(comps, map[string]any{"name": c.Name, "ingredients": lines, "steps": steps})
	}
	return map[string]any{"id": r.ID, "version": r.Version, "reason": reason, "recipe": map[string]any{"name": name, "components": comps}}
}

func (s *site) read(cs *sdk.ClientSession, id int64) gotRecipe {
	s.t.Helper()
	var r gotRecipe
	if msg := call(s.t, cs, "get_recipe", map[string]any{"id": id}, &r); msg != "" {
		s.t.Fatal(msg)
	}
	return r
}

// An MCP edit creates a proposal and changes no recipe until a person confirms
// it; the write records the credential (#353).
func TestProposingAnEditChangesNoRecipeAndRecordsTheCredential(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	r := s.read(cs, id)
	if r.Version != 1 || len(r.Components[0].Ingredients) != 1 || r.Components[0].Ingredients[0].Key == "" {
		t.Fatalf("get_recipe = %+v, want its version and the keys of its lines", r)
	}

	var out proposed
	if msg := call(t, cs, "propose_recipe_edit", editArgs(r, "Better bread", "the name sells it"), &out); msg != "" {
		t.Fatal(msg)
	}
	if out.ReviewPath != mcp.EditPath(id, out.Token) || !strings.HasPrefix(out.ReviewPath, "/recipes/") {
		t.Errorf("review path = %q", out.ReviewPath)
	}
	ctx := s.as(s.smiths, "ann@example.com")
	if cur, err := s.rec.Get(ctx, id); err != nil || cur.Name != "Bread" {
		t.Fatalf("recipe after the proposal = %q (%v), want it unchanged", cur.Name, err)
	}
	if info, _ := s.rec.Info(ctx, id); info.Version != 1 {
		t.Errorf("version = %d, want 1: a proposal is not a save", info.Version)
	}
	p, err := s.rec.GetProposal(ctx, out.Token)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != store.ProposalPending || p.Edit == nil || p.Edit.Name != "Better bread" || p.BaseVersion != 1 || p.RecipeID != id {
		t.Errorf("proposal = %+v, want a pending edit of version 1 naming 'Better bread'", p)
	}
	if p.Reason != "the name sells it" || p.Tool != "propose_recipe_edit" || p.Credential == nil || p.Credential.ID != key.KeyID {
		t.Errorf("provenance = %+v, want the reason, tool and credential", p.Provenance)
	}
	if w, _ := s.keys.Writes(t.Context(), key); len(w) != 1 || w[0].Tool != "propose_recipe_edit" {
		t.Errorf("recorded writes = %+v, want the proposal", w)
	}
	// A line sent back unchanged is the stored line, not a re-reading of its text.
	if got := p.Edit.Components[0].Lines[0]; got.Key != r.Components[0].Ingredients[0].Key || got.Raw != "flour" {
		t.Errorf("unchanged line = %+v, want the stored line", got)
	}
}

func TestAnEditedLineIsReadAndKeepsItsKey(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	r := s.read(cs, id)
	args := editArgs(r, "Bread", "more flour")
	lines := args["recipe"].(map[string]any)["components"].([]map[string]any)[0]["ingredients"].([]map[string]any)
	lines[0]["text"] = "3 cups zz mcp flour"
	lines = append(lines, map[string]any{"text": "1 tsp zz mcp flour, sifted"})
	args["recipe"].(map[string]any)["components"].([]map[string]any)[0]["ingredients"] = lines

	var out proposed
	if msg := call(t, cs, "propose_recipe_edit", args, &out); msg != "" {
		t.Fatal(msg)
	}
	p, err := s.rec.GetProposal(s.as(s.smiths, "ann@example.com"), out.Token)
	if err != nil {
		t.Fatal(err)
	}
	got := p.Edit.Components[0].Lines
	if len(got) != 2 || got[0].Key != r.Components[0].Ingredients[0].Key || got[0].Amount.Qty.String() != "3" || got[0].Amount.Unit.Name != "cup" {
		t.Fatalf("lines = %+v, want the first kept by key at 3 cups and a second", got)
	}
	if got[1].Key != "" || got[1].Ingredient != recipe.IngredientID(s.flour) || got[1].Amount.Unit.Name != "tsp" || len(got[1].Qualifiers) != 1 {
		t.Errorf("new line = %+v, want 1 tsp of flour, sifted, with no key yet", got[1])
	}
}

func TestAnEditIsRefusedWhenItCannotBeMadeSense(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	other := s.recipe(s.joneses, "jo@example.com", "Theirs")
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	r := s.read(cs, id)
	ok := editArgs(r, "Bread 2", "why")

	with := func(mutate func(map[string]any)) map[string]any {
		a := editArgs(r, "Bread 2", "why")
		mutate(a)
		return a
	}
	comp := func(a map[string]any) map[string]any {
		return a["recipe"].(map[string]any)["components"].([]map[string]any)[0]
	}
	for name, c := range map[string]struct {
		args map[string]any
		want string
	}{
		"no reason":          {with(func(a map[string]any) { a["reason"] = "  " }), "reason"},
		"nothing changed":    {editArgs(r, "Bread", "why"), "nothing to propose"},
		"stale version":      {with(func(a map[string]any) { a["version"] = 7 }), "version 1, not 7"},
		"another household":  {with(func(a map[string]any) { a["id"] = other }), "not found"},
		"unknown ingredient": {with(func(a map[string]any) { comp(a)["ingredients"] = []map[string]any{{"text": "2 cups zzz unobtainium"}} }), "catalog"},
		"unknown line key": {with(func(a map[string]any) {
			comp(a)["ingredients"] = []map[string]any{{"key": "lin-zzzzzzzz", "text": "2 cups zz mcp flour"}}
		}), "not a line of this recipe"},
		"unknown step id": {with(func(a map[string]any) { comp(a)["steps"] = []map[string]any{{"id": "stp-zzzzzzzz", "text": "x"}} }), "not a step of this recipe"},
		"step cycle": {with(func(a map[string]any) {
			comp(a)["steps"] = []map[string]any{{"id": "a", "text": "x", "depends_on": []string{"b"}}, {"id": "b", "text": "y", "depends_on": []string{"a"}}}
		}), "not valid"},
		"no name": {with(func(a map[string]any) { a["recipe"].(map[string]any)["name"] = " " }), "name"},
	} {
		if msg := call(t, cs, "propose_recipe_edit", c.args, nil); !strings.Contains(msg, c.want) {
			t.Errorf("%s: error = %q, want it to mention %q", name, msg, c.want)
		}
	}
	if _, found, _ := s.rec.PendingEdit(s.as(s.smiths, "ann@example.com"), id); found {
		t.Error("a refused proposal left a pending proposal behind")
	}
	if msg := call(t, cs, "propose_recipe_edit", ok, nil); msg != "" {
		t.Errorf("the control proposal was refused: %s", msg)
	}
}

// ADR-00035 obligation 9, over MCP: an agent reading its proposal is told it is
// stale, with the recipe's current version, once the recipe has moved on.
func TestAProposalIsReportedStaleOverMCPAfterAnEditorSave(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var out proposed
	if msg := call(t, cs, "propose_recipe_edit", editArgs(s.read(cs, id), "Agent's bread", "why"), &out); msg != "" {
		t.Fatal(msg)
	}
	var st proposalState
	if msg := call(t, cs, "get_recipe_proposal", map[string]any{"token": out.Token}, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.State != "pending" || st.Stale || st.ReviewPath == "" || st.CurrentVersion != 1 || st.Reason != "why" {
		t.Fatalf("fresh proposal = %+v, want pending, not stale, with a review path", st)
	}

	ctx := s.as(s.smiths, "ann@example.com")
	cur, err := s.rec.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cur.Name = "Person's bread"
	if err := s.rec.UpdateIfVersion(ctx, id, 1, cur); err != nil {
		t.Fatal(err)
	}
	st = proposalState{}
	if msg := call(t, cs, "get_recipe_proposal", map[string]any{"token": out.Token}, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.State != "pending" || !st.Stale || st.CurrentVersion != 2 || st.BaseVersion != 1 || st.ReviewPath != "" {
		t.Errorf("stale proposal = %+v, want stale, base 1, current 2 and no review path", st)
	}

	// Proposing again against the new version supersedes it.
	var again proposed
	if msg := call(t, cs, "propose_recipe_edit", editArgs(s.read(cs, id), "Agent's second try", "again"), &again); msg != "" {
		t.Fatal(msg)
	}
	if msg := call(t, cs, "get_recipe_proposal", map[string]any{"token": out.Token}, &st); msg != "" || st.State != "superseded" {
		t.Errorf("first proposal = %+v %q, want superseded", st, msg)
	}
}

func TestAProposalCannotBeReadByAnotherHousehold(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var out proposed
	if msg := call(t, cs, "propose_recipe_edit", editArgs(s.read(cs, id), "Agent's bread", "why"), &out); msg != "" {
		t.Fatal(msg)
	}
	joSecret, _ := s.personalKey("jo@example.com")
	if msg := call(t, s.connect(joSecret), "get_recipe_proposal", map[string]any{"token": out.Token}, nil); !strings.Contains(msg, "not found") {
		t.Errorf("another household reading the proposal: %q, want not found", msg)
	}
}
