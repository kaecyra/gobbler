package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	"github.com/kaecyra/gobbler/internal/web/editor"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

const threshold = 0.7

// fakeDrafter returns a fixed draft or error and counts calls.
type fakeDrafter struct {
	draft ingestcore.Draft
	err   error
	calls int
}

func (f *fakeDrafter) FromURL(context.Context, string) (ingestcore.Draft, error) {
	f.calls++
	return f.draft, f.err
}

func (f *fakeDrafter) FromText(context.Context, string) (ingestcore.Draft, error) {
	f.calls++
	return f.draft, f.err
}

// fakeStore is the recipe store and the catalog behind a real ingest.Saver, so
// tests see exactly what a confirmed save would write.
type fakeStore struct {
	mu        sync.Mutex
	recipes   []recipe.Recipe
	snapshots int
	created   []string
	known     map[string]catalog.Ingredient
	// createdEquipment are the names of equipment a save created.
	createdEquipment []string
	// thumbs are the video IDs whose thumbnail a save stored.
	thumbs map[string]bool
	// drafts is the proposals table the drafts are held in.
	drafts *store.Store
}

func (s *fakeStore) HasThumbnail(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thumbs[id], nil
}

func (s *fakeStore) SaveThumbnail(_ context.Context, id string, _ []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.thumbs == nil {
		s.thumbs = map[string]bool{}
	}
	s.thumbs[id] = true
	return nil
}

// stubThumbs stands in for YouTube's image host, so no test reaches the network.
type stubThumbs struct{}

func (stubThumbs) Fetch(_ context.Context, id string) (video.Thumbnail, error) {
	return video.Thumbnail{Content: []byte("jpeg:" + id), ContentType: "image/jpeg"}, nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{known: map[string]catalog.Ingredient{
		"butter": {ID: 7, Name: "butter"},
	}}
}

// CreateFromDraft stands in for the store's: it records the recipe and ends the
// draft, as the real one does in the same transaction by marking it accepted.
// Whether the real one attaches the snapshot and refuses a second confirm is
// the store's own tests' to show.
func (s *fakeStore) CreateFromDraft(ctx context.Context, token string, r recipe.Recipe) (int64, error) {
	if err := s.drafts.RejectProposal(ctx, token); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recipes = append(s.recipes, r)
	s.snapshots++
	return int64(len(s.recipes)), nil
}

// AcceptReimport is the real store's: a re-import's save and its refusal of a
// recipe that moved on are the store's own tests' to show; here it lets the
// handler's reading of them be seen.
func (s *fakeStore) AcceptReimport(ctx context.Context, token string, r recipe.Recipe) (int64, error) {
	return s.drafts.AcceptReimport(ctx, token, r)
}

func (s *fakeStore) CreateWithSnapshots(_ context.Context, r recipe.Recipe, snaps ...store.SnapshotInput) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recipes = append(s.recipes, r)
	s.snapshots += len(snaps)
	return int64(len(s.recipes)), nil
}

func (s *fakeStore) MatchIngredient(_ context.Context, term string) (catalog.Ingredient, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ing, ok := s.known[strings.ToLower(term)]
	return ing, ok, nil
}

// fakeUnits are the units the fake catalog knows.
var fakeUnits = []string{"tbsp", "cup"}

func (s *fakeStore) MatchUnit(_ context.Context, term string) (catalog.Unit, bool, error) {
	for i, n := range fakeUnits {
		if strings.EqualFold(term, n) {
			return catalog.Unit{ID: int64(i + 1), Unit: quantity.OtherUnit(n)}, true, nil
		}
	}
	return catalog.Unit{}, false, nil
}

func (s *fakeStore) ListUnits(context.Context) ([]catalog.Unit, error) {
	var out []catalog.Unit
	for i, n := range fakeUnits {
		out = append(out, catalog.Unit{ID: int64(i + 1), Unit: quantity.OtherUnit(n)})
	}
	return out, nil
}

func (s *fakeStore) ListIngredients(context.Context) ([]catalog.Ingredient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []catalog.Ingredient
	for _, i := range s.known {
		out = append(out, i)
	}
	return out, nil
}

var fakeEquipment = []catalog.Equipment{{ID: 5, Name: "dutch oven"}, {ID: 6, Name: "whisk"}}

