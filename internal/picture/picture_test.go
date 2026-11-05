package picture_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// env is a migrated database with two households, a store, a local media store
// and a picture service whose fetcher may reach the test server on loopback.
type env struct {
	db    *sql.DB
	store *store.Store
	hh    *household.Store
	media *flakyMedia
	svc   *picture.Service
	reap  *picture.Reaper
	ctx   context.Context
	other context.Context
}

// flakyMedia is a media store whose Delete can be made to fail.
type flakyMedia struct {
	media.Store
	mu       sync.Mutex
	failKeys bool
}

func (m *flakyMedia) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	fail := m.failKeys
	m.mu.Unlock()
	if fail {
		return errors.New("storage is down")
	}
	return m.Store.Delete(ctx, key)
}

func ctxFor(t *testing.T, h household.Household) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return household.WithContext(got, h)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	bg := context.Background()
	d, err := db.Open(bg, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(bg, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	hs := household.New(d)
	h1, err := hs.Create(bg, "One", auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hs.Create(bg, "Two", auth.DevOtherEmail)
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d)
	m := &flakyMedia{Store: media.NewLocal(t.TempDir())}
	return &env{
		db: d, store: st, hh: hs, media: m,
		svc:  picture.New(d, m, st, picture.NewFetcher(true), nil),
		reap: picture.NewReaper(m, nil),
		ctx:  ctxFor(t, h1), other: ctxFor(t, h2),
	}
}

func (e *env) recipe(t *testing.T, sourceURL string, snaps ...store.SnapshotInput) int64 {
	t.Helper()
	id, err := e.store.CreateWithSnapshots(e.ctx, recipe.Recipe{
		Name: "Stew", SourceURL: sourceURL,
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: "water"}},
			Steps: []recipe.Step{{ID: "s1", Text: "Do it."}},
		}},
	}, snaps...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// draft proposes a draft and returns its proposal id.
func (e *env) draft(t *testing.T) int64 {
	t.Helper()
	var ing int64
	if err := e.db.QueryRow(`SELECT id FROM ingredients WHERE household_id IS NULL ORDER BY id LIMIT 1`).Scan(&ing); err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"name":"Draft","components":[{"lines":[{"raw":"x","ingredient":%d,"confidence":0.5}]}]}`, ing)
	token, err := e.store.ProposeDraft(e.ctx, store.DraftInput{
		Document:   []byte(doc),
		Source:     store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("2 eggs")},
		Provenance: store.Provenance{Person: auth.DevEmail},
	})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM proposals WHERE token = ?`, token).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) has(t *testing.T, key string) bool {
	t.Helper()
	_, err := e.media.Get(context.Background(), key)
	if err != nil && !errors.Is(err, media.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// pic is a PNG of w by h whose pixels depend on seed.
func pic(t *testing.T, w, h, seed int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * seed), G: uint8(y * (seed + 1)), B: uint8(seed * 37), A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// site serves the named bodies at their paths; anything else is a 404.
func site(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchCandidatesDropsFailuresAndKeepsTheRest(t *testing.T) {
	e := newEnv(t)
	good := pic(t, 1200, 400, 3)
	srv := site(t, map[string][]byte{
		"/a.png":     good,
		"/dup.png":   good,                 // same pixels at another address
		"/b.png":     pic(t, 1200, 400, 5), // a different picture
		"/small.png": pic(t, 500, 200, 7),  // below the minimum
		"/junk":      []byte("not a picture"),
	})
	id := e.draft(t)
	got, err := e.svc.FetchCandidates(e.ctx, id, []string{
		srv.URL + "/missing.png", srv.URL + "/junk", srv.URL + "/small.png",
		srv.URL + "/a.png", srv.URL + "/dup.png", srv.URL + "/b.png", "ftp://example.com/x.png",
	})
	if err != nil {
		t.Fatalf("FetchCandidates: %v (a failed candidate must be dropped, not fail the call)", err)
	}
	if len(got) != 2 || got[0].SourceURL != srv.URL+"/a.png" || got[1].SourceURL != srv.URL+"/b.png" {
		t.Fatalf("candidates = %+v, want a.png then b.png", got)
	}
	for i, c := range got {
		if want := fmt.Sprintf("drafts/%d/%d.webp", id, i+1); c.Key != want || c.Position != i+1 {
			t.Errorf("candidate %d = %+v, want key %s", i, c, want)
		}
		if !e.has(t, c.Key) {
			t.Errorf("object %s is not stored", c.Key)
		}
		if c.Width > media.MaxSide || c.Height > media.MaxSide || c.Width < 1 {
			t.Errorf("candidate %d size %dx%d is not the fitted size", i, c.Width, c.Height)
		}
	}
	listed, err := e.svc.Candidates(e.ctx, id)
	if err != nil || len(listed) != 2 {
		t.Fatalf("Candidates = %v, %v; want the two stored", listed, err)
	}
}

func TestFetchCandidatesKeepsAtMostEight(t *testing.T) {
	e := newEnv(t)
	files := map[string][]byte{}
	var urls []string
	for i := 1; i <= 10; i++ {
		files[fmt.Sprintf("/%d.png", i)] = pic(t, 1100, 310, i)
	}
	srv := site(t, files)
	for i := 1; i <= 10; i++ {
		urls = append(urls, fmt.Sprintf("%s/%d.png", srv.URL, i))
	}
	got, err := e.svc.FetchCandidates(e.ctx, e.draft(t), urls)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != picture.MaxCandidates {
		t.Fatalf("kept %d candidates, want %d", len(got), picture.MaxCandidates)
	}
}

func TestPromoteMovesTheChosenCandidateAndClearsTheDraft(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 5)})
	pid := e.draft(t)
	cs, err := e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/a.png", srv.URL + "/b.png"})
	if err != nil || len(cs) != 2 {
		t.Fatalf("FetchCandidates = %v, %v", cs, err)
	}
	rid := e.recipe(t, "")
	second, err := e.media.Get(context.Background(), cs[1].Key)
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.svc.Promote(e.ctx, pid, 2, rid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, fmt.Sprintf("recipes/%d/", rid)) || !strings.HasSuffix(key, ".webp") {
		t.Errorf("key = %q, want recipes/%d/<version>.webp", key, rid)
	}
	if got, err := e.media.Get(context.Background(), key); err != nil || !bytes.Equal(got, second) {
		t.Errorf("the promoted object is not candidate 2 (err %v)", err)
	}
	if got, ok, err := e.svc.Key(e.ctx, rid); err != nil || !ok || got != key {
		t.Errorf("Key = %q, %v, %v; want %q", got, ok, err, key)
	}
	for _, c := range cs {
		if e.has(t, c.Key) {
			t.Errorf("draft object %s survived the promotion", c.Key)
		}
	}
	if left, _ := e.svc.Candidates(e.ctx, pid); len(left) != 0 {
		t.Errorf("candidate rows survived: %+v", left)
	}
}

