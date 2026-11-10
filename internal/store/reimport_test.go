package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// reimportDoc is a draft document that re-imports recipe id from version base.
func reimportDoc(ing, id, base int64) []byte {
	return []byte(fmt.Sprintf(`{"name":"Fresh","reimport":{"recipe":%d,"base_version":%d},"components":[{"lines":[{"raw":"x","ingredient":%d,"confidence":0.5}]}]}`, id, base, ing))
}

func reimportInput(ing, id, base int64) DraftInput {
	in := draftInput(ing)
	in.Document = reimportDoc(ing, id, base)
	in.Source.Content = []byte("<html>fetched again</html>")
	in.Source.Kind = SnapshotHTML
	in.Reason = "Re-imported from example.test"
	return in
}

// recipeWithSource stores a recipe with a first source snapshot.
func recipeWithSource(ctx context.Context, t *testing.T, s *Store, ing int64) int64 {
	t.Helper()
	r, err := s.Get(ctx, newRecipe(ctx, t, s, ing))
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateWithSnapshots(ctx, r, SnapshotInput{Kind: SnapshotHTML, Content: []byte("<html>first import</html>"), ParserVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type fakeRemover struct {
	mu   sync.Mutex
	keys []string
}

func (f *fakeRemover) Remove(_ context.Context, keys []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, keys...)
}

func addCandidate(t *testing.T, d *sql.DB, token, key string) {
	t.Helper()
	_, err := d.Exec(`INSERT INTO draft_candidates (household_id, proposal_id, offer, position, source_url, key, width, height)
		SELECT household_id, id, lower(hex(randomblob(11))), 1, 'https://img.test/a.jpg', ?, 1066, 300 FROM proposals WHERE token = ?`, key, token)
	if err != nil {
		t.Fatal(err)
	}
}

// ADR-00042 obligation 2 and 5: saving a re-import writes a revision of the
// existing recipe, creates no second one, attaches the new snapshot beside the
// first and leaves the first as it was.
func TestAcceptReimportReplacesTheRecipeInPlace(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	if _, err := s.AddNote(ctx, id, "less salt next time", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(ctx, id, MadeIt{On: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Comment: "good"}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	before.Tags, before.Rating = []string{"weeknight"}, ptr(4)
	if err := s.Update(ctx, id, before); err != nil {
		t.Fatal(err)
	}
	var firstSnap string
	if err := d.QueryRow(`SELECT sha256 FROM source_snapshots WHERE recipe_id = ?`, id).Scan(&firstSnap); err != nil {
		t.Fatal(err)
	}
	recipes := count(t, d, `SELECT COUNT(*) FROM recipes`)

	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 2))
	if err != nil {
		t.Fatal(err)
	}
	next := before
	next.Name, next.Tags, next.Rating = "Fresh", nil, nil
	got, err := s.AcceptReimport(ctx, token, next)
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("AcceptReimport returned recipe %d, want %d", got, id)
	}

	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != recipes {
		t.Errorf("%d recipes after the save, want %d: a re-import creates none", n, recipes)
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Name != "Fresh" {
		t.Errorf("name = %q, want the re-imported one", cur.Name)
	}
	if len(cur.Tags) != 1 || cur.Tags[0] != "weeknight" || cur.Rating == nil || *cur.Rating != 4 {
		t.Errorf("tags %v, rating %v; want the household's own kept", cur.Tags, cur.Rating)
	}
	if notes, _ := s.ListNotes(ctx, id); len(notes) != 1 {
		t.Errorf("%d notes, want the journal kept", len(notes))
	}
	if made, _ := s.ListMadeIt(ctx, id); len(made) != 1 {
		t.Errorf("%d made-it entries, want them kept", len(made))
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = ?`, id); n != 2 {
		t.Errorf("%d snapshots attached, want the first and the new one", n)
	}
	if snap, err := s.GetSnapshot(ctx, id, SnapshotHTML); err != nil || string(snap.Content) != "<html>fetched again</html>" {
		t.Errorf("single-snapshot read = %q (%v), want the newest of the kind", snap.Content, err)
	}
	if kinds, err := s.ListSnapshotKinds(ctx, id); err != nil || len(kinds) != 1 {
		t.Errorf("kinds = %v (%v), want html once", kinds, err)
	}
	var stillFirst string
	if err := d.QueryRow(`SELECT sha256 FROM source_snapshots WHERE recipe_id = ? ORDER BY id LIMIT 1`, id).Scan(&stillFirst); err != nil || stillFirst != firstSnap {
		t.Errorf("first snapshot hash = %q (%v), want it unchanged: %q", stillFirst, err, firstSnap)
	}
	var content string
	if err := d.QueryRow(`SELECT content FROM source_snapshots WHERE recipe_id = ? ORDER BY id LIMIT 1`, id).Scan(&content); err != nil || content != "<html>first import</html>" {
		t.Errorf("first snapshot content = %q (%v), want it unchanged", content, err)
	}

	// ADR-00042 obligation 11: the revision has the proposal change kind and
	// History can name the re-import.
	var kind string
	if err := d.QueryRow(`SELECT change_kind FROM revisions WHERE recipe_id = ? AND version = 2`, id).Scan(&kind); err != nil || kind != "proposal" {
		t.Errorf("revision change kind = %q (%v), want proposal", kind, err)
	}
	revs, err := s.ListRevisions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	last := revs[len(revs)-1]
	if last.Proposal == nil || last.Proposal.Reason != "Re-imported from example.test" || last.Author != "dev@localhost" {
		t.Errorf("revision = %+v, want the re-import's provenance and the saving person", last)
	}
	if p, _ := s.GetProposal(ctx, token); p.State != ProposalAccepted || p.ResultRecipeID != id {
		t.Errorf("draft = %s for recipe %d, want accepted for %d", p.State, p.ResultRecipeID, id)
	}

	// The previous version restores from history.
	if err := s.Restore(ctx, id, 2); err != nil {
		t.Fatalf("restore the version before the re-import: %v", err)
	}
	if back, _ := s.Get(ctx, id); back.Name != "Base" {
		t.Errorf("restored name = %q, want Base", back.Name)
	}
	if _, err := s.AcceptReimport(ctx, token, next); !errors.Is(err, ErrNotPending) {
		t.Errorf("saving the same re-import twice = %v, want ErrNotPending", err)
	}
}

// ADR-00042 obligation 6: a recipe edited after the re-import started refuses
// the save and stays as the editor left it.
func TestAcceptReimportRefusesARecipeThatMovedOn(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	edited := editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Edited meanwhile" })
	if err := s.Update(ctx, id, edited); err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptReimport(ctx, token, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Fresh" }))
	if !errors.Is(err, ErrStaleProposal) || !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("AcceptReimport after an edit = %v, want ErrStaleProposal (an ErrStaleVersion)", err)
	}
	if cur, _ := s.Get(ctx, id); cur.Name != "Edited meanwhile" {
		t.Errorf("name = %q, want the edit untouched", cur.Name)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = ?`, id); n != 1 {
		t.Errorf("%d snapshots attached, want only the first", n)
	}
	p, _ := s.GetProposal(ctx, token)
	if p.State != ProposalPending || !p.StaleAt(2) {
		t.Errorf("draft is %s, stale at 2 = %v; want pending and stale, for the banner", p.State, p.StaleAt(2))
	}
}

// ADR-00042 obligation 4: a recipe has one pending proposal, an edit or a
// re-import, and the newer supersedes the older either way round. The
// superseded re-import's candidate objects go with it.
func TestReimportAndEditSupersedeEachOther(t *testing.T) {
	s, d := newTestStore(t)
	gone := &fakeRemover{}
	s = s.WithObjects(gone)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)

	edit, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Agent" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProposal(ctx, edit); p.State != ProposalSuperseded {
		t.Errorf("agent edit is %s after a re-import, want superseded", p.State)
	}
	addCandidate(t, d, first, "drafts/9/1.webp")
	pend, found, err := s.PendingReimport(ctx, id)
	if err != nil || !found || pend.Token != first || pend.Reimport == nil || pend.Reimport.BaseVersion != 1 {
		t.Fatalf("PendingReimport = %+v, %v, %v; want the first re-import at version 1", pend, found, err)
	}
	if _, found, _ := s.PendingEdit(ctx, id); found {
		t.Error("PendingEdit still finds the superseded edit")
	}

	agent2, err := s.ProposeEdit(ctx, id, 1, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Agent again" }), agent)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProposal(ctx, first); p.State != ProposalSuperseded {
		t.Errorf("re-import is %s after an agent edit, want superseded", p.State)
	}
	if len(gone.keys) != 1 || gone.keys[0] != "drafts/9/1.webp" {
		t.Errorf("objects removed = %v, want the superseded re-import's candidate", gone.keys)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM draft_candidates`); n != 0 {
		t.Errorf("%d candidate rows left, want none", n)
	}
	if _, found, _ := s.PendingReimport(ctx, id); found {
		t.Error("PendingReimport finds a superseded draft")
	}

	second, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProposal(ctx, agent2); p.State != ProposalSuperseded {
		t.Errorf("second agent edit is %s, want superseded by the re-import", p.State)
	}
	if p, _ := s.GetProposal(ctx, second); p.State != ProposalSuperseded {
		t.Errorf("second re-import is %s, want superseded by the third", p.State)
	}
	if p, _ := s.GetProposal(ctx, third); p.State != ProposalPending {
		t.Errorf("third re-import is %s, want pending", p.State)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals WHERE state = 'pending'`); n != 1 {
		t.Errorf("%d proposals pending, want 1", n)
	}
}