func (s *fakeStore) MatchEquipment(_ context.Context, term string) (catalog.Equipment, bool, error) {
	for _, e := range fakeEquipment {
		if strings.EqualFold(term, e.Name) {
			return e, true, nil
		}
	}
	return catalog.Equipment{}, false, nil
}

func (s *fakeStore) ListEquipment(context.Context) ([]catalog.Equipment, error) {
	return fakeEquipment, nil
}

func (s *fakeStore) CreateEquipment(_ context.Context, in catalog.Equipment) (catalog.Equipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = int64(200 + len(s.createdEquipment))
	s.createdEquipment = append(s.createdEquipment, in.Name)
	return in, nil
}

func (s *fakeStore) CreateIngredient(_ context.Context, in catalog.Ingredient) (catalog.Ingredient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = int64(100 + len(s.created))
	s.created = append(s.created, in.Name)
	return in, nil
}

func sampleDraft() ingestcore.Draft {
	content := []byte("<html>the page</html>")
	sum := sha256.Sum256(content)
	return ingestcore.Draft{
		ParserVersion: ingestcore.ParserVersion,
		Source:        ingestcore.Source{Kind: store.SnapshotHTML, URL: "https://example.test/pie", Content: content, SHA256: hex.EncodeToString(sum[:])},
		Name:          ingestcore.Field[string]{Value: "Apple pie", Confidence: 1},
		Author:        ingestcore.Field[string]{Value: "Ada", Confidence: 1},
		Prep:          ingestcore.Field[time.Duration]{Value: 20 * time.Minute, Confidence: 1},
		Components: []ingestcore.Component{{
			Lines: []ingestcore.Line{
				{Raw: "2 tbsp butter", Name: "butter", Ingredient: 7, Confidence: 1,
					Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2)), Unit: quantity.OtherUnit("tbsp")}},
				{Raw: "1 dragonfruit zest", Name: "dragonfruit zest", Confidence: 0.4, Issues: []string{`ingredient "dragonfruit zest" not matched`}},
			},
			Steps: []ingestcore.Step{
				{Text: "Mix everything.", Confidence: 1},
				{Text: "Bake until goldn", Confidence: 0.3},
			},
		}},
		Overall:        0.5,
		Threshold:      threshold,
		BelowThreshold: true,
		Issues:         []string{"no_steps"},
		Proposed:       []ingestcore.ProposedIngredient{{Name: "dragonfruit zest", Aisle: "uncategorised"}},
		Equipment: []ingestcore.Equipment{
			{Name: "dutch oven", Equipment: 5},
			{Name: "flux capacitor"},
			{Name: "whisk", Equipment: 6},
		},
	}
}

// Two households, as the membership middleware would resolve them. A fixture
// acts as householdA; the tests of draft ownership also act as householdB.
var (
	householdA = household.Household{ID: 1, Name: "A"}
	householdB = household.Household{ID: 2, Name: "B"}
)

// memberEmails are the people who sign in as each household's member.
var memberEmails = map[int64]string{householdA.ID: auth.DevEmail, householdB.ID: auth.DevOtherEmail}

// asMember puts h on every request, as the membership middleware does for a
// member, and the verified identity of that household's member, as the
// authentication middleware does (here by its development bypass).
func asMember(h household.Household) func(http.Handler) http.Handler {
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		panic(err)
	}
	return func(next http.Handler) http.Handler {
		inner := a.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set(auth.DevIdentityHeader, memberEmails[h.ID])
			inner.ServeHTTP(w, r)
		})
	}
}

type fixture struct {
	h       *handler
	rs      *testRecipes
	srv     http.Handler
	drafter *fakeDrafter
	store   *fakeStore
	// db holds the proposals the drafts wait in.
	db *sql.DB
}

