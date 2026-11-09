package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// pictureEnv is env with the picture picker mounted, a source page that names
// one picture (or none, with an empty page), and a recipe imported from it.
type pictureEnv struct {
	*env
	pics *apirecipes.Pictures
	page *httptest.Server
}

func pngOf(t *testing.T, w, h, seed int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * seed), G: uint8(y), B: uint8(seed * 37), A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func newPictureEnv(t *testing.T, withImage bool) *pictureEnv {
	t.Helper()
	return newPictureEnvOver(t, withImage, media.NewLocal(t.TempDir()))
}

// brokenPuts is a media store that cannot store anything.
type brokenPuts struct{ media.Store }

func (brokenPuts) Put(context.Context, string, []byte, string) error {
	return errors.New("the media store is down")
}

// newPictureEnvOver is newPictureEnv over the media store m.
func newPictureEnvOver(t *testing.T, withImage bool, m media.Store) *pictureEnv {
	t.Helper()
	e := &pictureEnv{env: newEnv(t)}
	hero := pngOf(t, 2200, 700, 3)
	e.page = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hero.png" {
			_, _ = w.Write(hero)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		img := ""
		if withImage {
			img = fmt.Sprintf(`"image":["%s/hero.png"],`, e.page.URL)
		}
		_, _ = fmt.Fprintf(w, `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Loaf",%s"recipeIngredient":["1 cup flour"],"recipeInstructions":["Mix."]}
</script></head><body></body></html>`, img)
	}))
	t.Cleanup(e.page.Close)
	e.pics = apirecipes.NewPictures(e.d, m, e.store,
		config.Ingestion{AllowLoopbackFetch: true, UserAgent: "t", FetchTimeout: time.Second}, nil)
	svc, err := apirecipes.New(apirecipes.Deps{Recipes: e.store, Catalog: e.cat, Members: household.New(e.d), Thumbnails: e.thumbs, Pictures: e.pics})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	r := chi.NewRouter()
	r.Use(e.mw...)
	r.Mount(Pattern, Routes(Deps{Recipes: svc, Catalog: e.cat, Pictures: svc}))
	e.h = r
	rec := e.fixture()
	rec.SourceURL = e.page.URL + "/loaf"
	e.id, err = e.store.CreateWithSnapshots(e.ctx, rec, store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("src")})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *pictureEnv) key() (string, bool) {
	e.t.Helper()
	k, ok, err := e.pics.Key(e.ctx, e.id)
	if err != nil {
		e.t.Fatal(err)
	}
	return k, ok
}

func (e *pictureEnv) offered() []apirecipes.Offered {
	e.t.Helper()
	offers, err := e.pics.Offered(e.ctx, e.id)
	if err != nil {
		e.t.Fatal(err)
	}
	return offers
}

func (e *pictureEnv) search(htmx bool) result { return e.searchChoosing(htmx, "") }

// searchChoosing is search posting choice as the picker's checked radio, as
// the search link's hx-include does.
func (e *pictureEnv) searchChoosing(htmx bool, choice string) result {
	hdr := map[string]string{}
	if htmx {
		hdr["HX-Request"] = "true"
	}
	form := url.Values{}
	if choice != "" {
		form.Set(fieldPicture, choice)
	}
	return e.do(http.MethodPost, e.path()+"/picture/search", form, hdr)
}

func (e *pictureEnv) upload(data []byte) result { return e.uploadChoosing(data, "") }

// uploadChoosing is upload posting choice in the upload form's choice field,
// which comes ahead of the file.
func (e *pictureEnv) uploadChoosing(data []byte, choice string) result {
	e.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if choice != "" {
		if err := mw.WriteField(fieldPicture, choice); err != nil {
			e.t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile(uploadField, "p.png")
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		e.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, e.path()+"/picture/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return result{rec}
}

// A recipe with a source page, no picture and nothing offered searches on
// load: the page renders at once in the searching state.
func TestEditPageStartsASearchForARecipeWithNoPicture(t *testing.T) {
	e := newPictureEnv(t, true)
	r := e.get()
	r.wantStatus(t, http.StatusOK)
	r.wantBody(t, `class="picker edit-picture searching"`, `hx-trigger="load"`, `/picture/search`,
		"No picture", "Shows a coloured band", `class="pick ghost"`, "Searching the source page", "Upload a picture")
	if strings.Contains(r.body(), "Current picture") {
		t.Error("a recipe without a picture offers Current picture")
	}
}

func TestEditPageWithAPictureOffersItAndDoesNotSearch(t *testing.T) {
	e := newPictureEnv(t, true)
	if _, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2)); err != nil {
		t.Fatal(err)
	}
	r := e.get()
	r.wantBody(t, "Current picture", "Left as it is", "Removes it", `value="keep"`, "Search the source page again")
	if strings.Contains(r.body(), `hx-trigger="load"`) {
		t.Error("a recipe with a picture searched on load")
	}
	if strings.Index(r.body(), "Current picture") > strings.Index(r.body(), "Removes it") {
		t.Error("No picture comes before the current picture")
	}
}