func TestReimportsAreNotCountedAgainstTheDraftLimit(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	for range MaxPendingDrafts {
		if _, err := s.ProposeDraft(ctx, draftInput(ids.salt)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ProposeDraft(ctx, draftInput(ids.salt)); !errors.Is(err, ErrDraftLimit) {
		t.Fatalf("21st draft = %v, want ErrDraftLimit", err)
	}
	if _, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1)); err != nil {
		t.Fatalf("a re-import with %d drafts waiting = %v, want it held", MaxPendingDrafts, err)
	}
	if n, _ := s.PendingDrafts(ctx); n != MaxPendingDrafts {
		t.Errorf("PendingDrafts = %d, want the re-import not counted", n)
	}
}

func TestProposeReimportRefusals(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	if _, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 5)); !errors.Is(err, ErrStaleVersion) {
		t.Errorf("re-import from a version the recipe is not at = %v, want ErrStaleVersion", err)
	}
	if _, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id+99, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-import of a missing recipe = %v, want ErrNotFound", err)
	}
	if _, err := s.ProposeDraft(otherCtx(t), reimportInput(ids.salt, id, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-import of another household's recipe = %v, want ErrNotFound", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM proposals`); n != 0 {
		t.Errorf("%d proposals stored by refused re-imports, want none", n)
	}
}

