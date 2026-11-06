package picture_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	domain "github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/picture"
)

type env struct {
	st      *store.Store
	hs      *household.Store
	m       media.Store
	svc     *domain.Service
	api     *apirecipes.Service
	d       *sql.DB
	own     context.Context
	foreign context.Context
	logs    *bytes.Buffer
	router  chi.Router
}

func ctxFor(t *testing.T, h household.Household, email string) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), withIdentity(httptest.NewRequest(http.MethodGet, "/", nil), email))
	return household.WithContext(got, h)
}

func withIdentity(r *http.Request, email string) *http.Request {
	r.Header.Set(auth.DevIdentityHeader, email)
	return r
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
	m := media.NewLocal(t.TempDir())
	e := &env{st: st, hs: hs, m: m, d: d, own: ctxFor(t, h1, auth.DevEmail), foreign: ctxFor(t, h2, auth.DevOtherEmail), logs: &bytes.Buffer{}}
	e.svc = domain.New(d, m, st, domain.NewFetcher(true), nil)
	log := slog.New(slog.NewJSONHandler(e.logs, nil))
	e.api = recipesService(t, st, hs, d, e.svc)
	deps := picture.Deps{Pictures: e.api, Media: m, Logger: log}
	e.router = chi.NewRouter()
	e.router.Mount(picture.Pattern, picture.Routes(deps))
	e.router.Mount(picture.DraftsPattern, picture.DraftRoutes(deps))
	return e
}