func TestDeleteCandidatesRemovesRowsAndObjects(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3)})
	pid := e.draft(t)
	cs, _ := e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/a.png"})
	if len(cs) != 1 {
		t.Fatalf("setup: %v", cs)
	}
	if err := e.svc.DeleteCandidates(e.ctx, pid); err != nil {
		t.Fatal(err)
	}
	if e.has(t, cs[0].Key) {
		t.Error("candidate object survived")
	}
}

func TestUploadSetsThePictureAndRefusesABadFile(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, err := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err != nil {
		t.Fatal(err)
	}
	if !e.has(t, key) {
		t.Fatalf("object %s is not stored", key)
	}
	_, err = e.svc.Upload(e.ctx, rid, []byte("not a picture"))
	if _, refused := media.RefusalText(err); !refused {
		t.Fatalf("Upload(text) = %v, want a *media.Refusal", err)
	}
	_, err = e.svc.Upload(e.ctx, rid, make([]byte, media.MaxUploadBytes+1))
	if _, refused := media.RefusalText(err); !refused {
		t.Fatalf("Upload(over 5 MB) = %v, want a *media.Refusal", err)
	}
	if got, _, _ := e.svc.Key(e.ctx, rid); got != key {
		t.Errorf("a refused upload changed the picture: key = %q, want %q", got, key)
	}
	if !e.has(t, key) {
		t.Error("a refused upload deleted the current picture")
	}
}

func TestReplacingAPictureDeletesThePreviousObject(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	first, err := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 4))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("a replacement kept the same key; served pictures could not be cached as immutable")
	}
	if e.has(t, first) {
		t.Error("the previous object survived the replacement")
	}
	if !e.has(t, second) {
		t.Error("the new object is missing")
	}
}

func TestRemoveClearsThePictureAndItsObject(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	if err := e.svc.Remove(e.ctx, rid); err != nil {
		t.Fatalf("Remove with no picture = %v, want nil", err)
	}
	key, _ := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err := e.svc.Remove(e.ctx, rid); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := e.svc.Key(e.ctx, rid); ok {
		t.Error("the recipe still has a picture")
	}
	if e.has(t, key) {
		t.Error("the object survived Remove")
	}
}