// ctx is the context of a request by household A's member.
func (f *fixture) ctx(t *testing.T) context.Context {
	t.Helper()
	var got context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	asMember(householdA)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// pendingDrafts is how many drafts are waiting for review, in every household.
func (f *fixture) pendingDrafts(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM proposals WHERE kind = 'draft' AND state = 'pending'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// holdDraft replaces the pending draft under token, as a retry or a failed
// confirm does.
func (f *fixture) holdDraft(t *testing.T, token string, d ingestcore.Draft) {
	t.Helper()
	if err := f.rs.drafts.Replace(f.ctx(t), token, d); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(ctx, conn, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	for i, email := range []string{auth.DevEmail, auth.DevOtherEmail} {
		if _, err := household.New(conn).Create(ctx, fmt.Sprintf("Household %d", i+1), email); err != nil {
			t.Fatal(err)
		}
	}
	rec := store.New(conn)
	st := newFakeStore()
	st.drafts = rec
	d := &fakeDrafter{draft: sampleDraft()}
	rs := &testRecipes{
		saver:  ingestcore.NewSaver(st, st).WithThumbnails(stubThumbs{}),
		drafts: ingestcore.NewDrafts(rec),
	}
	h := &handler{
		drafter:    d,
		recipes:    rs,
		unsaved:    editor.NewUnsaved(st),
		photoCache: ingestcore.NewPhotos(time.Now),
		log:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	return &fixture{h: h, rs: rs, srv: asMember(householdA)(h.routes()), drafter: d, store: st, db: conn}
}

func newFormRequest(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func (f *fixture) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	f.srv.ServeHTTP(rec, req)
	return rec
}

// ingestText submits pasted text and returns the review token.
func (f *fixture) ingestText(t *testing.T) string {
	t.Helper()
	rec := f.do(http.MethodPost, "/", url.Values{"text": {"Apple pie\n2 tbsp butter"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303; body: %s", rec.Code, rec.Body)
	}
	loc := rec.Header().Get("Location")
	token, ok := strings.CutPrefix(loc, "/ingest/")
	if !ok || token == "" {
		t.Fatalf("redirect = %q, want /ingest/<token>", loc)
	}
	return token
}

func TestFormRenders(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodGet, "/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, want := range []string{`name="url"`, `name="text"`, `action="/ingest"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("form missing %s", want)
		}
	}
}

func TestSubmitValidation(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{"empty", url.Values{}, "Enter a recipe URL"},
		{"blank text", url.Values{"text": {"  \n "}}, "Enter a recipe URL"},
		{"both", url.Values{"url": {"https://a.test/x"}, "text": {"pie"}}, "not both"},
		{"malformed", url.Values{"url": {"not a url"}}, "http:// or https://"},
		{"wrong scheme", url.Values{"url": {"ftp://a.test/x"}}, "http:// or https://"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			rec := f.do(http.MethodPost, "/", tc.form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body lacks %q", tc.want)
			}
			if f.drafter.calls != 0 {
				t.Error("pipeline ran for invalid input")
			}
		})
	}
}

func TestSubmitKeepsInputOnError(t *testing.T) {
	f := newFixture(t)
	rec := f.do(http.MethodPost, "/", url.Values{"url": {"https://a.test/x"}, "text": {"my pasted pie"}})
	if !strings.Contains(rec.Body.String(), "my pasted pie") || !strings.Contains(rec.Body.String(), "https://a.test/x") {
		t.Error("the user's input was lost")
	}
}

func TestSubmitFetchErrorShowsOnFormAndRendersNoDraft(t *testing.T) {
	f := newFixture(t)
	f.drafter.err = fmt.Errorf("%w: get https://a.test/x: connection refused", ingestcore.ErrFetch)
	rec := f.do(http.MethodPost, "/", url.Values{"url": {"https://a.test/x"}})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Could not fetch that page") || !strings.Contains(body, `name="url"`) {
		t.Errorf("error not shown on the form: %s", body)
	}
	if strings.Contains(body, "Review recipe") {
		t.Error("a draft was rendered for a failed fetch")
	}
	if f.pendingDrafts(t) != 0 {
		t.Error("a failed ingest left a pending draft")
	}
}

func TestSubmitUnexpectedPipelineErrorIs500(t *testing.T) {
	f := newFixture(t)
	f.drafter.err = errors.New("catalog unreadable")
	rec := f.do(http.MethodPost, "/", url.Values{"text": {"pie"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "catalog unreadable") {
		t.Error("an internal error reached the page")
	}
}

func TestReviewShowsDraftAndSavesNothing(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	rec := f.do(http.MethodGet, "/"+token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Review recipe", "Apple pie", "2 tbsp butter", "Mix everything.", "20 min", "Nothing is saved until you confirm"} {
		if !strings.Contains(body, want) {
			t.Errorf("review lacks %q", want)
		}
	}
	if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
		t.Error("the ingest or review step saved something")
	}
}

func TestReviewShowsTheParserVersion(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if want := "parser " + ingestcore.ParserVersion; !strings.Contains(body, want) {
		t.Errorf("review lacks %q", want)
	}
}
func TestConfirmSavesEquipmentAndCreatesTheProposedOne(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if len(f.store.createdEquipment) != 1 || f.store.createdEquipment[0] != "flux capacitor" {
		t.Errorf("created equipment = %v, want the proposed one", f.store.createdEquipment)
	}
	var ids []recipe.EquipmentID
	for _, l := range f.store.recipes[0].Equipment {
		ids = append(ids, l.Equipment)
	}
	if want := []recipe.EquipmentID{5, 200, 6}; !reflect.DeepEqual(ids, want) {
		t.Errorf("saved equipment ids = %v, want %v", ids, want)
	}
}

func TestReviewUnknownTokenIs404(t *testing.T) {
	f := newFixture(t)
	if rec := f.do(http.MethodGet, "/nope", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestConfirmSavesAcceptedNewIngredientAndRedirects(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("name", "  Best apple pie ")
	form.Set("c.0.s.1.text", "Bake until golden.")
	rec := f.do(http.MethodPost, "/"+token+"/confirm", form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/recipes/1" {
		t.Fatalf("status = %d location = %q, want 303 /recipes/1; body: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(f.store.recipes) != 1 || f.store.snapshots != 1 {
		t.Fatalf("recipes = %d snapshots = %d, want one each", len(f.store.recipes), f.store.snapshots)
	}
	r := f.store.recipes[0]
	if r.Name != "Best apple pie" {
		t.Errorf("name = %q, edit not applied", r.Name)
	}
	if got := r.Components[0].Steps[1].Text; got != "Bake until golden." {
		t.Errorf("step = %q, edit not applied", got)
	}
	if len(f.store.created) != 1 || f.store.created[0] != "dragonfruit zest" {
		t.Errorf("created ingredients = %v, want the proposed one", f.store.created)
	}
	if rec := f.do(http.MethodGet, "/"+token, nil); rec.Code != http.StatusNotFound {
		t.Error("the draft is still reviewable after saving")
	}
}

func TestConfirmMapsProposedLineToExistingIngredient(t *testing.T) {
	f := newFixture(t)
	f.store.known["zest"] = catalog.Ingredient{ID: 9, Name: "zest"}
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("c.0.l.1.ing", "Zest")
	form.Del("c.0.l.1.propose")
	rec := f.do(http.MethodPost, "/"+token+"/confirm", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if len(f.store.created) != 0 {
		t.Errorf("created %v, but the line was mapped to an existing ingredient", f.store.created)
	}
	if got := f.store.recipes[0].Components[0].Lines[1].Ingredient; got != 9 {
		t.Errorf("line ingredient = %d, want 9", got)
	}
}
func TestConfirmIncompleteDraftKeepsEditsAndSavesNothing(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("name", "")
	form.Set("author", "Grace")
	rec := f.do(http.MethodPost, "/"+token+"/confirm", form)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "A recipe needs a name.") {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
		t.Error("an incomplete draft was saved")
	}
	if !strings.Contains(rec.Body.String(), `value="Grace"`) {
		t.Error("the user's other edits were lost")
	}
}

func TestConfirmTwiceSavesOnce(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("first confirm status = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm()); rec.Code != http.StatusNotFound {
		t.Fatalf("second confirm status = %d, want 404", rec.Code)
	}
	if len(f.store.recipes) != 1 {
		t.Errorf("recipes saved = %d, want 1", len(f.store.recipes))
	}
}

type failingSaver struct{}

func (failingSaver) SaveProposed(context.Context, string, ingestcore.Confirmed) (ingestcore.Saved, error) {
	return ingestcore.Saved{}, errors.New("disk on fire")
}

func TestConfirmSaveFailureKeepsDraftAndHidesDetail(t *testing.T) {
	f := newFixture(t)
	f.rs.saver = failingSaver{}
	token := f.ingestText(t)
	rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm())
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "disk on fire") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if f.do(http.MethodGet, "/"+token, nil).Code != http.StatusOK {
		t.Error("the draft was lost when saving failed")
	}
}

func TestCancelDiscardsAndSavesNothing(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	rec := f.do(http.MethodPost, "/"+token+"/cancel", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/ingest" {
		t.Fatalf("status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
		t.Error("cancel saved something")
	}
	if f.do(http.MethodGet, "/"+token, nil).Code != http.StatusNotFound {
		t.Error("the draft survived cancel")
	}
}

func TestRoutesWithBadConfigAnswerLoudly(t *testing.T) {
	h := Routes(Deps{Ingestion: config.Ingestion{}, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	routes := []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodPost, "/"},
		{http.MethodGet, "/sometoken"},
		{http.MethodPost, "/sometoken/confirm"},
		{http.MethodPost, "/sometoken/cancel"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.target, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.target, strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
		})
	}
}

func TestSubmitMapsPipelineErrorsThroughTheStatusTable(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		want   string
	}{
		{"fetch", fmt.Errorf("%w: get https://a.test/x: refused", ingestcore.ErrFetch), http.StatusBadGateway, "Could not fetch that page"},
		{"fetch wrapping a not-found cause", fmt.Errorf("%w: get x: %w", ingestcore.ErrFetch, layout.ErrNotFound), http.StatusBadGateway, "Could not fetch that page"},
		{"empty text", fmt.Errorf("split: %w", parse.ErrEmptyText), http.StatusBadRequest, "nothing to read"},
		{"empty html", fmt.Errorf("extract: %w", extract.ErrEmptyInput), http.StatusBadRequest, "nothing to read"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.drafter.err = tc.err
			rec := f.do(http.MethodPost, "/", url.Values{"text": {"pie"}})
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.status, rec.Body)
			}
			if layout.StatusFor(tc.err) != tc.status {
				t.Errorf("layout.StatusFor = %d, want %d", layout.StatusFor(tc.err), tc.status)
			}
		})
	}
}
func TestIncompleteProblemsUnwrapsEachProblem(t *testing.T) {
	err := fmt.Errorf("%w: %w", ingestcore.ErrIncompleteDraft,
		errors.Join(errors.New("the recipe has no name"), errors.New("line one\nline two")))
	got := incompleteProblems(err)
	want := []string{"The recipe has no name.", "Line one\nline two."}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("problems = %q, want %q", got, want)
	}
}

