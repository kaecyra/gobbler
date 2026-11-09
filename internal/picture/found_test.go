package picture_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
)

// pages is a live page fetch: it serves body, or fails with err.
type pages struct {
	body  []byte
	err   error
	calls int
}

func (p *pages) FetchPage(_ context.Context, rawURL string) ([]byte, string, error) {
	p.calls++
	if p.err != nil {
		return nil, "", p.err
	}
	return p.body, rawURL, nil
}

func (e *env) offered(t *testing.T, id int64) []picture.Offered {
	t.Helper()
	got, err := e.svc.Offered(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// offer uploads a picture of seed as recipe id's offered upload.
func (e *env) offer(t *testing.T, id int64, seed int) picture.Offered {
	t.Helper()
	o, err := e.svc.Offer(e.ctx, id, pic(t, 300, 200, seed))
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// search runs a search that must succeed.
func (e *env) search(t *testing.T, id int64) []picture.Offered {
	t.Helper()
	got, err := e.svc.Search(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// age moves every offered picture of the database back by d.
func (e *env) age(t *testing.T, d time.Duration) {
	t.Helper()
	at := time.Now().Add(-d).UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err := e.db.Exec(`UPDATE recipe_found_pictures SET found_at = ?`, at); err != nil {
		t.Fatal(err)
	}
}

func (e *env) snapshots(t *testing.T, id int64) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM source_snapshots WHERE recipe_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSearchReadsTheLivePageAndKeepsWhatItFinds(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/live.png": pic(t, 1200, 400, 3), "/saved.png": pic(t, 1200, 400, 5)})
	rid := e.recipe(t, srv.URL+"/recipe", store.SnapshotInput{Kind: store.SnapshotHTML, Content: page(srv.URL + "/saved.png")})
	e.svc.WithPages(&pages{body: page(srv.URL + "/live.png")})
	got := e.search(t, rid)
	if len(got) != 1 || got[0].SourceURL != srv.URL+"/live.png" || got[0].Uploaded || !picture.ValidOfferID(got[0].ID) {
		t.Fatalf("Search = %+v, want the live page's one picture under an ID", got)
	}
	if !e.has(t, got[0].Key) {
		t.Error("the found picture has no object")
	}
	if kept := e.offered(t, rid); len(kept) != 1 || kept[0].ID != got[0].ID {
		t.Errorf("Offered = %+v, want what Search found", kept)
	}
	if n := e.snapshots(t, rid); n != 1 {
		t.Errorf("the recipe has %d snapshots after a search, want the 1 it had", n)
	}
}

func TestSearchFallsBackToTheSavedSnapshotWhenFetchingFails(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/saved.png": pic(t, 1200, 400, 5)})
	rid := e.recipe(t, srv.URL+"/recipe", store.SnapshotInput{Kind: store.SnapshotHTML, Content: page(srv.URL + "/saved.png")})
	p := &pages{err: errors.New("fetching is disabled")}
	e.svc.WithPages(p)
	got := e.search(t, rid)
	if p.calls != 1 {
		t.Errorf("the live page was fetched %d times, want 1", p.calls)
	}
	if len(got) != 1 || got[0].SourceURL != srv.URL+"/saved.png" {
		t.Fatalf("Search = %+v, want the saved page's picture", got)
	}
}

func TestSearchWithNothingFoundIsAnAnswer(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "https://example.com/r", store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("2 eggs")})
	e.svc.WithPages(&pages{err: errors.New("down")})
	if got := e.search(t, rid); len(got) != 0 {
		t.Fatalf("Search = %v; want nothing", got)
	}
}

func TestANewSearchReplacesTheFoundSetUnderNewIDsAndKeepsTheUpload(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 4)})
	rid := e.recipe(t, srv.URL+"/recipe")
	p := &pages{body: page(srv.URL + "/a.png")}
	e.svc.WithPages(p)
	first := e.search(t, rid)
	if len(first) != 1 {
		t.Fatalf("first Search = %v", first)
	}
	up := e.offer(t, rid, 2)
	p.body = page(srv.URL + "/b.png")
	second := e.search(t, rid)
	if len(second) != 1 || second[0].SourceURL != srv.URL+"/b.png" {
		t.Fatalf("second Search = %+v", second)
	}
	if second[0].ID == first[0].ID || second[0].Key == first[0].Key {
		t.Error("the second search reused the first search's ID or key")
	}
	if e.has(t, first[0].Key) {
		t.Error("the first search's object survived its replacement")
	}
	kept := e.offered(t, rid)
	if len(kept) != 2 || kept[0].ID != second[0].ID || !kept[1].Uploaded || kept[1].ID != up.ID {
		t.Errorf("Offered = %+v, want the new find and the upload", kept)
	}
}

// failingPuts is a media store whose Put fails while fail is set.
type failingPuts struct {
	*flakyMedia
	fail bool
}

