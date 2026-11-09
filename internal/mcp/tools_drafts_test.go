package mcp_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// heldDraft is get_recipe_draft's result, with the recipe kept as JSON so a
// test sends it back as an agent would: what it read, with its changes.
type heldDraft struct {
	Token      string         `json:"token"`
	Revision   string         `json:"revision"`
	ReviewPath string         `json:"review_path"`
	Reason     string         `json:"reason"`
	Recipe     map[string]any `json:"draft"`
}

const draftText = "Pie\n\nIngredients\n- 2 cups zz mcp flour\n- 1 tsp salt\n\nInstructions\n1. Mix the flour and salt.\n2. Bake for 30 minutes.\n"

// importForRevision holds a draft of draftText through the real pipeline and
// returns its token.
func importForRevision(t *testing.T, s *site, secret string) string {
	t.Helper()
	var out struct {
		ReviewPath string `json:"review_path"`
	}
	if msg := call(t, s.connect(secret), "import_recipe", map[string]any{"text": draftText}, &out); msg != "" {
		t.Fatal(msg)
	}
	return strings.TrimPrefix(out.ReviewPath, "/ingest/")
}

func readDraft(t *testing.T, s *site, secret, token string) heldDraft {
	t.Helper()
	var got heldDraft
	if msg := call(t, s.connect(secret), "get_recipe_draft", map[string]any{"token": token}, &got); msg != "" {
		t.Fatal(msg)
	}
	return got
}

// component returns the recipe's first component, to change in place.
func component(r map[string]any) map[string]any {
	return r["components"].([]any)[0].(map[string]any)
}