func TestARecipeWithoutASourceGetsNoSearch(t *testing.T) {
	e := newPictureEnv(t, true)
	r, v := e.stored()
	r.SourceURL = ""
	if err := e.store.UpdateIfVersion(e.ctx, e.id, v, r); err != nil {
		t.Fatal(err)
	}
	body := e.get().body()
	if strings.Contains(body, "/picture/search") || strings.Contains(body, "Search the source page") {
		t.Error("a recipe with no source address offers a search")
	}
	if !strings.Contains(body, "Upload a picture") {
		t.Error("no upload link")
	}
}

func TestSearchOffersWhatItFoundAndChangesNothing(t *testing.T) {
	e := newPictureEnv(t, true)
	_, before := e.stored()
	r := e.search(true)
	r.wantStatus(t, http.StatusOK)
	r.wantBody(t, `id="edit-picture"`, "Recipe image", "1024 × 326", "/picture/offered/"+offerIDs(t, r.body())[0], "Search the source page again")
	if strings.Contains(r.body(), "<html") || strings.Contains(r.body(), "no usable pictures") {
		t.Error("the search answer is not the picker alone with its finds")
	}
	if _, ok := e.key(); ok {
		t.Error("a search set the picture")
	}
	if _, after := e.stored(); after != before {
		t.Errorf("a search moved the recipe from version %d to %d", before, after)
	}
	if len(e.offered()) != 1 {
		t.Errorf("offered = %v, want the one find kept", e.offered())
	}
}

func TestSearchFindingNothingSaysSo(t *testing.T) {
	e := newPictureEnv(t, false)
	e.search(true).wantBody(t, "The source page had no usable pictures.")
}

func TestSearchWithoutScriptsLandsOnTheEditPage(t *testing.T) {
	e := newPictureEnv(t, true)
	r := e.search(false)
	r.wantStatus(t, http.StatusSeeOther)
	if loc := r.Header().Get("Location"); loc != e.path() {
		t.Errorf("Location = %q, want %q", loc, e.path())
	}
}

func TestUploadOffersThePictureChosenAndRefusesABadFile(t *testing.T) {
	e := newPictureEnv(t, true)
	r := e.upload(pngOf(t, 300, 200, 4))
	r.wantStatus(t, http.StatusOK)
	ids := offerIDs(t, r.body())
	r.wantBody(t, "Uploaded picture", "300 × 200", `value="`+ids[len(ids)-1]+`" form="editor" checked`)
	if _, ok := e.key(); ok {
		t.Error("an upload set the picture before Save")
	}
	e.upload([]byte("not a picture")).wantBody(t, `class="pick-note"`, "Uploaded picture")
}

func TestSavingAppliesTheChosenPicture(t *testing.T) {
	e := newPictureEnv(t, true)
	f := e.form()
	f.Set(fieldPicture, offerIDs(t, e.search(true).body())[0])
	e.post(f).wantStatus(t, http.StatusSeeOther)
	if _, ok := e.key(); !ok {
		t.Error("Save did not set the chosen picture")
	}
	if len(e.offered()) != 0 {
		t.Errorf("offered after Save = %v, want none", e.offered())
	}
}

func TestSavingWithNoPictureRemovesItAndKeepLeavesIt(t *testing.T) {
	e := newPictureEnv(t, true)
	key, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2))
	if err != nil {
		t.Fatal(err)
	}
	e.upload(pngOf(t, 300, 200, 5)).wantStatus(t, http.StatusOK)
	f := e.form()
	f.Set(fieldPicture, pictureKeep)
	e.post(f).wantStatus(t, http.StatusSeeOther)
	if got, ok := e.key(); !ok || got != key {
		t.Errorf("Keep changed the picture to %q, %v", got, ok)
	}
	if len(e.offered()) != 0 {
		t.Error("Keep left the offered pictures")
	}
	f = e.form()
	f.Set(fieldPicture, pictureNone)
	e.post(f).wantStatus(t, http.StatusSeeOther)
	if _, ok := e.key(); ok {
		t.Error("No picture left the picture")
	}
}

