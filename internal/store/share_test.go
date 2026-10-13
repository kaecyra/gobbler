package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// shareFixture is household A (testHouseholdID) with a recipe that uses one
// central ingredient and one ingredient of A's own overlay, a note, a made-it
// entry, a rating and a source snapshot, plus household B to send it to.
type shareFixture struct {
	s       *Store
	cat     *catalog.Store
	a, b    context.Context
	recipe  int64
	central int64 // a central ingredient, used by the recipe
	own     int64 // an ingredient in A's overlay, used by the recipe
}

const ownName = "zz sender only fruit"

func newShareFixture(t *testing.T) shareFixture {
	t.Helper()
	s, d := newTestStore(t)
	f := shareFixture{s: s, cat: catalog.New(d), a: authedCtx(t), b: otherCtx(t)}
	central, err := f.cat.CreateIngredient(context.Background(), catalog.Ingredient{Name: "zz central salt", Aisle: "test", Reviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	own, err := f.cat.CreateIngredient(f.a, catalog.Ingredient{Name: ownName, Aisle: "test", Aliases: []string{"zz sender alias"}})
	if err != nil {
		t.Fatal(err)
	}
	f.central, f.own = central.ID, own.ID
	r := recipe.Recipe{
		Name:   "Shared Stew",
		Rating: ptr(5),
		Tags:   []string{"supper"},
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{
				{Amount: quantity.Amount{Qty: qty(t, "1"), Unit: quantity.MustUnit("cup")}, Ingredient: recipe.IngredientID(central.ID), Rule: quantity.RuleLinear, Raw: "1 cup salt"},
				{Amount: quantity.Amount{Qty: qty(t, "2")}, Ingredient: recipe.IngredientID(own.ID), Rule: quantity.RuleLinear, Raw: "2 sender only fruit"},
				{Amount: quantity.Amount{Qty: qty(t, "3")}, Ingredient: recipe.IngredientID(own.ID), Rule: quantity.RuleLinear, Raw: "3 more sender only fruit"},
			},
			Steps: []recipe.Step{{ID: "s1", Text: "Stew it."}},
		}},
	}
	f.recipe, err = s.CreateWithSnapshots(f.a, r,
		SnapshotInput{Kind: SnapshotHTML, Content: []byte("<html>original \x00\xff bytes</html>")},
		SnapshotInput{Kind: SnapshotText, Content: []byte("pasted text")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(f.a, f.recipe, "sender's private note"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMadeIt(f.a, f.recipe, MadeIt{Comment: "sender's private comment"}); err != nil {
		t.Fatal(err)
	}
	edited := r
	edited.Description = "edited once so a revision exists"
	if err := s.Update(f.a, f.recipe, edited); err != nil {
		t.Fatal(err)
	}
	return f
}

// send sends the fixture's recipe from A to B and returns the share id.
func (f shareFixture) send(t *testing.T) int64 {
	t.Helper()
	id, err := f.s.SendShare(f.a, f.recipe, otherHouseholdID)
	if err != nil {
		t.Fatalf("SendShare: %v", err)
	}
	return id
}

// bFruit is the ingredient B's catalog resolves the sender's overlay name to.
func (f shareFixture) bFruit(t *testing.T) int64 {
	t.Helper()
	in, err := f.cat.CreateIngredient(f.b, catalog.Ingredient{Name: "zz recipient fruit", Aisle: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return in.ID
}

func TestShareReachesOnlyTheRecipientAndOnlyAsAnOffer(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)

	// A itself has no inbox entry; B has the offer, described without ids.
	if got, err := f.s.PendingShares(f.a); err != nil || len(got) != 0 {
		t.Fatalf("sender's inbox = %v, %v; want empty", got, err)
	}
	offers, err := f.s.PendingShares(f.b)
	if err != nil || len(offers) != 1 {
		t.Fatalf("recipient's inbox = %v, %v; want one offer", offers, err)
	}
	o := offers[0]
	if o.ID != id || o.SenderName != "Household 1" || o.RecipeName != "Shared Stew" {
		t.Errorf("offer = %+v", o)
	}
	if len(o.Ingredients) != 1 || o.Ingredients[0] != ownName {
		t.Errorf("names to resolve = %v, want the overlay name once", o.Ingredients)
	}
	// Nothing entered B's recipes before it accepted.
	if all, err := f.s.Search(f.b, SearchQuery{}); err != nil || len(all) != 0 {
		t.Errorf("recipient's recipes before accepting = %d (%v), want 0", len(all), err)
	}
}

func TestAcceptedShareIsAnIndependentCopyWithoutTheSendersPrivateData(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)
	fruit := f.bFruit(t)

	copyID, err := f.s.AcceptShare(f.b, id, map[string]int64{ownName: fruit})
	if err != nil {
		t.Fatalf("AcceptShare: %v", err)
	}

	got, err := f.s.Get(f.b, copyID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Shared Stew" || got.Rating != nil || len(got.Tags) != 1 {
		t.Errorf("copy = name %q rating %v tags %v; want the document without the rating", got.Name, got.Rating, got.Tags)
	}
	lines := got.Components[0].Lines
	if int64(lines[0].Ingredient) != f.central {
		t.Errorf("central line points at %d, want the central entry %d", lines[0].Ingredient, f.central)
	}
	for _, i := range []int{1, 2} {
		if int64(lines[i].Ingredient) != fruit || lines[i].Ingredient == recipe.IngredientID(f.own) {
			t.Errorf("overlay line %d points at %d, want the recipient's %d and never the sender's %d", i, lines[i].Ingredient, fruit, f.own)
		}
	}
	if lines[1].Raw != "2 sender only fruit" || lines[0].Amount.Unit.Name != "cup" {
		t.Errorf("lines changed in the copy: %+v", lines)
	}

	// The snapshots arrive byte for byte; the sender's notes, revisions and
	// made-it log do not. One note says where the copy came from.
	for kind, want := range map[SnapshotKind]string{SnapshotHTML: "<html>original \x00\xff bytes</html>", SnapshotText: "pasted text"} {
		snap, err := f.s.GetSnapshot(f.b, copyID, kind)
		if err != nil || string(snap.Content) != want {
			t.Errorf("%s snapshot = %q (%v), want %q", kind, snap.Content, err, want)
		}
	}
	notes, err := f.s.ListNotes(f.b, copyID)
	if err != nil || len(notes) != 1 || !strings.HasPrefix(notes[0].Body, "Shared by Household 1 on 2026-03-01") {
		t.Errorf("copy's notes = %+v (%v), want only the provenance note", notes, err)
	}
	if m, err := f.s.ListMadeIt(f.b, copyID); err != nil || len(m) != 0 {
		t.Errorf("copy's made-it log = %v (%v), want empty", m, err)
	}
	if r, err := f.s.ListRevisions(f.b, copyID); err != nil || len(r) != 0 {
		t.Errorf("copy's revisions = %v (%v), want none", r, err)
	}
	if info, err := f.s.Info(f.b, copyID); err != nil || info.Version != 1 || info.ClonedFrom != nil {
		t.Errorf("copy's info = %+v (%v), want version 1 and no clone origin", info, err)
	}

	// Edits on either side stay on that side.
	edited := got
	edited.Name = "Recipient's Stew"
	if err := f.s.Update(f.b, copyID, edited); err != nil {
		t.Fatal(err)
	}
	senders, err := f.s.Get(f.a, f.recipe)
	if err != nil || senders.Name != "Shared Stew" || senders.Rating == nil {
		t.Errorf("sender's recipe after the recipient's edit = %q rating %v (%v)", senders.Name, senders.Rating, err)
	}
	senders.Name = "Sender's Stew"
	if err := f.s.Update(f.a, f.recipe, senders); err != nil {
		t.Fatal(err)
	}
	if again, err := f.s.Get(f.b, copyID); err != nil || again.Name != "Recipient's Stew" {
		t.Errorf("recipient's recipe after the sender's edit = %q (%v)", again.Name, err)
	}
}

func TestShareIsTheRecipeAsSentNotAsItBecomes(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)
	r, err := f.s.Get(f.a, f.recipe)
	if err != nil {
		t.Fatal(err)
	}
	r.Name = "Changed after sending"
	if err := f.s.Update(f.a, f.recipe, r); err != nil {
		t.Fatal(err)
	}
	copyID, err := f.s.AcceptShare(f.b, id, map[string]int64{ownName: f.bFruit(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.Get(f.b, copyID); err != nil || got.Name != "Shared Stew" {
		t.Errorf("accepted name = %q (%v), want the name at send time", got.Name, err)
	}
}

func TestShareCanBeAcceptedOrDeclinedOnce(t *testing.T) {
	f := newShareFixture(t)
	fruit := f.bFruit(t)
	names := map[string]int64{ownName: fruit}

	id := f.send(t)
	if _, err := f.s.AcceptShare(f.b, id, names); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AcceptShare(f.b, id, names); !errors.Is(err, ErrNotFound) {
		t.Errorf("second accept = %v, want ErrNotFound", err)
	}
	if err := f.s.DeclineShare(f.b, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("decline after accept = %v, want ErrNotFound", err)
	}
	if all, err := f.s.Search(f.b, SearchQuery{}); err != nil || len(all) != 1 {
		t.Errorf("recipient has %d recipes (%v), want exactly the one copy", len(all), err)
	}

	// Sending again sends a new copy, which can be declined.
	second := f.send(t)
	if second == id {
		t.Fatal("a second send reused the share")
	}
	if err := f.s.DeclineShare(f.b, second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AcceptShare(f.b, second, names); !errors.Is(err, ErrNotFound) {
		t.Errorf("accept after decline = %v, want ErrNotFound", err)
	}
	if _, err := f.s.PendingShare(f.b, second); !errors.Is(err, ErrNotFound) {
		t.Errorf("a declined share is still pending: %v", err)
	}
	if offers, err := f.s.PendingShares(f.b); err != nil || len(offers) != 0 {
		t.Errorf("inbox after resolving everything = %v (%v)", offers, err)
	}
	if all, err := f.s.Search(f.b, SearchQuery{}); err != nil || len(all) != 1 {
		t.Errorf("declining added a recipe: %d recipes (%v)", len(all), err)
	}
}

func TestAcceptNeedsEveryNameResolvedInTheRecipientsOwnCatalog(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)

	for name, ids := range map[string]map[string]int64{
		"no name given":                    {},
		"the sender's own overlay entry":   {ownName: f.own},
		"an id that does not exist at all": {ownName: 999999},
	} {
		if _, err := f.s.AcceptShare(f.b, id, ids); !errors.Is(err, ErrUnknownReference) {
			t.Errorf("%s: accept = %v, want ErrUnknownReference", name, err)
		}
	}
	// Every failed attempt left the share pending and created nothing.
	if _, err := f.s.PendingShare(f.b, id); err != nil {
		t.Errorf("share after failed accepts: %v", err)
	}
	if all, err := f.s.Search(f.b, SearchQuery{}); err != nil || len(all) != 0 {
		t.Errorf("failed accepts left %d recipes (%v)", len(all), err)
	}
}

func TestAnotherHouseholdCannotSeeAcceptOrDeclineAShare(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)
	third, err := household.New(f.s.db).Create(context.Background(), "Third", "third@e2e.example")
	if err != nil {
		t.Fatal(err)
	}
	c := household.WithContext(identityCtx(t), third)

	if offers, err := f.s.PendingShares(c); err != nil || len(offers) != 0 {
		t.Errorf("third household's inbox = %v (%v)", offers, err)
	}
	if _, err := f.s.PendingShare(c, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("PendingShare = %v, want ErrNotFound", err)
	}
	if _, err := f.s.AcceptShare(c, id, map[string]int64{ownName: f.central}); !errors.Is(err, ErrNotFound) {
		t.Errorf("AcceptShare = %v, want ErrNotFound", err)
	}
	if err := f.s.DeclineShare(c, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeclineShare = %v, want ErrNotFound", err)
	}
	// The sender cannot accept its own outgoing copy either.
	if err := f.s.DeclineShare(f.a, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("sender's DeclineShare = %v, want ErrNotFound", err)
	}
	if _, err := f.s.PendingShare(f.b, id); err != nil {
		t.Errorf("recipient lost the share: %v", err)
	}
}

func TestSendShareRefusesWhatItCannotSend(t *testing.T) {
	f := newShareFixture(t)
	if _, err := f.s.SendShare(f.a, f.recipe, testHouseholdID); !errors.Is(err, ErrInvalid) {
		t.Errorf("send to own household = %v, want ErrInvalid", err)
	}
	if _, err := f.s.SendShare(f.b, f.recipe, testHouseholdID); !errors.Is(err, ErrNotFound) {
		t.Errorf("send a recipe of another household = %v, want ErrNotFound", err)
	}
	if _, err := f.s.SendShare(noHouseholdCtx(t), f.recipe, otherHouseholdID); !errors.Is(err, ErrNoHousehold) {
		t.Errorf("send with no household = %v, want ErrNoHousehold", err)
	}
	if got, err := f.s.PendingShares(f.b); err != nil || len(got) != 0 {
		t.Errorf("refused sends left %v (%v)", got, err)
	}
}

// The stored share names the sender's overlay ingredient only by name: no id of
// it, and not its aliases (ADR-00018: the sender's overlay is never exposed).
func TestStoredShareHoldsNoSenderOverlayIdOrAlias(t *testing.T) {
	f := newShareFixture(t)
	id := f.send(t)
	var doc string
	if err := f.s.db.QueryRow(`SELECT recipe_document FROM recipe_shares WHERE id = ?`, id).Scan(&doc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "zz sender alias") {
		t.Errorf("the share carries the sender's alias: %s", doc)
	}
	var d shareDoc
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	for _, l := range d.Recipe.Components[0].Lines[1:] {
		if l.Ingredient != 0 {
			t.Errorf("an overlay line still carries ingredient id %d", l.Ingredient)
		}
	}
	if d.Recipe.Rating != nil {
		t.Errorf("the share carries the sender's rating")
	}
}
