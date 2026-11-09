package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/store"
)

// previewPage is a recipe page the rules read with high confidence, naming a
// picture so a test can tell whether picture candidates were fetched.
const previewPage = `<!doctype html>
<html><head><title>Flour Toast</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Flour Toast","author":{"@type":"Person","name":"Pat Baker"},
"image":"%s/toast.jpg","recipeYield":"2","prepTime":"PT5M","cookTime":"PT10M","totalTime":"PT15M","recipeCategory":"Breakfast","recipeCuisine":"French",
"recipeIngredient":["2 cups zz mcp flour"],
"recipeInstructions":[{"@type":"HowToStep","text":"Mix the flour."},{"@type":"HowToStep","text":"Bake for 10 minutes."}]}
</script></head><body><h1>Flour Toast</h1></body></html>`

// page serves previewPage, counting every request for its picture.
type page struct {
	srv      *httptest.Server
	pictures atomic.Int32
}

func servePreviewPage(t *testing.T) *page {
	t.Helper()
	p := &page{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jpg") {
			p.pictures.Add(1)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, previewPage, p.srv.URL)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *page) url() string { return p.srv.URL + "/toast" }

// previewSite is the endpoint drafting through the real ingestion service.
func previewSite(t *testing.T) *site {
	t.Helper()
	return newSiteDrafting(t, config.MCP{Rate: 1000, Burst: 1000}, nil, true)
}

// asMap is v as the JSON object a client decodes.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// importAndHold imports url over MCP and returns the held draft and its token.
func (s *site) importAndHold(t *testing.T, secret, url string) (ingest.Draft, string) {
	t.Helper()
	var out struct {
		ReviewPath string `json:"review_path"`
	}
	if msg := call(t, s.connect(secret), "import_recipe", map[string]any{"url": url}, &out); msg != "" {
		t.Fatal(msg)
	}
	token := strings.TrimPrefix(out.ReviewPath, "/ingest/")
	d, ok := s.draft(s.smiths, "ann@example.com", token)
	if !ok {
		t.Fatal("no draft is waiting")
	}
	return d, token
}