// Leaving without saving changes nothing: the offered pictures wait, and the
// picture is the one the recipe had.
func TestOfferedPicturesWaitForSave(t *testing.T) {
	e := newPictureEnv(t, true)
	e.search(true).wantStatus(t, http.StatusOK)
	e.upload(pngOf(t, 300, 200, 4)).wantStatus(t, http.StatusOK)
	if _, ok := e.key(); ok {
		t.Error("the picture changed without a save")
	}
	body := e.get().body()
	if !strings.Contains(body, "Recipe image") || !strings.Contains(body, "Uploaded picture") || strings.Contains(body, `hx-trigger="load"`) {
		t.Error("the edit page does not show what is offered, or searches again")
	}
}

func TestSavingAPictureNoLongerOfferedSavesNothing(t *testing.T) {
	e := newPictureEnv(t, true)
	_, before := e.stored()
	f := e.form()
	f.Set("name", "Renamed")
	f.Set(fieldPicture, "AAAAAAAAAAAAAAAAAAAAAA")
	r := e.post(f)
	r.wantStatus(t, http.StatusBadRequest)
	r.wantBody(t, "no longer offered")
	if got, after := e.stored(); after != before || got.Name == "Renamed" {
		t.Error("the recipe was saved with a picture that is not offered")
	}
}

func TestAMalformedPictureChoiceIs400(t *testing.T) {
	e := newPictureEnv(t, true)
	f := e.form()
	f.Set(fieldPicture, "x")
	e.post(f).wantStatus(t, http.StatusBadRequest)
}

// ADR-00051: a choice names the picture the person saw. After a second search
// the choice the page was rendered with is refused, nothing is saved, and the
// new set's picture in the same place is not applied.
func TestSavingAChoiceASecondSearchReplacedIsRefused(t *testing.T) {
	e := newPictureEnv(t, true)
	rendered := offerIDs(t, e.search(true).body())
	again := offerIDs(t, e.search(true).body())
	if len(rendered) != 1 || len(again) != 1 || rendered[0] == again[0] {
		t.Fatalf("searches offered %v then %v; want one picture each under different IDs", rendered, again)
	}
	_, before := e.stored()
	f := e.form()
	f.Set("name", "Renamed")
	f.Set(fieldPicture, rendered[0])
	r := e.post(f)
	r.wantStatus(t, http.StatusBadRequest)
	r.wantBody(t, "no longer offered")
	if got, after := e.stored(); after != before || got.Name == "Renamed" {
		t.Error("the recipe was saved with a choice a later search replaced")
	}
	if _, ok := e.key(); ok {
		t.Error("the new search's picture was applied in place of the old choice")
	}
	if offers := e.offered(); len(offers) != 1 || offers[0].ID != again[0] {
		t.Errorf("offered = %+v, want the second search's picture still waiting", offers)
	}
}

// offerIDs are the IDs of the offered pictures in a rendered picker, in order.
func offerIDs(t *testing.T, body string) []string {
	t.Helper()
	var ids []string
	for _, m := range offerRadio.FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
	}
	if len(ids) == 0 {
		t.Fatalf("no offered picture in %s", body)
	}
	return ids
}

var offerRadio = regexp.MustCompile(`name="picture" value="([A-Za-z0-9_-]{22})"`)

// checked is the value of the picker's checked radio in body.
func checked(t *testing.T, body string) string {
	t.Helper()
	m := checkedRadio.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no checked radio in %s", body)
	}
	return m[1]
}

var checkedRadio = regexp.MustCompile(`name="picture" value="([^"]+)" form="editor" checked`)

// A search keeps the choice the picker posts with it: No picture and the
// uploaded picture stay chosen. A chosen found picture the search replaced
// falls back to the default.
func TestSearchKeepsThePickersChoice(t *testing.T) {
	e := newPictureEnv(t, true)
	if _, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2)); err != nil {
		t.Fatal(err)
	}
	if got := checked(t, e.searchChoosing(true, pictureNone).body()); got != pictureNone {
		t.Errorf("after a search with No picture chosen, %q is chosen", got)
	}
	upload := checked(t, e.upload(pngOf(t, 300, 200, 5)).body())
	if got := checked(t, e.searchChoosing(true, upload).body()); got != upload {
		t.Errorf("after a search with the upload chosen, %q is chosen, want %q", got, upload)
	}
	found := offerIDs(t, e.search(true).body())[0]
	if got := checked(t, e.searchChoosing(true, found).body()); got != pictureKeep {
		t.Errorf("after a search replaced the chosen picture, %q is chosen, want the default %q", got, pictureKeep)
	}
}