func TestAReimportDraftIsNotConfirmedAsANewRecipe(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFromDraft(ctx, token, editOf(ctx, t, s, id, func(*recipe.Recipe) {})); !errors.Is(err, ErrInvalid) {
		t.Errorf("CreateFromDraft of a re-import = %v, want ErrInvalid", err)
	}
	if err := s.ReplaceDraft(ctx, token, draftDoc(ids.salt)); !errors.Is(err, ErrInvalid) {
		t.Errorf("ReplaceDraft dropping the marker = %v, want ErrInvalid", err)
	}
	if err := s.ReplaceDraft(ctx, token, reimportDoc(ids.salt, id, 1)); err != nil {
		t.Errorf("ReplaceDraft keeping the marker = %v, want nil", err)
	}
	other, err := s.ProposeDraft(ctx, draftInput(ids.salt))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDraft(ctx, other, reimportDoc(ids.salt, id, 1)); !errors.Is(err, ErrInvalid) {
		t.Errorf("ReplaceDraft turning a new-recipe draft into a re-import = %v, want ErrInvalid", err)
	}
	if _, err := s.AcceptReimport(ctx, other, editOf(ctx, t, s, id, func(*recipe.Recipe) {})); !errors.Is(err, ErrInvalid) {
		t.Errorf("AcceptReimport of a new-recipe draft = %v, want ErrInvalid", err)
	}
}

// Deleting the recipe ends its re-import, with its candidate objects.
func TestDeletingARecipeEndsItsReimport(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	addCandidate(t, d, token, "drafts/4/1.webp")
	gone := &removingReaper{}
	if err := s.DeleteWithPicture(ctx, id, gone); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetProposal(ctx, token); p.State != ProposalSuperseded {
		t.Errorf("re-import of a deleted recipe is %s, want superseded", p.State)
	}
	if len(gone.removed) != 1 || gone.removed[0] != "drafts/4/1.webp" {
		t.Errorf("objects removed = %v, want the candidate", gone.removed)
	}
	if _, err := s.AcceptReimport(ctx, token, recipe.Recipe{Name: "x", Components: []recipe.Component{{}}}); !errors.Is(err, ErrNotPending) {
		t.Errorf("saving the re-import of a deleted recipe = %v, want ErrNotPending", err)
	}
}

type removingReaper struct{ removed []string }

func (r *removingReaper) CollectRecipe(context.Context, *sql.Tx, int64, int64) ([]string, error) {
	return nil, nil
}

func (r *removingReaper) Remove(_ context.Context, keys []string) {
	r.removed = append(r.removed, keys...)
}