func TestAnotherHouseholdsRecipeIsNotFound(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	if _, err := e.svc.Upload(e.other, rid, pic(t, 300, 200, 2)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Upload = %v, want ErrNotFound", err)
	}
	if err := e.svc.Remove(e.other, rid); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Remove = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Refind(e.other, rid); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Refind = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Upload(context.Background(), rid, nil); !errors.Is(err, store.ErrNoHousehold) {
		t.Errorf("Upload with no household = %v, want ErrNoHousehold", err)
	}
}

func page(imageURL string) []byte {
	return []byte(fmt.Sprintf(`<html><head><meta property="og:image" content="%s"><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Stew","image":["%s"],"recipeIngredient":["1 cup water"],"recipeInstructions":["Boil."]}
</script></head><body></body></html>`, imageURL, imageURL))
}

func TestRefindReadsTheSavedSnapshot(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/hero.png": pic(t, 1200, 400, 3)})
	rid := e.recipe(t, srv.URL+"/recipe", store.SnapshotInput{Kind: store.SnapshotHTML, Content: page(srv.URL + "/hero.png")})
	got, err := e.svc.Refind(e.ctx, rid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceURL != srv.URL+"/hero.png" || len(got[0].WebP) == 0 {
		t.Fatalf("Refind = %+v, want the one image of the saved page", got)
	}
	key, err := e.svc.Choose(e.ctx, rid, got[0])
	if err != nil || !e.has(t, key) {
		t.Fatalf("Choose = %q, %v", key, err)
	}
}

func TestRefindWithoutAnHTMLSnapshotReturnsNoCandidates(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "", store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("2 eggs")})
	got, err := e.svc.Refind(e.ctx, rid)
	if err != nil || len(got) != 0 {
		t.Fatalf("Refind = %v, %v; want no candidates and no error", got, err)
	}
}

func TestARevisionRestoreLeavesThePicture(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, err := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.store.Get(e.ctx, rid)
	if err != nil {
		t.Fatal(err)
	}
	r.Name = "Stew, edited"
	if err := e.store.Update(e.ctx, rid, r); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Restore(e.ctx, rid, 1); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := e.svc.Key(e.ctx, rid); err != nil || !ok || got != key {
		t.Errorf("after a restore Key = %q, %v, %v; want %q unchanged", got, ok, err, key)
	}
	if !e.has(t, key) {
		t.Error("a restore deleted the picture object")
	}
}

func TestDeletingARecipeDeletesItsPicture(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, _ := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err := e.store.DeleteWithPicture(e.ctx, rid, e.reap); err != nil {
		t.Fatal(err)
	}
	if e.has(t, key) {
		t.Error("the picture object survived the recipe's deletion")
	}
}

func TestDeletingAMissingRecipeKeepsOtherPictures(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, _ := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	if err := e.store.DeleteWithPicture(e.other, rid, e.reap); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete by another household = %v, want ErrNotFound", err)
	}
	if !e.has(t, key) {
		t.Error("a refused delete removed the picture")
	}
}

func TestDeletingAHouseholdDeletesItsPicturesAndCandidates(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3)})
	rid := e.recipe(t, "")
	key, _ := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	cs, _ := e.svc.FetchCandidates(e.ctx, e.draft(t), []string{srv.URL + "/a.png"})
	if len(cs) != 1 {
		t.Fatalf("setup: %v", cs)
	}
	h, _ := household.FromContext(e.ctx)
	if err := e.hh.DeleteWithPictures(context.Background(), h.ID, auth.DevEmail, "One", e.reap); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{key, cs[0].Key} {
		if e.has(t, k) {
			t.Errorf("object %s survived the household's deletion", k)
		}
	}
}