func (m *failingPuts) Put(ctx context.Context, key string, data []byte, ct string) error {
	if m.fail {
		return errors.New("storage is full")
	}
	return m.flakyMedia.Put(ctx, key, data, ct)
}

func TestAFailedSearchKeepsThePreviousPictures(t *testing.T) {
	e := newEnv(t)
	m := &failingPuts{flakyMedia: e.media}
	svc := picture.New(e.db, m, e.store, picture.NewFetcher(true), nil)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 4)})
	rid := e.recipe(t, srv.URL+"/recipe")
	p := &pages{body: page(srv.URL + "/a.png")}
	svc.WithPages(p)
	first, err := svc.Search(e.ctx, rid)
	if err != nil || len(first) != 1 {
		t.Fatalf("first Search = %v, %v", first, err)
	}
	m.fail = true
	p.body = page(srv.URL + "/b.png")
	if _, err := svc.Search(e.ctx, rid); err == nil {
		t.Fatal("a search whose store failed succeeded")
	}
	if kept := e.offered(t, rid); len(kept) != 1 || kept[0].ID != first[0].ID || !e.has(t, first[0].Key) {
		t.Errorf("after a failed search Offered = %+v; want the first search's picture, object and all", kept)
	}
}

func TestOfferKeepsAnUploadAndRefusesABadFile(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	if _, err := e.svc.Offer(e.ctx, rid, []byte("not a picture")); !errors.As(err, new(*media.Refusal)) {
		t.Fatalf("Offer of a bad file = %v, want a refusal", err)
	}
	o := e.offer(t, rid, 2)
	if !o.Uploaded || !picture.ValidOfferID(o.ID) || !e.has(t, o.Key) {
		t.Errorf("Offer = %+v", o)
	}
	if _, ok, err := e.svc.Key(e.ctx, rid); err != nil || ok {
		t.Errorf("Key = %v, %v; an upload set the picture before the form was saved", ok, err)
	}
	again := e.offer(t, rid, 7)
	if kept := e.offered(t, rid); len(kept) != 1 || kept[0].ID != again.ID || e.has(t, o.Key) {
		t.Errorf("after a second upload Offered = %+v, want only the second upload", kept)
	}
}

func TestApplyPromotesTheChoiceAndDeletesTheRest(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3)})
	rid := e.recipe(t, srv.URL+"/recipe")
	e.svc.WithPages(&pages{body: page(srv.URL + "/a.png")})
	found := e.search(t, rid)
	if len(found) != 1 {
		t.Fatalf("Search = %v", found)
	}
	up := e.offer(t, rid, 2)
	saved := false
	if err := e.svc.Apply(e.ctx, rid, picture.Choice{Offer: up.ID}, func(context.Context) error { saved = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Error("Apply did not run the save")
	}
	key, ok, err := e.svc.Key(e.ctx, rid)
	if err != nil || !ok || !e.has(t, key) {
		t.Fatalf("after Apply Key = %q, %v, %v", key, ok, err)
	}
	for _, k := range []string{found[0].Key, up.Key} {
		if e.has(t, k) {
			t.Errorf("offered object %s survived the save", k)
		}
	}
	if kept := e.offered(t, rid); len(kept) != 0 {
		t.Errorf("Offered after Apply = %+v, want none", kept)
	}
}

func TestApplyKeepAndNone(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, err := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err != nil {
		t.Fatal(err)
	}
	up := e.offer(t, rid, 3)
	if err := e.svc.Apply(e.ctx, rid, picture.ChoiceKeep, nil); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := e.svc.Key(e.ctx, rid); err != nil || !ok || got != key {
		t.Errorf("Keep changed the picture to %q, %v, %v", got, ok, err)
	}
	if e.has(t, up.Key) {
		t.Error("Keep left the offered picture")
	}
	if err := e.svc.Apply(e.ctx, rid, picture.ChoiceNone, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := e.svc.Key(e.ctx, rid); err != nil || ok || e.has(t, key) {
		t.Errorf("None left the picture (%v, %v)", ok, err)
	}
}

// ADR-00051: a choice names one picture. After a second search the choice the
// page was rendered with is refused, before the save runs, and the picture the
// new set holds in its place is not applied.
func TestApplyAfterASecondSearchRefusesTheOldChoice(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 4)})
	rid := e.recipe(t, srv.URL+"/recipe")
	p := &pages{body: page(srv.URL + "/a.png")}
	e.svc.WithPages(p)
	rendered := e.search(t, rid)
	p.body = page(srv.URL + "/b.png")
	e.search(t, rid)
	saved := false
	err := e.svc.Apply(e.ctx, rid, picture.Choice{Offer: rendered[0].ID}, func(context.Context) error { saved = true; return nil })
	if !errors.Is(err, picture.ErrOfferGone) {
		t.Fatalf("Apply of the replaced choice = %v, want ErrOfferGone", err)
	}
	if saved {
		t.Error("the save ran for a choice that is no longer offered")
	}
	if _, ok, err := e.svc.Key(e.ctx, rid); err != nil || ok {
		t.Errorf("Key = %v, %v; a picture was applied for a refused choice", ok, err)
	}
	if kept := e.offered(t, rid); len(kept) != 1 {
		t.Errorf("a refused Apply changed what is offered: %+v", kept)
	}
}

