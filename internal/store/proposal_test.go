package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

var agent = Provenance{
	Person:     "owner@example.test",
	Credential: &Credential{Kind: "api_key", ID: 3, Ref: "gobk_ab12", Owner: "owner@example.test", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	Tool:       "edit_recipe",
	Reason:     "less salt",
}

// draftDoc is a draft document in the shape the store reads: a line naming
// ingredient ing, and a field the store knows nothing about.
func draftDoc(ing int64) []byte {
	return []byte(fmt.Sprintf(`{"name":"Draft","components":[{"lines":[{"raw":"x","ingredient":%d,"confidence":0.5}]}],"extra":{"kept":true}}`, ing))
}

func draftInput(ing int64) DraftInput {
	return DraftInput{
		Document:   draftDoc(ing),
		Source:     SnapshotInput{Kind: SnapshotText, Content: []byte("2 eggs"), ParserVersion: "7"},
		Provenance: Provenance{Person: "dev@localhost"},
	}
}

// newRecipe stores a one-line recipe and returns its id.
func newRecipe(ctx context.Context, t *testing.T, s *Store, ing int64) int64 {
	t.Helper()
	id, err := s.Create(ctx, recipe.Recipe{Name: "Base", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Ingredient: recipe.IngredientID(ing), Rule: quantity.RuleLinear, Raw: "salt", Amount: quantity.Amount{Qty: qty(t, "2")}}},
		Steps: []recipe.Step{{ID: "a", Text: "Mix."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func editOf(ctx context.Context, t *testing.T, s *Store, id int64, mutate func(*recipe.Recipe)) recipe.Recipe {
	t.Helper()
	r, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&r)
	return r
}

// ADR-00035 obligation 5: a proposal survives a restart, with its state,
// reason and provenance.
func TestProposalSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{Server: config.Server{DataDir: dir}}
	d1, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, d1, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedHouseholds(t, d1)
	_, ids := seedCatalog(t, d1)
	s1 := New(d1)
	hctx := authedCtx(t)
	id := newRecipe(hctx, t, s1, ids.salt)
	token, err := s1.ProposeEdit(hctx, id, 1, editOf(hctx, t, s1, id, func(r *recipe.Recipe) { r.Name = "Less salty" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	d2, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d2.Close() })
	got, err := New(d2).GetProposal(hctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != ProposalPending || got.Kind != ProposalEdit || got.RecipeID != id || got.BaseVersion != 1 {
		t.Errorf("proposal = %+v, want a pending edit of recipe %d at version 1", got, id)
	}
	if got.Reason != "less salt" || got.Tool != "edit_recipe" || got.Person != agent.Person {
		t.Errorf("provenance = %+v, want the reason, tool and person kept", got.Provenance)
	}
	if c := got.Credential; c == nil || *c != *agent.Credential {
		t.Errorf("credential = %+v, want %+v", c, agent.Credential)
	}
	if got.Edit == nil || got.Edit.Name != "Less salty" {
		t.Errorf("proposed recipe = %+v, want the edit", got.Edit)
	}
}

// ADR-00035 obligation 7.
func TestASecondProposalSupersedesTheFirst(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	first, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "One" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Two" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProposal(ctx, first); p.State != ProposalSuperseded {
		t.Errorf("first proposal is %s, want superseded (and kept)", p.State)
	}
	if p, _ := s.GetProposal(ctx, second); p.State != ProposalPending {
		t.Errorf("second proposal is %s, want pending", p.State)
	}
	cur, ok, err := s.PendingEdit(ctx, id)
	if err != nil || !ok || cur.Token != second {
		t.Errorf("PendingEdit = %v, %v, %v; want the second proposal", cur.Token, ok, err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals WHERE state = 'pending'`); n != 1 {
		t.Errorf("%d proposals are pending, want 1", n)
	}
}

func TestProposeEditAgainstAMovedRecipeIsRefused(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	if err := s.Update(ctx, id, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Moved" })); err != nil {
		t.Fatal(err)
	}
	_, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Late" }), agent)
	if !errors.Is(err, ErrStaleVersion) {
		t.Errorf("ProposeEdit(version 1 of a recipe at 2) = %v, want ErrStaleVersion", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals`); n != 0 {
		t.Errorf("a refused proposal left %d rows", n)
	}
}

func TestProposalsBelongToTheirHousehold(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProposal(otherCtx(t), token); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's GetProposal = %v, want ErrNotFound", err)
	}
	if err := s.RejectProposal(otherCtx(t), token); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's RejectProposal = %v, want ErrNotFound", err)
	}
	if p, err := s.GetProposal(ctx, token); err != nil || p.State != ProposalPending {
		t.Errorf("the owner's proposal = %+v, %v; want it still pending", p, err)
	}
}

// A confirmed draft makes the recipe, attaches the source snapshot and records
// the recipe on the proposal, once.
func TestConfirmingADraftAttachesItsSnapshotAndRecordsTheRecipe(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProposal(ctx, token)
	if err != nil || p.Snapshot == nil || string(p.Snapshot.Content) != "2 eggs" || p.Snapshot.ParserVersion != "7" {
		t.Fatalf("pending draft = %+v, %v; want its source snapshot loaded", p, err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id IS NULL`); n != 1 {
		t.Fatalf("%d unattached snapshots, want 1", n)
	}
	rid, err := s.CreateFromDraft(ctx, token, simpleRecipe("From draft"))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.GetSnapshot(ctx, rid, SnapshotText)
	if err != nil || string(snap.Content) != "2 eggs" || snap.ParserVersion != "7" {
		t.Errorf("recipe snapshot = %+v, %v; want the draft's source", snap, err)
	}
	done, _ := s.GetProposal(ctx, token)
	if done.State != ProposalAccepted || done.ResultRecipeID != rid {
		t.Errorf("proposal = %s result %d, want accepted with recipe %d", done.State, done.ResultRecipeID, rid)
	}
	if _, err := s.CreateFromDraft(ctx, token, simpleRecipe("Again")); !errors.Is(err, ErrNotPending) {
		t.Errorf("confirming twice = %v, want ErrNotPending", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 1 {
		t.Errorf("%d recipes after a double confirm, want 1", n)
	}
}

// A confirm that fails after the recipe is written leaves the draft pending and
// its snapshot unattached.
func TestFailedConfirmLeavesTheDraftPending(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	bad := simpleRecipe("Bad")
	bad.Components[0].Lines[0].Ingredient = 999999
	if _, err := s.CreateFromDraft(ctx, token, bad); !errors.Is(err, ErrUnknownReference) {
		t.Fatalf("confirming with an unknown ingredient = %v, want ErrUnknownReference", err)
	}
	if p, _ := s.GetProposal(ctx, token); p.State != ProposalPending {
		t.Errorf("proposal is %s after a failed confirm, want pending", p.State)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id IS NULL`); n != 1 {
		t.Errorf("%d unattached snapshots after a failed confirm, want 1", n)
	}
}

// ADR-00035 obligation 12.
func TestSnapshotAttachesOnceDeletesOnlyWhileUnattachedAndNeverChanges(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	q := s.q
	hid := int64(testHouseholdID)
	tokenA, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	// A second draft of the same kind is fine: unattached snapshots do not
	// collide on (recipe_id, kind).
	tokenB, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatalf("a second unattached snapshot of one kind: %v", err)
	}
	var snapA, snapB int64
	for token, dst := range map[string]*int64{tokenA: &snapA, tokenB: &snapB} {
		if err := d.QueryRow(`SELECT snapshot_id FROM proposals WHERE token = ?`, token).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	r1, r2 := mustCreate(ctx, t, s, "One"), mustCreate(ctx, t, s, "Two")

	if err := attachSnapshot(ctx, q, hid, snapA, r1); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	if err := attachSnapshot(ctx, q, hid, snapA, r2); !errors.Is(err, ErrSnapshotAttached) {
		t.Errorf("second attach = %v, want ErrSnapshotAttached", err)
	}
	var owner int64
	if err := d.QueryRow(`SELECT recipe_id FROM source_snapshots WHERE id = ?`, snapA).Scan(&owner); err != nil || owner != r1 {
		t.Errorf("snapshot belongs to recipe %d (err %v) after a refused attach, want %d", owner, err, r1)
	}

	if err := deleteUnattachedSnapshot(ctx, q, hid, snapA); !errors.Is(err, ErrSnapshotAttached) {
		t.Errorf("deleting an attached snapshot = %v, want ErrSnapshotAttached", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE id = ?`, snapA); n != 1 {
		t.Error("an attached snapshot was deleted")
	}
	// Another household's id for it deletes nothing.
	if err := deleteUnattachedSnapshot(ctx, q, otherHouseholdID, snapB); !errors.Is(err, ErrSnapshotAttached) {
		t.Errorf("deleting household 1's snapshot as household 2 = %v, want it refused", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE id = ?`, snapB); n != 1 {
		t.Error("another household's delete removed a snapshot")
	}
	if err := deleteUnattachedSnapshot(ctx, q, hid, snapB); err != nil {
		t.Errorf("deleting an unattached snapshot: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE id = ?`, snapB); n != 0 {
		t.Error("an unattached snapshot survived its deletion")
	}

	// Deleting the recipe is the one thing that removes an attached snapshot.
	if err := s.Delete(ctx, r1); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE id = ?`, snapA); n != 0 {
		t.Error("deleting the recipe left its snapshot")
	}
}

func mustCreate(ctx context.Context, t *testing.T, s *Store, name string) int64 {
	t.Helper()
	id, err := s.Create(ctx, simpleRecipe(name))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// ADR-00035 obligation 13.
func TestSweepExpiresAtAYearAndPrunesAtSixtyDays(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	_, ids := seedCatalog(t, d)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := start
	s.now = func() time.Time { return clock }

	pendingDraft, _ := s.ProposeDraft(ctx, draftInput(0))
	rejected, _ := s.ProposeDraft(ctx, draftInput(0))
	accepted, _ := s.ProposeDraft(ctx, draftInput(0))
	rid, err := s.CreateFromDraft(ctx, accepted, simpleRecipe("Kept"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RejectProposal(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	// An edit that is superseded gets pruned at 60 days, one that is pending
	// expires.
	base := newRecipe(ctx, t, s, ids.salt)
	superseded, err := s.ProposeEdit(ctx, base, 1, editOf(ctx, t, s, base, func(r *recipe.Recipe) { r.Name = "A" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	pendingEdit, err := s.ProposeEdit(ctx, base, 1, editOf(ctx, t, s, base, func(r *recipe.Recipe) { r.Name = "B" }), agent)
	if err != nil {
		t.Fatal(err)
	}

	state := func(token string) string {
		p, err := s.GetProposal(ctx, token)
		if errors.Is(err, ErrNotFound) {
			return "deleted"
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(p.State)
	}
	sweepAt := func(at time.Time) SweepResult {
		clock = at
		res, err := s.Sweep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// Day 59: the finished ones are still here.
	sweepAt(start.Add(59 * 24 * time.Hour))
	if state(rejected) != "rejected" || state(superseded) != "superseded" {
		t.Errorf("at 59 days: rejected %s, superseded %s; want both kept", state(rejected), state(superseded))
	}
	// Day 60: the rejected and superseded ones go, with the rejected draft's
	// snapshot; the accepted one and its attached snapshot stay.
	res := sweepAt(start.Add(60 * 24 * time.Hour))
	if res.Deleted != 2 {
		t.Errorf("day 60 deleted %d, want 2", res.Deleted)
	}
	if state(rejected) != "deleted" || state(superseded) != "deleted" {
		t.Errorf("at 60 days: rejected %s, superseded %s; want both deleted", state(rejected), state(superseded))
	}
	if state(accepted) != "accepted" {
		t.Errorf("accepted proposal is %s, want accepted", state(accepted))
	}
	if _, err := s.GetSnapshot(ctx, rid, SnapshotText); err != nil {
		t.Errorf("the accepted draft's attached snapshot: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id IS NULL`); n != 1 {
		t.Errorf("%d unattached snapshots at day 60, want 1 (the pending draft's)", n)
	}

	// Day 364: pending still. Day 365: expired, as of its anniversary.
	sweepAt(start.Add(364 * 24 * time.Hour))
	if state(pendingDraft) != "pending" || state(pendingEdit) != "pending" {
		t.Errorf("at 364 days: %s and %s, want pending", state(pendingDraft), state(pendingEdit))
	}
	res = sweepAt(start.Add(365 * 24 * time.Hour))
	if res.Expired < 2 || state(pendingDraft) != "expired" || state(pendingEdit) != "expired" {
		t.Errorf("at 365 days: expired %d, %s and %s; want both expired", res.Expired, state(pendingDraft), state(pendingEdit))
	}
	// The sweep that expired them ran late; they are pruned 60 days after the
	// day they expired, not 60 days after the sweep.
	sweepAt(start.Add(365*24*time.Hour + 59*24*time.Hour))
	if state(pendingDraft) != "expired" {
		t.Errorf("an expired draft is %s on its 59th day, want kept", state(pendingDraft))
	}
	sweepAt(start.Add(365*24*time.Hour + 60*24*time.Hour))
	if state(pendingDraft) != "deleted" || state(pendingEdit) != "deleted" {
		t.Errorf("expired proposals are %s and %s on their 60th day, want deleted", state(pendingDraft), state(pendingEdit))
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id IS NULL`); n != 0 {
		t.Errorf("%d unattached snapshots after every draft finished, want 0", n)
	}
	if state(accepted) != "accepted" {
		t.Errorf("an accepted proposal is %s after all this, want accepted", state(accepted))
	}
}

// ADR-00035 obligation 14, store half.
func TestTwentyFirstPendingDraftIsRefused(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	var tokens []string
	for i := 0; i < MaxPendingDrafts; i++ {
		tok, err := s.ProposeDraft(ctx, draftInput(0))
		if err != nil {
			t.Fatalf("draft %d: %v", i+1, err)
		}
		tokens = append(tokens, tok)
	}
	if _, err := s.ProposeDraft(ctx, draftInput(0)); !errors.Is(err, ErrDraftLimit) {
		t.Fatalf("21st draft = %v, want ErrDraftLimit", err)
	} else if !strings.Contains(err.Error(), "waiting for review") {
		t.Errorf("the refusal reads %q, want it to say drafts are waiting for review", err)
	}
	for i, tok := range tokens {
		if p, err := s.GetProposal(ctx, tok); err != nil || p.State != ProposalPending {
			t.Errorf("draft %d = %v, %v; want it still pending: nothing is dropped", i+1, p.State, err)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id IS NULL`); n != MaxPendingDrafts {
		t.Errorf("%d snapshots held, want %d: a refused draft writes nothing", n, MaxPendingDrafts)
	}
	if _, err := s.ProposeDraft(otherCtx(t), draftInput(0)); err != nil {
		t.Errorf("another household's first draft: %v", err)
	}
	id := newRecipe(ctx, t, s, ids.salt)
	if _, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Edit" }), agent); err != nil {
		t.Errorf("an edit proposal at the draft limit: %v", err)
	}
	// Room is made by deciding, not by waiting: rejecting one admits one.
	if err := s.RejectProposal(ctx, tokens[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProposeDraft(ctx, draftInput(0)); err != nil {
		t.Errorf("a draft after one was rejected: %v", err)
	}
}

// ADR-00035 obligation 16: a merge re-points the proposals that name the loser,
// of both kinds, and they still load.
func TestMergeRepointsProposals(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	s.RegisterMergeHooks(c)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	edit, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) {
		r.Components[0].Lines = append(r.Components[0].Lines, recipe.IngredientLine{Ingredient: recipe.IngredientID(ids.butter), Rule: quantity.RuleLinear, Raw: "butter"})
	}), agent)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProposeDraft(ctx, draftInput(ids.salt))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.MergeIngredients(ctx, ids.butter, ids.salt); err != nil {
		t.Fatalf("MergeIngredients: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposal_ingredients WHERE ingredient_id = ?`, ids.salt); n != 0 {
		t.Errorf("%d proposal index rows still name the merged ingredient", n)
	}
	got, err := s.GetProposal(ctx, edit)
	if err != nil {
		t.Fatalf("the edit proposal no longer loads: %v", err)
	}
	for _, l := range got.Edit.Components[0].Lines {
		if l.Ingredient != recipe.IngredientID(ids.butter) {
			t.Errorf("proposed line names ingredient %d, want the winner %d", l.Ingredient, ids.butter)
		}
	}
	dp, err := s.GetProposal(ctx, draft)
	if err != nil {
		t.Fatalf("the draft proposal no longer loads: %v", err)
	}
	if !strings.Contains(string(dp.Document), fmt.Sprintf(`"ingredient":%d`, ids.butter)) || strings.Contains(string(dp.Document), fmt.Sprintf(`"ingredient":%d,`, ids.salt)) {
		t.Errorf("draft document = %s, want it to name the winner", dp.Document)
	}
	if !strings.Contains(string(dp.Document), `"extra":{"kept":true}`) || !strings.Contains(string(dp.Document), `"confidence":0.5`) {
		t.Errorf("draft document = %s, want the fields the store does not read left alone", dp.Document)
	}
	// The ingredient is gone, so a proposal that kept naming it could not be
	// stored, and the index agrees with the documents.
	if n := count(t, d, `SELECT COUNT(*) FROM proposal_ingredients WHERE ingredient_id = ?`, ids.butter); n != 2 {
		t.Errorf("%d proposal index rows name the winner, want 2", n)
	}
}

func TestProposalNamingAnInvisibleIngredientIsRefused(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	_ = c
	if _, err := s.ProposeDraft(otherCtx(t), draftInput(ids.salt)); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("a draft naming another household's ingredient = %v, want ErrUnknownReference", err)
	}
}

func TestDraftReplaceAndRejectOnlyWhilePending(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDraft(ctx, token, draftDoc(ids.salt)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposal_ingredients WHERE ingredient_id = ?`, ids.salt); n != 1 {
		t.Errorf("replaced draft indexes %d rows for its ingredient, want 1", n)
	}
	if err := s.RejectProposal(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDraft(ctx, token, draftDoc(0)); !errors.Is(err, ErrNotPending) {
		t.Errorf("replacing a rejected draft = %v, want ErrNotPending", err)
	}
	if err := s.RejectProposal(ctx, token); !errors.Is(err, ErrNotPending) {
		t.Errorf("rejecting twice = %v, want ErrNotPending", err)
	}
	if err := s.RejectProposal(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rejecting an unknown token = %v, want ErrNotFound", err)
	}
}

func TestProposalNeedsAPersonAndAKnownSnapshotKind(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	in := draftInput(0)
	in.Person = ""
	if _, err := s.ProposeDraft(ctx, in); !errors.Is(err, ErrInvalid) {
		t.Errorf("a draft with no person = %v, want ErrInvalid", err)
	}
	in = draftInput(0)
	in.Source.Kind = "carrier pigeon"
	if _, err := s.ProposeDraft(ctx, in); !errors.Is(err, ErrInvalid) {
		t.Errorf("a draft with an unknown snapshot kind = %v, want ErrInvalid", err)
	}
	in = draftInput(0)
	in.Document = []byte("not json")
	if _, err := s.ProposeDraft(ctx, in); !errors.Is(err, ErrInvalid) {
		t.Errorf("a draft that is not JSON = %v, want ErrInvalid", err)
	}
	if _, err := s.ProposeDraft(noHouseholdCtx(t), draftInput(0)); !errors.Is(err, ErrNoHousehold) {
		t.Errorf("a draft with no household = %v, want ErrNoHousehold", err)
	}
}

// Deleting a recipe deletes the proposals about it and the draft that made it
// (ADR-00035: an accepted proposal lives as long as its recipe, no longer).
func TestDeletingARecipeDeletesItsProposals(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	if _, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Edit" }), agent); err != nil {
		t.Fatal(err)
	}
	draft, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	made, err := s.CreateFromDraft(ctx, draft, simpleRecipe("From a draft"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, made); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals`); n != 1 {
		t.Errorf("%d proposals left after both recipes went, want only the draft nobody confirmed", n)
	}
	if p, err := s.GetProposal(ctx, other); err != nil || p.State != ProposalPending {
		t.Errorf("the unrelated draft = %v, %v; want it untouched", p.State, err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposal_ingredients WHERE proposal_id NOT IN (SELECT id FROM proposals)`); n != 0 {
		t.Errorf("%d index rows outlived their proposals", n)
	}
}

// A draft whose snapshot is gone is a broken row. Confirming it must not store
// a recipe with no source, which Save's rule forbids.
func TestConfirmingADraftWithNoSnapshotStoresNothing(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	token, err := s.ProposeDraft(ctx, draftInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE proposals SET snapshot_id = NULL WHERE token = ?`, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFromDraft(ctx, token, simpleRecipe("No source")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("confirming a draft with no snapshot = %v, want ErrCorrupt", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("%d recipes stored with no source snapshot", n)
	}
	if p, _ := s.GetProposal(ctx, token); p.State != ProposalPending {
		t.Errorf("proposal is %s after the refusal, want pending", p.State)
	}
}

// ADR-00035 obligation 14 under contention: many requests racing for a
// household's last places get exactly the places there are.
func TestTheDraftCapHoldsUnderConcurrentRequests(t *testing.T) {
	s, d := newTestStore(t)
	ctx := otherCtx(t)
	const racers = 25
	var wg sync.WaitGroup
	errs := make([]error, racers)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.ProposeDraft(ctx, draftInput(0))
		}()
	}
	wg.Wait()
	var ok, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrDraftLimit):
			refused++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != MaxPendingDrafts || refused != racers-MaxPendingDrafts {
		t.Errorf("%d succeeded and %d were refused, want %d and %d", ok, refused, MaxPendingDrafts, racers-MaxPendingDrafts)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals WHERE household_id = ? AND state = 'pending'`, otherHouseholdID); n != MaxPendingDrafts {
		t.Errorf("%d drafts pending, want %d", n, MaxPendingDrafts)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE household_id = ?`, otherHouseholdID); n != MaxPendingDrafts {
		t.Errorf("%d snapshots written, want %d: a refused draft writes none", n, MaxPendingDrafts)
	}
}

func TestReplacingAnEditProposalAsADraftIsInvalidNotStale(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	tok, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "E" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDraft(ctx, tok, draftDoc(0)); !errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotPending) {
		t.Errorf("ReplaceDraft on an edit proposal = %v, want ErrInvalid", err)
	}
}