// ADR-00051: an automatic search that fails ends in the picker's resting
// state with a note saying so, and the search link still works.
func TestAFailedSearchSaysSoAndStopsSearching(t *testing.T) {
	e := newPictureEnvOver(t, true, brokenPuts{media.NewLocal(t.TempDir())})
	if !strings.Contains(e.get().body(), `hx-trigger="load"`) {
		t.Fatal("setup: the edit page did not search on load")
	}
	r := e.search(true)
	r.wantStatus(t, http.StatusOK)
	r.wantBody(t, "Could not search the source page.", `class="picker edit-picture"`, "/picture/search", "Search the source page again")
	if strings.Contains(r.body(), `hx-trigger="load"`) || strings.Contains(r.body(), "searching") {
		t.Error("a failed search left the picker searching")
	}
	if strings.Contains(r.body(), "Try again") || strings.Contains(r.body(), "Retry") {
		t.Error("a failed search offers a retry button")
	}
}

// Without scripts, a search's outcome reaches the edit page it lands on, and
// that page starts no search of its own.
func TestSearchWithoutScriptsCarriesItsOutcome(t *testing.T) {
	for name, tc := range map[string]struct {
		env  func(t *testing.T) *pictureEnv
		flag string
		note string
	}{
		"nothing found": {func(t *testing.T) *pictureEnv { return newPictureEnv(t, false) }, searchEmpty, "The source page had no usable pictures."},
		"failed": {func(t *testing.T) *pictureEnv {
			return newPictureEnvOver(t, true, brokenPuts{media.NewLocal(t.TempDir())})
		}, searchFailed, "Could not search the source page."},
	} {
		t.Run(name, func(t *testing.T) {
			e := tc.env(t)
			r := e.search(false)
			r.wantStatus(t, http.StatusSeeOther)
			want := e.path() + "?" + searchParam + "=" + tc.flag
			if loc := r.Header().Get("Location"); loc != want {
				t.Fatalf("Location = %q, want %q", loc, want)
			}
			page := e.do(http.MethodGet, want, nil, nil)
			page.wantStatus(t, http.StatusOK)
			page.wantBody(t, tc.note, `class="picker edit-picture"`)
			if strings.Contains(page.body(), `hx-trigger="load"`) || strings.Contains(page.body(), "edit-picture searching") {
				t.Error("the page after a search without scripts searches again")
			}
		})
	}
}

func TestARefusedUploadWithoutScriptsSaysWhy(t *testing.T) {
	e := newPictureEnv(t, true)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(uploadField, "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("not a picture")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, e.path()+"/picture/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	r := result{rec}
	r.wantStatus(t, http.StatusBadRequest)
	r.wantBody(t, "That is not a JPEG, PNG, GIF, WebP, HEIC or AVIF picture.")
}

// A refused upload redraws the picker with the choice it was posted with: No
// picture, or a found picture, stays chosen.
func TestARefusedUploadKeepsThePickersChoice(t *testing.T) {
	e := newPictureEnv(t, true)
	if _, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2)); err != nil {
		t.Fatal(err)
	}
	r := e.uploadChoosing([]byte("not a picture"), pictureNone)
	r.wantBody(t, `class="pick-note"`)
	if got := checked(t, r.body()); got != pictureNone {
		t.Errorf("after a refused upload with No picture chosen, %q is chosen", got)
	}
	found := offerIDs(t, e.search(true).body())[0]
	r = e.uploadChoosing([]byte("not a picture"), found)
	r.wantBody(t, `class="pick-note"`)
	if got := checked(t, r.body()); got != found {
		t.Errorf("after a refused upload with a found picture chosen, %q is chosen, want %q", got, found)
	}
}

// An upload over the size limit is refused before the end of its body is read;
// the choice, posted ahead of the file, still stays chosen.
func TestAnUploadOverTheLimitKeepsThePickersChoice(t *testing.T) {
	e := newPictureEnv(t, true)
	if _, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2)); err != nil {
		t.Fatal(err)
	}
	r := e.uploadChoosing(bytes.Repeat([]byte{7}, media.MaxUploadBytes+layout.MultipartOverhead+1024), pictureNone)
	r.wantStatus(t, http.StatusOK)
	r.wantBody(t, "That file is over 5 MB.")
	if got := checked(t, r.body()); got != pictureNone {
		t.Errorf("after an upload over the limit with No picture chosen, %q is chosen", got)
	}
}

// The upload form's choice field comes ahead of its file input, and starts
// at the picker's choice.
func TestTheUploadFormPostsTheChoiceAheadOfTheFile(t *testing.T) {
	e := newPictureEnv(t, true)
	if _, err := e.pics.Upload(e.ctx, e.id, pngOf(t, 300, 200, 2)); err != nil {
		t.Fatal(err)
	}
	body := e.get().body()
	field := strings.Index(body, `type="hidden" name="picture" value="keep" data-picture-choice`)
	file := strings.Index(body, `type="file" name="file"`)
	if field < 0 || file < 0 || field > file {
		t.Errorf("choice field at %d, file input at %d; want the field first, holding keep", field, file)
	}
}