func (e *env) get(ctx context.Context, path string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (e *env) recipeWithPicture(t *testing.T) (int64, string) {
	t.Helper()
	id, err := e.st.CreateWithSnapshots(e.own, recipe.Recipe{
		Name: "Stew", SourceURL: "https://example.com/stew",
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: "water"}},
			Steps: []recipe.Step{{ID: "s1", Text: "Do it."}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	key, err := e.svc.Upload(e.own, id, pngBytes(t, 40, 30))
	if err != nil {
		t.Fatal(err)
	}
	return id, key
}

// draftWithCandidate holds a draft and gives it one candidate, found from a
// page of images served on loopback.
func (e *env) draftWithCandidate(t *testing.T) int64 {
	t.Helper()
	var ing int64
	if err := e.d.QueryRow(`SELECT id FROM ingredients WHERE household_id IS NULL ORDER BY id LIMIT 1`).Scan(&ing); err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"name":"Draft","components":[{"lines":[{"raw":"x","ingredient":%d,"confidence":0.5}]}]}`, ing)
	token, err := e.st.ProposeDraft(e.own, store.DraftInput{
		Document:   []byte(doc),
		Source:     store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("2 eggs")},
		Provenance: store.Provenance{Person: auth.DevEmail},
	})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := e.d.QueryRow(`SELECT id FROM proposals WHERE token = ?`, token).Scan(&id); err != nil {
		t.Fatal(err)
	}
	img := pngBytes(t, domain.MinWidth, domain.MinHeight)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(img) }))
	t.Cleanup(srv.Close)
	cs, err := e.svc.FetchCandidates(e.own, id, []string{srv.URL + "/a.png"})
	if err != nil || len(cs) != 1 {
		t.Fatalf("FetchCandidates = %v, %v, want one candidate", cs, err)
	}
	return id
}

func TestRecipePictureIsServedToItsHousehold(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)

	w := e.get(e.own, picture.Src(id, key))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body)
	}
	want, err := e.m.Get(e.own, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Errorf("body is not the stored picture")
	}
	if got := w.Header().Get("Content-Type"); got != "image/webp" {
		t.Errorf("Content-Type = %q, want image/webp", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestRecipePictureAtItsVersionIsImmutable(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)

	w := e.get(e.own, picture.Src(id, key))
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") || !strings.Contains(cc, "private") {
		t.Errorf("Cache-Control = %q, want private and immutable", cc)
	}
	// An address that does not name the current version must not be pinned.
	w = e.get(e.own, fmt.Sprintf("/recipes/%d/picture?v=an-old-version", id))
	if cc := w.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("a stale version's Cache-Control = %q, want it revalidated", cc)
	}
	w = e.get(e.own, fmt.Sprintf("/recipes/%d/picture", id), "If-None-Match", w.Header().Get("ETag"))
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Errorf("matching If-None-Match = %d with %d bytes, want 304 and none", w.Code, w.Body.Len())
	}
}

func TestAnotherHouseholdCannotFetchARecipePicture(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)

	w := e.get(e.foreign, picture.Src(id, key))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("webp")) || strings.Contains(w.Header().Get("Content-Type"), "image") {
		t.Errorf("a foreign household's response carries the picture")
	}
}

func TestRecipeWithoutAPictureIsNotFound(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)
	if err := e.svc.Remove(e.own, id); err != nil {
		t.Fatal(err)
	}
	if w := e.get(e.own, picture.Src(id, key)); w.Code != http.StatusNotFound {
		t.Errorf("recipe without a picture = %d, want 404", w.Code)
	}
	for _, p := range []string{"/recipes/0/picture", "/recipes/x/picture", "/recipes/999999/picture"} {
		if w := e.get(e.own, p); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
	}
}

func TestMissingObjectIsNotFoundAndLogged(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)
	if err := e.m.Delete(e.own, key); err != nil {
		t.Fatal(err)
	}

	w := e.get(e.own, picture.Src(id, key))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for the page to show the placeholder", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store so a restored object shows", cc)
	}
	if log := e.logs.String(); !strings.Contains(log, key) || !strings.Contains(log, `"level":"WARN"`) {
		t.Errorf("log = %q, want a warning naming the missing key %q", log, key)
	}
}

func TestCandidateIsServedToItsHouseholdOnly(t *testing.T) {
	e := newEnv(t)
	proposal := e.draftWithCandidate(t)
	path := picture.CandidateSrc(proposal, 1)

	w := e.get(e.own, path)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("own candidate = %d %q, want 200 image/webp", w.Code, w.Header().Get("Content-Type"))
	}
	if cc := w.Header().Get("Cache-Control"); strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q: a candidate is replaced under its key when found again", cc)
	}
	if again := e.get(e.own, path, "If-None-Match", w.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Errorf("matching If-None-Match = %d, want 304", again.Code)
	}
	if w := e.get(e.foreign, path); w.Code != http.StatusNotFound {
		t.Errorf("another household's candidate fetch = %d, want 404", w.Code)
	}
	if w := e.get(e.own, picture.CandidateSrc(proposal, 2)); w.Code != http.StatusNotFound {
		t.Errorf("a position the draft has no candidate for = %d, want 404", w.Code)
	}
}

func TestMissingCandidateObjectIsNotFoundAndLogged(t *testing.T) {
	e := newEnv(t)
	proposal := e.draftWithCandidate(t)
	cs, err := e.svc.Candidates(e.own, proposal)
	if err != nil || len(cs) != 1 {
		t.Fatal(cs, err)
	}
	if err := e.m.Delete(e.own, cs[0].Key); err != nil {
		t.Fatal(err)
	}
	if w := e.get(e.own, picture.CandidateSrc(proposal, 1)); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(e.logs.String(), cs[0].Key) {
		t.Errorf("log = %q, want the missing key %q", e.logs.String(), cs[0].Key)
	}
}

func TestStoreFailureIsNotMistakenForAMissingPicture(t *testing.T) {
	e := newEnv(t)
	id, key := e.recipeWithPicture(t)
	h := picture.Routes(picture.Deps{Pictures: e.api, Media: failingMedia{}, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	r := chi.NewRouter()
	r.Mount(picture.Pattern, h)
	req := httptest.NewRequest(http.MethodGet, picture.Src(id, key), nil).WithContext(e.own)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("a failing store = %d, want 500", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" || w.Header().Get("ETag") != "" {
		t.Errorf("a failing store answered Cache-Control %q, ETag %q, want no-store and no ETag: an error must not be cached as the picture", cc, w.Header().Get("ETag"))
	}
}

type failingMedia struct{}

func (failingMedia) Get(context.Context, string) ([]byte, error) { return nil, io.ErrUnexpectedEOF }

func recipesService(t *testing.T, st *store.Store, hs *household.Store, d *sql.DB, p *apirecipes.Pictures) *apirecipes.Service {
	t.Helper()
	svc, err := apirecipes.New(apirecipes.Deps{Recipes: st, Catalog: catalog.New(d), Members: hs, Pictures: p})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