// ADR-00048 obligation 6: a revision replaces the held draft, leaves its
// snapshot, source and parser version alone, marks what it changed as
// LLM-supplied and nothing else, stores its reason, records the credential's
// write, and saves no recipe.
func TestReviseRecipeDraftMarksWhatChangedAndKeepsTheSnapshot(t *testing.T) {
	s := newSiteDrafting(t, config.MCP{Rate: 100, Burst: 100}, nil, true)
	secret, key := s.personalKey("ann@example.com")
	ctx := s.as(s.smiths, "ann@example.com")
	token := importForRevision(t, s, secret)
	before, err := s.rec.GetProposal(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	held := readDraft(t, s, secret, token)
	if held.Revision == "" || held.ReviewPath != "/ingest/"+token || held.Recipe["parser_version"] == "" || held.Reason != "" {
		t.Fatalf("get_recipe_draft = %+v", held)
	}

	r := held.Recipe
	r["name"] = map[string]any{"value": "Salted pie"}
	r["servings"] = map[string]any{"value": "4"}
	r["calories"] = map[string]any{"value": 320}
	r["meal_type"] = "dessert"
	c := component(r)
	lines := c["lines"].([]any)
	lines[1].(map[string]any)["raw"] = "2 tsp salt"
	steps := c["steps"].([]any)
	steps[1].(map[string]any)["passive"] = !boolOf(steps[1].(map[string]any)["passive"])
	c["steps"] = append(steps, map[string]any{"text": "Cool on a rack.", "depends_on": []int{2}})
	var out struct {
		Revision string `json:"revision"`
	}
	if msg := call(t, s.connect(secret), "revise_recipe_draft", map[string]any{
		"token": token, "revision": held.Revision, "draft": r, "reason": "house style: salt by the teaspoon",
	}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Revision == "" || out.Revision == held.Revision {
		t.Errorf("revision after revising = %q, was %q", out.Revision, held.Revision)
	}

	after, err := s.rec.GetProposal(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if after.Snapshot == nil || !bytes.Equal(after.Snapshot.Content, before.Snapshot.Content) ||
		after.Snapshot.SHA256 != before.Snapshot.SHA256 || after.Snapshot.ParserVersion != before.Snapshot.ParserVersion {
		t.Errorf("snapshot changed: %+v, was %+v", after.Snapshot, before.Snapshot)
	}
	if after.Reason != "house style: salt by the teaspoon" || after.State != store.ProposalPending {
		t.Errorf("proposal after revising: reason %q, state %s", after.Reason, after.State)
	}
	d, ok := s.draft(s.smiths, "ann@example.com", token)
	if !ok {
		t.Fatal("the revised draft is gone")
	}
	if d.ParserVersion != held.Recipe["parser_version"] || d.Source.SHA256 != before.Snapshot.SHA256 {
		t.Errorf("parser version %q or source %s changed", d.ParserVersion, d.Source.SHA256)
	}
	if d.Name.Value != "Salted pie" || !d.Name.FromLLM {
		t.Errorf("name = %+v, want the agent's, marked", d.Name)
	}
	ls, ss := d.Components[0].Lines, d.Components[0].Steps
	if len(ls) != 2 || ls[0].FromLLM || !ls[1].FromLLM || ls[1].Raw != "2 tsp salt" {
		t.Errorf("lines = %+v, want flour unmarked and the changed salt marked", ls)
	}
	if ls[0].Ingredient != recipe.IngredientID(s.flour) {
		t.Errorf("the unchanged flour line lost its ingredient: %+v", ls[0])
	}
	if len(ss) != 3 || ss[0].FromLLM || ss[0].GraphFromLLM {
		t.Fatalf("steps = %+v, want the first unchanged and unmarked", ss)
	}
	if ss[1].FromLLM || !ss[1].GraphFromLLM {
		t.Errorf("step 2 = %+v, want its text unmarked and its changed graph marked", ss[1])
	}
	if !ss[2].FromLLM || !ss[2].GraphFromLLM || len(ss[2].DependsOn) != 1 || ss[2].DependsOn[0] != 2 {
		t.Errorf("new step = %+v, want it marked and depending on step 2", ss[2])
	}
	if !d.Servings.FromLLM || d.Servings.Value.String() != "4" || !d.Calories.FromLLM || d.Calories.Value != 320 || !d.MealTypeFromLLM {
		t.Errorf("servings %+v, calories %+v, meal type marked %v; want the agent's, marked", d.Servings, d.Calories, d.MealTypeFromLLM)
	}
	if d.Author.FromLLM || d.Description.FromLLM || d.Yield.FromLLM || d.CuisineFromLLM {
		t.Error("an unchanged field is marked")
	}

	all, err := s.rec.Search(ctx, store.SearchQuery{})
	if err != nil || len(all) != 0 {
		t.Errorf("recipes after revising = %+v, %v; want none", all, err)
	}
	w, err := s.keys.Writes(context.Background(), key)
	if err != nil || len(w) != 2 || w[1].Tool != "revise_recipe_draft" || w[1].HouseholdID != s.smiths.ID {
		t.Errorf("recorded writes = %+v, %v; want the import and the revision", w, err)
	}
	if got := readDraft(t, s, secret, token); got.Reason != "house style: salt by the teaspoon" || got.Revision != out.Revision || got.Recipe["meal_type_from_llm"] != true {
		t.Errorf("get_recipe_draft after revising = reason %q, revision %q", got.Reason, got.Revision)
	}
}

func boolOf(v any) bool { b, _ := v.(bool); return b }

// ADR-00048 obligation 7: an agent reads a draft, a person then edits it on
// the review screen (the replace the review screen makes), and the agent's
// revision from the old revision is refused, leaving the person's edit.
func TestReviseRecipeDraftIsRefusedAfterAPersonsEdit(t *testing.T) {
	s := newSiteDrafting(t, config.MCP{Rate: 100, Burst: 100}, nil, true)
	secret, _ := s.personalKey("ann@example.com")
	ctx := s.as(s.smiths, "ann@example.com")
	token := importForRevision(t, s, secret)
	held := readDraft(t, s, secret, token)

	d, ok := s.draft(s.smiths, "ann@example.com", token)
	if !ok {
		t.Fatal("no draft")
	}
	d.Name.Value = "Ann's pie"
	if err := s.drafts.Replace(ctx, token, d); err != nil {
		t.Fatal(err)
	}

	r := held.Recipe
	r["name"] = map[string]any{"value": "Agent pie"}
	msg := call(t, s.connect(secret), "revise_recipe_draft", map[string]any{
		"token": token, "revision": held.Revision, "draft": r, "reason": "rename",
	}, nil)
	if !strings.Contains(msg, "changed since you read it") {
		t.Errorf("revising from an old revision = %q, want a refusal saying the draft changed", msg)
	}
	after, _ := s.draft(s.smiths, "ann@example.com", token)
	if after.Name.Value != "Ann's pie" || after.Reason != "" {
		t.Errorf("draft after the refused revision: name %q, reason %q; want the person's edit and no reason", after.Name.Value, after.Reason)
	}
}

// list_recipe_drafts gives each pending draft with whether an agent has
// revised it; an edit proposal is not a draft and cannot be read or revised
// as one; another household's draft is unknown.
func TestListAndRefusalsOfRecipeDrafts(t *testing.T) {
	s := newSiteDrafting(t, config.MCP{Rate: 100, Burst: 100}, nil, true)
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	token := importForRevision(t, s, secret)
	other := importForRevision(t, s, secret)

	var page pageOf[struct {
		Token      string `json:"token"`
		Name       string `json:"name"`
		CreatedAt  string `json:"created_at"`
		Revised    bool   `json:"revised_by_agent"`
		ReviewPath string `json:"review_path"`
	}]
	if msg := call(t, cs, "list_recipe_drafts", map[string]any{"limit": 1}, &page); msg != "" {
		t.Fatal(msg)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || page.Items[0].Token != token || page.Items[0].Name != "Pie" || page.Items[0].Revised || page.Items[0].CreatedAt == "" {
		t.Fatalf("first page = %+v", page)
	}

	held := readDraft(t, s, secret, token)
	held.Recipe["name"] = map[string]any{"value": "Better pie"}
	if msg := call(t, cs, "revise_recipe_draft", map[string]any{"token": token, "revision": held.Revision, "draft": held.Recipe, "reason": "name"}, nil); msg != "" {
		t.Fatal(msg)
	}
	if msg := call(t, cs, "list_recipe_drafts", nil, &page); msg != "" {
		t.Fatal(msg)
	}
	revised := map[string]bool{}
	for _, it := range page.Items {
		revised[it.Token] = it.Revised
	}
	if len(page.Items) != 2 || !revised[token] || revised[other] {
		t.Errorf("list after revising = %+v", page.Items)
	}

	// The same recipe sent back is nothing to revise.
	again := readDraft(t, s, secret, token)
	if msg := call(t, cs, "revise_recipe_draft", map[string]any{"token": token, "revision": again.Revision, "draft": again.Recipe, "reason": "none"}, nil); !strings.Contains(msg, "same") {
		t.Errorf("an unchanged revision = %q", msg)
	}
	if msg := call(t, cs, "revise_recipe_draft", map[string]any{"token": token, "revision": again.Revision, "draft": again.Recipe}, nil); !strings.Contains(msg, "reason") {
		t.Errorf("a revision with no reason = %q", msg)
	}

	id := s.recipe(s.smiths, "ann@example.com", "Saved", s.flourLine(1))
	edit, err := s.rec.ProposeEdit(s.as(s.smiths, "ann@example.com"), id, 1, recipe.Recipe{Name: "Saved 2", Components: []recipe.Component{{Steps: []recipe.Step{{ID: "s1", Text: "Mix."}}}}}, store.Provenance{Person: "ann@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if msg := call(t, cs, "get_recipe_draft", map[string]any{"token": edit}, nil); !strings.Contains(msg, "edit proposal") {
		t.Errorf("reading an edit proposal as a draft = %q", msg)
	}
	if msg := call(t, cs, "revise_recipe_draft", map[string]any{"token": edit, "revision": "x", "draft": again.Recipe, "reason": "r"}, nil); !strings.Contains(msg, "edit proposal") {
		t.Errorf("revising an edit proposal = %q", msg)
	}

	jo, err := s.keys.CreateHousehold(context.Background(), s.joneses.ID, "jo@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	if msg := call(t, s.connect(jo), "get_recipe_draft", map[string]any{"token": token}, nil); msg == "" {
		t.Error("another household read the draft")
	}
	var theirs pageOf[struct{}]
	if msg := call(t, s.connect(jo), "list_recipe_drafts", nil, &theirs); msg != "" || len(theirs.Items) != 0 {
		t.Errorf("another household's list = %+v, %q", theirs, msg)
	}
}

// A step's depends_on must name another step of the revision.
func TestReviseRecipeDraftRefusesADependencyOnNoStep(t *testing.T) {
	s := newSiteDrafting(t, config.MCP{Rate: 100, Burst: 100}, nil, true)
	secret, _ := s.personalKey("ann@example.com")
	token := importForRevision(t, s, secret)
	held := readDraft(t, s, secret, token)
	steps := component(held.Recipe)["steps"].([]any)
	steps[0].(map[string]any)["depends_on"] = []int{9}
	msg := call(t, s.connect(secret), "revise_recipe_draft", map[string]any{"token": token, "revision": held.Revision, "draft": held.Recipe, "reason": "graph"}, nil)
	if !strings.Contains(msg, "step 9") {
		t.Errorf("a dependency on no step = %q", msg)
	}
}