func TestAFailedObjectDeleteDoesNotFailTheHouseholdDeletion(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	key, _ := e.svc.Upload(e.ctx, rid, pic(t, 300, 200, 2))
	e.media.failKeys = true
	h, _ := household.FromContext(e.ctx)
	if err := e.hh.DeleteWithPictures(context.Background(), h.ID, auth.DevEmail, "One", e.reap); err != nil {
		t.Fatalf("DeleteWithPictures = %v, want nil: a failed object delete is logged only", err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM households WHERE id = ?`, h.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("household rows = %d, %v; want 0", n, err)
	}
	e.media.failKeys = false
	if !e.has(t, key) {
		t.Error("setup: the object should have been left behind by the failed delete")
	}
}

func TestFetchCandidatesRefusesAnotherHouseholdsDraft(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 5)})
	pid := e.draft(t)
	mine, err := e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/a.png"})
	if err != nil || len(mine) != 1 {
		t.Fatalf("setup: %v, %v", mine, err)
	}
	before, _ := e.media.Get(context.Background(), mine[0].Key)
	if _, err := e.svc.FetchCandidates(e.other, pid, []string{srv.URL + "/b.png"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("FetchCandidates for another household's draft = %v, want ErrNotFound", err)
	}
	after, err := e.media.Get(context.Background(), mine[0].Key)
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("the other household's candidate object was overwritten or deleted (err %v)", err)
	}
	if left, _ := e.svc.Candidates(e.ctx, pid); len(left) != 1 {
		t.Errorf("candidate rows = %d, want 1", len(left))
	}
}

func TestConcurrentFetchesOnOneDraftLeaveObjectsForEveryRow(t *testing.T) {
	e := newEnv(t)
	srv := site(t, map[string][]byte{"/a.png": pic(t, 1200, 400, 3), "/b.png": pic(t, 1200, 400, 5)})
	pid := e.draft(t)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/a.png", srv.URL + "/b.png"})
		}()
	}
	wg.Wait()
	rows, err := e.svc.Candidates(e.ctx, pid)
	if err != nil || len(rows) != 2 {
		t.Fatalf("Candidates = %v, %v; want 2 rows", rows, err)
	}
	for _, c := range rows {
		if !e.has(t, c.Key) {
			t.Errorf("row %d names %s, which has no object", c.Position, c.Key)
		}
	}
}

func TestSettingAPictureOnADeletedRecipeIsNotFound(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	if err := e.store.Delete(e.ctx, rid); err != nil {
		t.Fatal(err)
	}
	// Choose skips the up-front check Upload makes, so it reaches the write.
	f := picture.Found{WebP: pic(t, 300, 200, 2)}
	if _, err := e.svc.Choose(e.ctx, rid, f); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Choose on a deleted recipe = %v, want ErrNotFound", err)
	}
}

func TestChooseRefusesBytesThatAreNotAnImage(t *testing.T) {
	e := newEnv(t)
	rid := e.recipe(t, "")
	_, err := e.svc.Choose(e.ctx, rid, picture.Found{WebP: []byte("not an image")})
	if _, refused := media.RefusalText(err); !refused {
		t.Fatalf("Choose(junk) = %v, want a *media.Refusal", err)
	}
	if _, ok, _ := e.svc.Key(e.ctx, rid); ok {
		t.Error("junk bytes were stored as the picture")
	}
}

func TestPromoteWaitsForAFetchOnTheSameDraft(t *testing.T) {
	e := newEnv(t)
	a, b := pic(t, 1200, 400, 3), pic(t, 1200, 400, 5)
	var slow sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b.png" {
			slow.Do(func() { time.Sleep(300 * time.Millisecond) }) // keep the fetch in flight
			_, _ = w.Write(b)
			return
		}
		_, _ = w.Write(a)
	}))
	defer srv.Close()
	pid := e.draft(t)
	if cs, err := e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/a.png"}); err != nil || len(cs) != 1 {
		t.Fatalf("setup: %v, %v", cs, err)
	}
	rid := e.recipe(t, "")
	done := make(chan []picture.Candidate)
	go func() {
		cs, _ := e.svc.FetchCandidates(e.ctx, pid, []string{srv.URL + "/b.png"}) // replaces the candidates with b
		done <- cs
	}()
	time.Sleep(100 * time.Millisecond) // the fetch is now downloading b
	key, err := e.svc.Promote(e.ctx, pid, 1, rid)
	fetched := <-done
	if err != nil {
		t.Fatalf("Promote during a fetch = %v; it must wait for the fetch and promote its result", err)
	}
	if len(fetched) != 1 {
		t.Fatalf("fetch = %v", fetched)
	}
	got, err := e.media.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	d, err := media.Decode(got)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := media.Decode(b)
	wp, _ := media.Picture(want.Image)
	wd, _ := media.Decode(wp)
	if pixelsDiffer(d.Image, wd.Image) {
		t.Error("the promoted picture is not the one the fetch stored")
	}
}

func pixelsDiffer(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return true
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y += 17 {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x += 17 {
			if a.At(x, y) != b.At(x, y) {
				return true
			}
		}
	}
	return false
}