// A share copies every snapshot of a recipe, the original first (ADR-00042).
func TestShareCopiesEverySnapshotOfAReimportedRecipe(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptReimport(ctx, token, editOf(ctx, t, s, id, func(r *recipe.Recipe) { r.Name = "Fresh" })); err != nil {
		t.Fatal(err)
	}
	snaps, err := shareSnapshots(ctx, s.q, testHouseholdID, id)
	if err != nil || !snaps.Valid {
		t.Fatalf("shareSnapshots = %v, %v", snaps, err)
	}
	var got []shareSnapshot
	if err := json.Unmarshal([]byte(snaps.String), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got[0].Content) != "<html>first import</html>" || string(got[1].Content) != "<html>fetched again</html>" {
		t.Errorf("shared snapshots = %+v, want the original then the re-import", got)
	}
}

// A share document holding a snapshot kind the store does not know is stored
// data gone bad, not a caller's bad input.
func TestSharedSnapshotOfAnUnknownKindIsCorrupt(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	body, err := json.Marshal([]shareSnapshot{{Kind: "carrier_pigeon", Content: []byte("x")}})
	if err != nil {
		t.Fatal(err)
	}
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		return s.insertSharedSnapshots(ctx, q, testHouseholdID, id, sql.NullString{String: string(body), Valid: true}, "dev@localhost")
	})
	if !errors.Is(err, ErrCorrupt) || errors.Is(err, ErrInvalid) {
		t.Errorf("unknown snapshot kind in a share = %v, want ErrCorrupt and not ErrInvalid", err)
	}
}

// FirstSnapshot reads the original snapshot of a kind, which a saved re-import
// adds to and never replaces (ADR-00042), with its parser version; a recipe
// with no snapshot of the kind is not found, and a missing recipe is
// ErrNotFound (ADR-00048, Preview).
func TestFirstSnapshotIsTheOriginalAfterAReimport(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := recipeWithSource(ctx, t, s, ids.salt)
	token, err := s.ProposeDraft(ctx, reimportInput(ids.salt, id, 1))
	if err != nil {
		t.Fatal(err)
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptReimport(ctx, token, cur); err != nil {
		t.Fatal(err)
	}
	if latest, err := s.GetSnapshot(ctx, id, SnapshotHTML); err != nil || string(latest.Content) != "<html>fetched again</html>" {
		t.Fatalf("latest snapshot = %q (%v), want the re-import's", latest.Content, err)
	}
	first, found, err := s.FirstSnapshot(ctx, id, SnapshotHTML)
	if err != nil || !found {
		t.Fatalf("FirstSnapshot = %v, found %v", err, found)
	}
	if snap := first.Snapshot; string(snap.Content) != "<html>first import</html>" || snap.ParserVersion != "1" || snap.Kind != SnapshotHTML {
		t.Errorf("first snapshot = %q at parser %q, want the original at parser 1", snap.Content, snap.ParserVersion)
	}
	// It was stored with its recipe, not as a draft's source, so no draft
	// document goes with it.
	if first.DraftDocument != nil {
		t.Errorf("draft document = %s, want none for a snapshot no proposal names", first.DraftDocument)
	}
	if _, found, err := s.FirstSnapshot(ctx, id, SnapshotOCR); err != nil || found {
		t.Errorf("FirstSnapshot(no such kind) = found %v, %v; want not found and no error", found, err)
	}
	if _, _, err := s.FirstSnapshot(ctx, id+50, SnapshotHTML); !errors.Is(err, ErrNotFound) {
		t.Errorf("FirstSnapshot(missing recipe) = %v, want ErrNotFound", err)
	}
}

// A recipe whose first HTML snapshot came from a saved re-import gets the
// re-import draft's document with it, which names the page's address
// (ADR-00048, Preview).
func TestFirstSnapshotCarriesTheDraftThatHeldIt(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	id := newRecipe(ctx, t, s, ids.salt)
	in := reimportInput(ids.salt, id, 1)
	token, err := s.ProposeDraft(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptReimport(ctx, token, cur); err != nil {
		t.Fatal(err)
	}
	first, found, err := s.FirstSnapshot(ctx, id, SnapshotHTML)
	if err != nil || !found {
		t.Fatalf("FirstSnapshot = %v, found %v", err, found)
	}
	if string(first.Snapshot.Content) != "<html>fetched again</html>" || string(first.DraftDocument) != string(in.Document) {
		t.Errorf("first source = %q with document %s, want the re-import's page and document", first.Snapshot.Content, first.DraftDocument)
	}
}
