package ingest

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	saver_ *Saver
}

func newPictureEnv(t *testing.T) *pictureEnv {
	t.Helper()
	e := newEnv(t)
	dir := t.TempDir()
	svc := picture.New(e.db, media.NewLocal(dir), e.store, picture.NewFetcher(true), nil)
	return &pictureEnv{
		env: e, dir: dir, svc: svc,
		drafts: NewDrafts(e.store).WithPictures(svc),
		saver_: e.saver().WithPictures(svc),
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

func TestURLDraftFetchesCandidatesAndConfirmWaitsForAChoice(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, held := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if held.Candidates != 2 || held.Picture != PictureUnchosen {
		t.Fatalf("candidates = %d, picture = %d; want 2 candidates and no choice", held.Candidates, held.Picture)
	}
	if _, err := Confirm(held); !errors.Is(err, ErrIncompleteDraft) || !strings.Contains(err.Error(), "choose a picture") {
		t.Fatalf("Confirm without a choice = %v, want ErrIncompleteDraft asking for one", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, 3); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("choosing a third of two candidates = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, PictureUnchosen); !errors.Is(err, ErrBadPictureChoice) {
		t.Errorf("choosing nothing = %v, want ErrBadPictureChoice", err)
	}
	if err := e.drafts.ChoosePicture(e.ctx, token, 2); err != nil {
		t.Fatal(err)
	}
	held, err := e.drafts.Get(e.ctx, token)
	if err != nil || held.Picture != 2 {
		t.Fatalf("held choice = %d, %v; want 2", held.Picture, err)
	}
	// An edit of the draft keeps the choice.
	if err := e.drafts.Replace(e.ctx, token, held); err != nil {
		t.Fatal(err)
	}
	if held, _ = e.drafts.Get(e.ctx, token); held.Picture != 2 {
		t.Errorf("choice after an edit = %d, want 2", held.Picture)
	}
}

func TestConfirmKeepsOnlyTheChosenObjectUnderRecipes(t *testing.T) {
	e := newPictureEnv(t)
	srv := imageSite(t)
	token, _ := e.hold(t, e.urlDraft(t, srv.URL+"/a.png", srv.URL+"/b.png"))
	if err := e.drafts.ChoosePicture(e.ctx, token, 1); err != nil {
		t.Fatal(err)
	}
	held, _ := e.drafts.Get(e.ctx, token)
	saved, err := e.saver_.SaveProposed(e.ctx, token, confirm(t, held))
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
	saved, err := e.saver_.SaveProposed(e.ctx, token, confirm(t, held))
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
	if held.Candidates != 0 {
		t.Fatalf("candidates = %d", held.Candidates)
	}
	if _, err := e.saver_.SaveProposed(e.ctx, token, confirm(t, held)); err != nil {
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
	if held.Candidates != 0 {
		t.Fatalf("candidates = %d, want none", held.Candidates)
	}
	if _, err := e.saver_.SaveProposed(e.ctx, token, confirm(t, held)); err != nil {
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