// between returns the text after the first start and before the next end.
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("%q not found in page", start)
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, end)
	if j < 0 {
		t.Fatalf("%q not found after %q", end, start)
	}
	return rest[:j]
}

type unknownUnitSaver struct{}

func (unknownUnitSaver) SaveProposed(context.Context, string, ingestcore.Confirmed) (ingestcore.Saved, error) {
	return ingestcore.Saved{}, fmt.Errorf("component 1 line 2 (%q): %w", "3 zorbs butter", &ingestcore.UnknownUnitError{Name: "zorb"})
}

// A unit the catalog no longer knows is a problem with the draft, not a broken
// pipeline: the review screen comes back with the problem and the draft stays.
func TestConfirmUnknownUnitReturnsToReviewWithProblem(t *testing.T) {
	f := newFixture(t)
	f.rs.saver = unknownUnitSaver{}
	token := f.ingestText(t)
	rec := f.do(http.MethodPost, "/"+token+"/confirm", reviewForm())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if strings.Count(body, "not in the catalog") != 1 || !strings.Contains(body, "zorb") {
		t.Errorf("the problem must name the unit and say catalog once: %s", body)
	}
	if f.do(http.MethodGet, "/"+token, nil).Code != http.StatusOK {
		t.Error("the draft was lost")
	}
}

// reviewForm is the review form as the browser would send it untouched for
// sampleDraft: the editor's own field names.
func reviewForm() url.Values {
	return url.Values{
		"name":            {"Apple pie"},
		"author":          {"Ada"},
		"prep":            {"20 min"},
		"c.0.name":        {""},
		"c.0.l.0.qty":     {"2"},
		"c.0.l.0.unit":    {"tbsp"},
		"c.0.l.0.ing":     {"butter"},
		"c.0.l.0.rule":    {"linear"},
		"c.0.l.0.raw":     {"2 tbsp butter"},
		"c.0.l.1.ing":     {"dragonfruit zest"},
		"c.0.l.1.propose": {"1"},
		"c.0.l.1.rule":    {"linear"},
		"c.0.l.1.raw":     {"1 dragonfruit zest"},
		"c.0.s.0.text":    {"Mix everything."},
		"c.0.s.0.to":      {"0"},
		"c.0.s.1.text":    {"Bake until goldn"},
		"c.0.s.1.to":      {"0"},
		"e.0.name":        {"dutch oven"},
		"e.1.name":        {"flux capacitor"},
		"e.1.propose":     {"1"},
		"e.2.name":        {"whisk"},
	}
}
