package ingest

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
)

// pictureEnv is an env with a picture service over a local media directory.
type pictureEnv struct {
	*env
	dir    string
	svc    *picture.Service
	drafts *Drafts
	keeper *Saver
}

func newPictureEnv(t *testing.T) *pictureEnv {
	t.Helper()
	e := newEnv(t)
	dir := t.TempDir()
	svc := picture.New(e.db, media.NewLocal(dir), e.store, picture.NewFetcher(true), nil)
	return &pictureEnv{
		env: e, dir: dir, svc: svc,
		// The drafts' store deletes the candidates of a draft a re-import
		// supersedes, as in production (web.go).
		drafts: NewDrafts(e.store.WithObjects(picture.NewReaper(media.NewLocal(dir), nil))).WithPictures(svc),
		keeper: e.saver().WithPictures(svc),
	}
}

// objects lists the keys of every stored media object.
func (e *pictureEnv) objects(t *testing.T) []string {
	t.Helper()
	var keys []string
	err := filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(e.dir, p)
		keys = append(keys, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(keys)
	return keys
}

func png1200(seed int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 1200; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * seed), G: uint8(y * (seed + 1)), B: uint8(seed * 37), A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// imageSite serves two distinct pictures at /a.png and /b.png.
func imageSite(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string][]byte{"/a.png": png1200(3), "/b.png": png1200(5)}
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

// urlDraft is a draft that, like a URL import's, names picture candidates. The
// ingredient is a catalog one so the draft saves.
func (e *pictureEnv) urlDraft(t *testing.T, urls ...string) Draft {
	t.Helper()
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "mystery_stew.txt")))
	if err != nil {
		t.Fatal(err)
	}
	d.ImageURLs = urls
	return d
}

func (e *pictureEnv) hold(t *testing.T, d Draft) (string, Draft) {
	t.Helper()
	token, err := e.drafts.Add(e.ctx, d, store.Provenance{Person: auth.DevEmail})
	if err != nil {
		t.Fatal(err)
	}
	held, err := e.drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	return token, held
}

// first is the choice of d's first picture candidate.
func (d Draft) first() PictureChoice { return PictureChoice(d.Candidates[0]) }

func TestURLDraftFetchesCandidatesAndConfirmWaitsForAChoice(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if len(held.Candidates) != 2 || held.Picture != PictureUnchosen {
		t.Fatalf("candidates = %q, picture = %q; want 2 candidates and no choice", held.Candidates, held.Picture)
	}
	second := PictureChoice(held.Candidates[1])
	if _, err := Confirm(held); !errors.Is(err, ErrIncompleteDraft) || !strings.Contains(err.Error(), "choose a picture") {
		t.Fatalf("Confirm without a choice = %v, want ErrIncompleteDraft asking for one", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, "AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("choosing an ID that is not a candidate = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureKeep); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("keeping the picture of a new recipe = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureUnchosen); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("choosing nothing = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, second); err != nil {
		t.Fatal(err)
	}
	held, err := e.drafts.Get(e.ctx, token)
	if err != nil || held.Picture != second {
		t.Fatalf("held choice = %q, %v; want %q", held.Picture, err, second)
	}
	// An edit of the draft keeps the choice.
	if err := e.drafts.Replace(e.ctx, token, held); err != nil {
		t.Fatal(err)
	}
	if held, _ = e.drafts.Get(e.ctx, token); held.Picture != second {
		t.Errorf("choice after an edit = %q, want %q", held.Picture, second)
	}
}

// A choice names a candidate by its ID, so candidates fetched again after the
// choice, or after the page that made it was drawn, never put a different
// picture behind it (ADR-00052).
func TestCandidatesFetchedAgainRefuseAnEarlierChoice(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	urls := []string{srv.URL + "/a.png", srv.URL + "/b.png"}
	token, held := e.hold(t, e.urlDraft(t, urls...))
	first := PictureChoice(held.Candidates[0])
	if err := e.drafts.ChoosePicture(e.ctx, token, first); err != nil {
		t.Fatal(err)
	}
	// The same pictures in the opposite order, as a later fetch could find them.
	if _, err := e.svc.FetchCandidates(e.ctx, held.ProposalID, []string{urls[1], urls[0]}); err != nil {
		t.Fatal(err)
	}
	held, err := e.drafts.Get(e.ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if len(held.Candidates) != 2 || slices.Contains(held.Candidates, string(first)) {
		t.Fatalf("candidates after a second fetch = %q; want 2, none of them %q", held.Candidates, first)
	}
	if _, err := Confirm(held); !errors.Is(err, ErrStalePicture) {
		t.Errorf("Confirm with a choice from before the fetch = %v, want ErrStalePicture", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, first); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("choosing an ID from before the fetch = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureChoice(held.Candidates[1])); err != nil {
		t.Fatal(err)
	}
	held, _ = e.drafts.Get(e.ctx, token)
	if _, err := Confirm(held); err != nil {
		t.Errorf("Confirm after choosing again = %v", err)
	}
}

func TestConfirmKeepsOnlyTheChosenObjectUnderRecipes(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if err := e.drafts.ChoosePicture(e.ctx, token, held.first()); err != nil {
		t.Fatal(err)
	}
	held, _ = e.drafts.Get(e.ctx, token)
	saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil || saved.PictureErr != nil {
		t.Fatalf("SaveProposed = %v, picture error %v", err, saved.PictureErr)
	}
	objs := e.objects(t)
	if len(objs) != 1 || !strings.HasPrefix(objs[0], "recipes/") {
		t.Fatalf("objects = %v, want exactly one under recipes/", objs)
	}
	if key, ok, err := e.svc.Key(e.ctx, saved.RecipeID); err != nil || !ok || key != objs[0] {
		t.Errorf("recipe picture = %q, %v, %v; want %q", key, ok, err, objs[0])
	}
}

func TestConfirmWithNoPictureLeavesNoObjects(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureNone); err != nil {
		t.Fatal(err)
	}
	held, _ := e.drafts.Get(e.ctx, token)
	saved, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil {
		t.Fatal(err)
	}
	if objs := e.objects(t); len(objs) != 0 {
		t.Errorf("objects = %v, want none", objs)
	}
	if _, ok, _ := e.svc.Key(e.ctx, saved.RecipeID); ok {
		t.Error("recipe has a picture, want none")
	}
}

func TestDraftWithNoCandidatesConfirmsWithoutAChoice(t *testing.T) {
	e := newPictureEnv(t)
	token, held := e.hold(t, e.urlDraft(t)) // pasted text and photos have none
	if len(held.Candidates) != 0 {
		t.Fatalf("candidates = %q", held.Candidates)
	}
	if _, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held)); err != nil {
		t.Fatal(err)
	}
}