func TestApplyReturnsTheSaveErrorAndLeavesThePicture(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	up := e.offer(t, rid, 3)
	stale := errors.New("stale")
	if err := e.svc.Apply(e.ctx, rid, picture.Choice{Offer: up.ID}, func(context.Context) error { return stale }); !errors.Is(err, stale) {
		t.Fatalf("Apply = %v, want the save's error", err)
	}
	if _, ok, err := e.svc.Key(e.ctx, rid); err != nil || ok {
		t.Errorf("Key = %v, %v; a failed save set the picture", ok, err)
	}
	if !e.has(t, up.Key) {
		t.Error("a failed save deleted the offered pictures")
	}
}

func TestOfferedPicturesExpireAndAreSwept(t *testing.T) {
	e := newEnv(t)
	old := e.recipe(t, "")
	o := e.offer(t, old, 3)
	e.age(t, picture.FoundLifetime+time.Minute)
	fresh := e.recipe(t, "")
	f := e.offer(t, fresh, 4)

	if kept := e.offered(t, old); len(kept) != 0 {
		t.Errorf("an expired offer is still offered: %+v", kept)
	}
	if err := e.svc.Apply(e.ctx, old, picture.Choice{Offer: o.ID}, nil); !errors.Is(err, picture.ErrOfferGone) {
		t.Errorf("Apply of an expired offer = %v, want ErrOfferGone", err)
	}
	n, err := e.reap.SweepFound(context.Background(), e.db, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || e.has(t, o.Key) {
		t.Errorf("SweepFound deleted %d, expired object kept %v; want 1 and gone", n, e.has(t, o.Key))
	}
	if !e.has(t, f.Key) || len(e.offered(t, fresh)) != 1 {
		t.Error("the sweep deleted an offer younger than 14 days")
	}
}

func TestDeletingARecipeOrHouseholdDeletesOfferedPictures(t *testing.T) {
	e := newEnv(t)
	gone := e.recipe(t, "")
	o1 := e.offer(t, gone, 3)
	if err := e.store.DeleteWithPicture(e.ctx, gone, e.reap); err != nil {
		t.Fatal(err)
	}
	if e.has(t, o1.Key) {
		t.Error("an offered picture survived its recipe's deletion")
	}
	rid := e.recipe(t, "")
	o2 := e.offer(t, rid, 4)
	h, _ := household.FromContext(e.ctx)
	if err := e.hh.DeleteWithPictures(context.Background(), h.ID, auth.DevEmail, "One", e.reap); err != nil {
		t.Fatal(err)
	}
	if e.has(t, o2.Key) {
		t.Error("an offered picture survived its household's deletion")
	}
}

func TestOfferedPicturesOfAnotherHouseholdAreNotFound(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	if _, err := e.svc.Search(e.other, rid); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Search = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Offer(e.other, rid, pic(t, 300, 200, 3)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Offer = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Offered(e.other, rid); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Offered = %v, want ErrNotFound", err)
	}
	if err := e.svc.Apply(e.other, rid, picture.ChoiceNone, nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Apply = %v, want ErrNotFound", err)
	}
}

// A recipe whose sweep fails does not stop the sweep: a later recipe's
// expired offers are still deleted, and the failure is returned.
func TestSweepFoundContinuesPastAFailingRecipe(t *testing.T) {
	e := newEnv(t)
	stuck := e.recipe(t, "")
	s := e.offer(t, stuck, 3)
	later := e.recipe(t, "")
	l := e.offer(t, later, 4)
	e.age(t, picture.FoundLifetime+time.Minute)
	if _, err := e.db.Exec(fmt.Sprintf(`CREATE TRIGGER stuck BEFORE DELETE ON recipe_found_pictures
		WHEN old.recipe_id = %d BEGIN SELECT RAISE(ABORT, 'disk on fire'); END`, stuck)); err != nil {
		t.Fatal(err)
	}
	n, err := e.reap.SweepFound(context.Background(), e.db, time.Now())
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("SweepFound = %v, want the failing recipe's error", err)
	}
	if n != 1 || e.has(t, l.Key) {
		t.Errorf("SweepFound deleted %d and kept the later recipe's object %v; want 1 deleted and it gone", n, e.has(t, l.Key))
	}
	if !e.has(t, s.Key) {
		t.Error("the failing recipe's object was deleted though its rows were not")
	}
}