// previewDraft is the whole draft a preview of src makes for ann: the
// ingestion service over the site's database, configured as NewDeps configures
// the one behind preview_import.
func (s *site) previewDraft(t *testing.T, src apiingest.PreviewSource) apiingest.Preview {
	t.Helper()
	svc, err := apiingest.New(
		config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t", AllowLoopbackFetch: true},
		apiingest.Deps{Catalog: catalog.New(s.d), Members: household.New(s.d), Originals: s.rec},
	)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Preview(context.Background(), apiingest.Caller{Person: "ann@example.com", Household: s.smiths}, src)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// sameDraft fails unless got and want are the same draft: the same encoded
// document, which is everything a held draft keeps, and the same source.
func sameDraft(t *testing.T, got, want ingest.Draft) {
	t.Helper()
	g, err := ingest.EncodeDraft(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ingest.EncodeDraft(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Errorf("drafts differ:\n got:  %s\n want: %s", g, w)
	}
	if got.Source.Kind != want.Source.Kind || got.Source.SHA256 != want.Source.SHA256 || !bytes.Equal(got.Source.Content, want.Source.Content) {
		t.Errorf("sources differ: %s %s, want %s %s", got.Source.Kind, got.Source.SHA256, want.Source.Kind, want.Source.SHA256)
	}
}

func count(t *testing.T, s *site, query string) int {
	t.Helper()
	var n int
	if err := s.d.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ADR-00048 obligation 1: one page drafted through import_recipe and through
// preview_import with its url gives the same draft. The whole draft is
// compared through the service preview_import calls, and the tool's result
// against the held draft in the tool's shape.
func TestPreviewOfAURLIsTheDraftAnImportHolds(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	secret, _ := s.personalKey("ann@example.com")
	held, _ := s.importAndHold(t, secret, p.url())

	var got map[string]any
	if msg := call(t, s.connect(secret), "preview_import", map[string]any{"url": p.url()}, &got); msg != "" {
		t.Fatal(msg)
	}
	if want := asMap(t, mcp.PreviewOf(held)); !reflect.DeepEqual(got, want) {
		t.Errorf("preview differs from the held import:\n preview: %v\n import:  %v", got, want)
	}
	sameDraft(t, s.previewDraft(t, apiingest.PreviewSource{URL: p.url()}).Draft, held)
	if got["name"].(map[string]any)["value"] != "Flour Toast" {
		t.Errorf("preview name = %v, want the page's recipe", got["name"])
	}
}

// ADR-00048 obligation 2: a preview writes no proposal, snapshot, picture or
// credential write record, and runs when the household already holds the most
// drafts it may.
func TestPreviewWritesNothingAndIgnoresTheDraftBound(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	for i := 0; i < store.MaxPendingDrafts; i++ {
		if msg := call(t, cs, "import_recipe", map[string]any{"text": fmt.Sprintf("Pie %d\n\nIngredients\n- 2 cups flour\n\nInstructions\n1. Bake.\n", i)}, nil); msg != "" {
			t.Fatalf("draft %d: %s", i+1, msg)
		}
	}
	if msg := call(t, cs, "import_recipe", map[string]any{"text": "One more pie"}, nil); !strings.Contains(msg, "waiting for review") {
		t.Fatalf("the household is not full: the 21st draft = %q", msg)
	}
	tables := []string{"proposals", "source_snapshots", "draft_candidates", "recipes"}
	before := map[string]int{}
	for _, tb := range tables {
		before[tb] = count(t, s, "SELECT COUNT(*) FROM "+tb)
	}
	writes, err := s.keys.Writes(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Name struct {
			Value string `json:"value"`
		} `json:"name"`
		Note string `json:"note"`
	}
	if msg := call(t, cs, "preview_import", map[string]any{"url": p.url()}, &got); msg != "" {
		t.Fatalf("preview with %d drafts waiting: %s", store.MaxPendingDrafts, msg)
	}
	if got.Name.Value != "Flour Toast" || !strings.Contains(got.Note, "Nothing was saved or held") {
		t.Errorf("preview = %+v, want the page's recipe and a note that nothing was held", got)
	}
	for _, tb := range tables {
		if n := count(t, s, "SELECT COUNT(*) FROM "+tb); n != before[tb] {
			t.Errorf("%s rows went from %d to %d", tb, before[tb], n)
		}
	}
	if n := p.pictures.Load(); n != 0 {
		t.Errorf("the page's picture was fetched %d times", n)
	}
	if after, err := s.keys.Writes(context.Background(), key); err != nil || len(after) != len(writes) {
		t.Errorf("writes went from %d to %d (%v): a preview is not a write", len(writes), len(after), err)
	}
}

// ADR-00048 obligation 3: a saved import previewed by its recipe_id, with the
// page gone, gives the draft first held, and reports the parser version stored
// with its snapshot.
func TestPreviewOfASavedRecipeIsItsFirstHeldDraft(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	secret, _ := s.personalKey("ann@example.com")
	held, token := s.importAndHold(t, secret, p.url())

	ctx := s.as(s.smiths, "ann@example.com")
	toSave := held
	toSave.Picture = ingest.PictureNone
	conf, err := ingest.Confirm(toSave)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := ingest.NewSaver(s.rec, catalog.New(s.d)).SaveProposed(ctx, token, conf)
	if err != nil {
		t.Fatal(err)
	}
	// A later edit of the source address does not change what is replayed.
	r, err := s.rec.Get(ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	r.SourceURL = "https://example.test/moved"
	if err := s.rec.Update(ctx, saved.RecipeID, r); err != nil {
		t.Fatal(err)
	}
	p.srv.Close() // fetching is disabled from here on

	var got map[string]any
	if msg := call(t, s.connect(secret), "preview_import", map[string]any{"recipe_id": saved.RecipeID}, &got); msg != "" {
		t.Fatal(msg)
	}
	var stored string
	if err := s.d.QueryRow(`SELECT parser_version FROM source_snapshots WHERE recipe_id = ? AND kind = 'html'`, saved.RecipeID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" || got["snapshot_parser_version"] != stored {
		t.Errorf("snapshot_parser_version = %v, want the stored %q", got["snapshot_parser_version"], stored)
	}
	delete(got, "snapshot_parser_version")
	if want := asMap(t, mcp.PreviewOf(held)); !reflect.DeepEqual(got, want) {
		t.Errorf("replay differs from the draft first held:\n replay: %v\n held:   %v", got, want)
	}
	replay := s.previewDraft(t, apiingest.PreviewSource{RecipeID: saved.RecipeID})
	sameDraft(t, replay.Draft, held)
	if replay.SnapshotParserVersion != stored {
		t.Errorf("service snapshot parser version = %q, want %q", replay.SnapshotParserVersion, stored)
	}
}

// A recipe typed in, then given a source address and re-imported from it, has
// the re-import's page as its first HTML snapshot. Its replay uses the address
// the re-import fetched: the first version has none, and a later edit of the
// address changes nothing (ADR-00048, Preview).
func TestPreviewOfATypedRecipeReplaysItsReimportsAddress(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	ctx := s.as(s.smiths, "ann@example.com")
	saver := ingest.NewSaver(s.rec, catalog.New(s.d))
	save := func(token string, d ingest.Draft) int64 {
		t.Helper()
		conf, err := ingest.Confirm(d)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := saver.SaveProposed(ctx, token, conf)
		if err != nil {
			t.Fatal(err)
		}
		return saved.RecipeID
	}
	setSource := func(id int64, addr string) {
		t.Helper()
		r, err := s.rec.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		r.SourceURL = addr
		if err := s.rec.Update(ctx, id, r); err != nil {
			t.Fatal(err)
		}
	}

	var typed struct {
		ReviewPath string `json:"review_path"`
	}
	if msg := call(t, cs, "import_recipe", map[string]any{"text": "Flour Toast\n\nIngredients\n- 2 cups zz mcp flour\n\nInstructions\n1. Mix the flour.\n"}, &typed); msg != "" {
		t.Fatal(msg)
	}
	token := strings.TrimPrefix(typed.ReviewPath, "/ingest/")
	d, ok := s.draft(s.smiths, "ann@example.com", token)
	if !ok {
		t.Fatal("no draft is waiting")
	}
	id := save(token, d)
	setSource(id, p.url())

	var re struct {
		Token string `json:"token"`
	}
	if msg := call(t, cs, "reimport_recipe", map[string]any{"id": id}, &re); msg != "" {
		t.Fatal(msg)
	}
	held, ok := s.draft(s.smiths, "ann@example.com", re.Token)
	if !ok {
		t.Fatal("no re-import draft is waiting")
	}
	if got := save(re.Token, held); got != id {
		t.Fatalf("the re-import saved recipe %d, want %d", got, id)
	}
	setSource(id, "https://example.test/moved")
	p.srv.Close()

	var got struct {
		SourceURL string `json:"source_url"`
		Name      struct {
			Value string `json:"value"`
		} `json:"name"`
		Method string `json:"parse_method"`
	}
	if msg := call(t, cs, "preview_import", map[string]any{"recipe_id": id}, &got); msg != "" {
		t.Fatal(msg)
	}
	if got.SourceURL != p.url() || got.Name.Value != "Flour Toast" || got.Method != "jsonld" {
		t.Errorf("replay = %+v, want the re-imported page read at %s", got, p.url())
	}
}

// ADR-00048 obligation 4, MCP half: a high-confidence page's preview says the
// LLM was not asked. The endpoint's pipeline has no provider; the service test
// TestPreviewAsksTheLLMOnlyBelowTheThreshold shows no call is made with one.
func TestPreviewOfAHighConfidencePageSaysTheLLMWasNotAsked(t *testing.T) {
	s := previewSite(t)
	p := servePreviewPage(t)
	secret, _ := s.personalKey("ann@example.com")
	var got struct {
		LLMAsked       *bool   `json:"llm_asked"`
		BelowThreshold bool    `json:"below_threshold"`
		Overall        float64 `json:"overall_confidence"`
		Method         string  `json:"parse_method"`
		ParserVersion  string  `json:"parser_version"`
		MealType       string  `json:"meal_type"`
		Cuisine        string  `json:"cuisine"`
		PrepMinutes    struct {
			Value int `json:"value"`
		} `json:"prep_minutes"`
		TotalMinutes struct {
			Value int `json:"value"`
		} `json:"total_minutes"`
		Components []struct {
			Lines []struct {
				Raw          string  `json:"raw"`
				IngredientID int64   `json:"ingredient_id"`
				Confidence   float64 `json:"confidence"`
			} `json:"lines"`
			Steps []struct {
				Number    int   `json:"number"`
				Minutes   int   `json:"minutes"`
				DependsOn []int `json:"depends_on"`
			} `json:"steps"`
		} `json:"components"`
	}
	if msg := call(t, s.connect(secret), "preview_import", map[string]any{"url": p.url()}, &got); msg != "" {
		t.Fatal(msg)
	}
	if got.BelowThreshold {
		t.Fatalf("the page scored %v: the test needs a high-confidence page", got.Overall)
	}
	if got.LLMAsked == nil || *got.LLMAsked {
		t.Errorf("llm_asked = %v, want false and present", got.LLMAsked)
	}
	if got.Method != "jsonld" || got.ParserVersion != ingest.ParserVersion || got.MealType != "breakfast" || got.Cuisine != "french" ||
		got.PrepMinutes.Value != 5 || got.TotalMinutes.Value != 15 {
		t.Errorf("preview = %+v, want jsonld at parser %s, breakfast, french, 5 and 15 minutes", got, ingest.ParserVersion)
	}
	if len(got.Components) != 1 || len(got.Components[0].Lines) != 1 || got.Components[0].Lines[0].IngredientID != s.flour ||
		len(got.Components[0].Steps) != 2 || got.Components[0].Steps[1].Number != 2 || got.Components[0].Steps[1].Minutes != 10 {
		t.Errorf("components = %+v, want the flour line and two numbered steps", got.Components)
	}
}

func TestPreviewImportRefusals(t *testing.T) {
	s := previewSite(t)
	typed := s.recipe(s.smiths, "ann@example.com", "Typed in")
	theirs := s.recipe(s.joneses, "jo@example.com", "Jones")
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	for name, c := range map[string]struct {
		args map[string]any
		want string
	}{
		"neither":      {map[string]any{}, "give either url or recipe_id"},
		"both":         {map[string]any{"url": "https://example.test/x", "recipe_id": typed}, "give either url or recipe_id"},
		"not http":     {map[string]any{"url": "ftp://example.test/x"}, "http or https"},
		"no host":      {map[string]any{"url": "https:///x"}, "http or https"},
		"negative id":  {map[string]any{"recipe_id": -3}, "give the recipe's id"},
		"no page":      {map[string]any{"recipe_id": typed}, "not imported from a web page"},
		"other house":  {map[string]any{"recipe_id": theirs}, "not found"},
		"missing":      {map[string]any{"recipe_id": 9999}, "not found"},
		"failed fetch": {map[string]any{"url": "http://127.0.0.1:1/gone"}, "could not be fetched"},
	} {
		if msg := call(t, cs, "preview_import", c.args, nil); !strings.Contains(msg, c.want) {
			t.Errorf("%s: refusal = %q, want it to mention %q", name, msg, c.want)
		}
	}
	if w, _ := s.keys.Writes(context.Background(), key); len(w) != 0 {
		t.Errorf("writes = %+v, want none", w)
	}
}

func TestPreviewImportIsListedReadOnly(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, _ := s.personalKey("ann@example.com")
	res, err := s.connect(secret).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "preview_import" {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("preview_import annotations = %+v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Error("preview_import is not listed")
}