func TestDiscardLeavesNoCandidateObjects(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if got := e.objects(t); len(got) != 2 {
		t.Fatalf("objects while waiting = %v, want 2 candidates", got)
	}
	if err := e.drafts.Discard(e.ctx, token); err != nil {
		t.Fatal(err)
	}
	if objs := e.objects(t); len(objs) != 0 {
		t.Errorf("objects after discard = %v, want none under drafts/", objs)
	}
}

func TestCandidateFetchFailureDoesNotFailTheIngest(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, held := e.hold(t, e.urlDraft(t, srv.URL+"/missing.png", "http://127.0.0.1:1/x.png"))
	if len(held.Candidates) != 0 {
		t.Fatalf("candidates = %q, want none", held.Candidates)
	}
	if _, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held)); err != nil {
		t.Fatalf("a draft whose pictures all failed must still confirm: %v", err)
	}
}

func TestURLImportNamesTheImagesOfThePage(t *testing.T) {
	e := newEnv(t)
	page := bytes.Replace(readFixture(t, "pancakes.html"), []byte("</head>"),
		[]byte(`<meta property="og:image" content="/hero.png"></head>`), 1)
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.ImageURLs) != 1 || d.ImageURLs[0] != srv.URL+"/hero.png" {
		t.Errorf("ImageURLs = %v, want the page's og:image resolved against it", d.ImageURLs)
	}
}

// brokenFetch is a Pictures whose candidate fetch itself fails, as when the
// media store is down, rather than dropping candidates one by one.
type brokenFetch struct{ *picture.Service }

func (brokenFetch) FetchCandidates(context.Context, int64, []string) ([]picture.Candidate, error) {
	return nil, errors.New("media store is down")
}

func TestCandidateFetchErrorStillHoldsAndConfirmsTheDraft(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	drafts := NewDrafts(e.store).WithPictures(brokenFetch{e.svc})
	token, err := drafts.Add(e.ctx, e.urlDraft(t, srv.URL+"/a.png"), store.Provenance{Person: auth.DevEmail})
	if err != nil || token == "" {
		t.Fatalf("Add = %q, %v; a failed candidate fetch must not fail the ingest", token, err)
	}
	held, err := drafts.Get(e.ctx, token)
	if err != nil || len(held.Candidates) != 0 {
		t.Fatalf("held = %q candidates, %v; want none", held.Candidates, err)
	}
	if _, err := e.keeper.SaveProposed(e.ctx, token, confirm(t, held)); err != nil {
		t.Fatalf("the draft must still confirm: %v", err)
	}
}

// brokenPromote is a PictureKeeper that cannot keep the chosen picture.
type brokenPromote struct{ PictureKeeper }

func (brokenPromote) Promote(context.Context, int64, string, int64) (string, error) {
	return "", errors.New("media store is down")
}

func TestSavedPictureErrReportsAPictureThatCouldNotBeKept(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png"))
	if err := e.drafts.ChoosePicture(e.ctx, token, held.first()); err != nil {
		t.Fatal(err)
	}
	held, _ = e.drafts.Get(e.ctx, token)
	saved, err := e.saver().WithPictures(brokenPromote{e.svc}).SaveProposed(e.ctx, token, confirm(t, held))
	if err != nil {
		t.Fatalf("SaveProposed = %v; the recipe is stored, so this is not an error", err)
	}
	if saved.RecipeID == 0 || saved.PictureErr == nil {
		t.Fatalf("saved = %+v; want a recipe id and a PictureErr", saved)
	}
	if _, err := e.store.Get(e.ctx, saved.RecipeID); err != nil {
		t.Errorf("recipe %d is not stored: %v", saved.RecipeID, err)
	}
}
