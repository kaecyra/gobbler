package mcp_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
)

// testSite is where the test endpoint claims to be reached; a token is bound to the
// MCP endpoint under it.
var testSite = mcp.Site{Origin: "https://gobbler.example", MetadataURL: "https://gobbler.example/.well-known/oauth-protected-resource/mcp"}

// site is the MCP endpoint over real stores, behind the same two layers the
// router puts in front of it: Bearer, and a membership step that puts the
// credential owner's household on the context.
type site struct {
	t       *testing.T
	d       *sql.DB
	keys    *apikey.Store
	oauth   *oauth.Store
	drafts  *ingest.Drafts
	rec     *store.Store
	srv     *httptest.Server
	now     time.Time
	flour   int64
	smiths  household.Household
	joneses household.Household
	// media holds the picture objects of the recipe service's picture
	// service.
	media media.Store
}

type fakeDrafter struct{ ingest.Draft }

// fetches counts the calls any fakeDrafter has been asked to make.
var fetches atomic.Int32

func (f fakeDrafter) FromURL(context.Context, string) (ingest.Draft, error) {
	fetches.Add(1)
	return f.Draft, nil
}
func (f fakeDrafter) FromText(_ context.Context, text string) (ingest.Draft, error) {
	fetches.Add(1)
	d := f.Draft
	d.Source.Content = []byte(text)
	return d, nil
}

func newSite(t *testing.T, limits config.MCP) *site { return newSiteWith(t, limits, nil) }

// newSiteWith is newSite with the drafter replaced, when drafter is not nil.
func newSiteWith(t *testing.T, limits config.MCP, drafter func(*catalog.Store) mcp.Drafter) *site {
	t.Helper()
	return newSiteDrafting(t, limits, drafter, false)
}

// newSiteDrafting is newSiteWith that, with service true, keeps the drafter
// NewDeps built (the ingestion service bound to the request's credential)
// instead of the fake, so a test drives the real service.
func newSiteDrafting(t *testing.T, limits config.MCP, drafter func(*catalog.Store) mcp.Drafter, service bool) *site {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	s := &site{t: t, d: d, keys: apikey.New(d), oauth: oauth.New(d), rec: store.New(d), now: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)}
	s.drafts = ingest.NewDrafts(s.rec)
	hs := household.New(d)
	if s.smiths, err = hs.Create(ctx, "Smiths", "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if s.joneses, err = hs.Create(ctx, "Joneses", "jo@example.com"); err != nil {
		t.Fatal(err)
	}
	cat := catalog.New(d)
	ing, err := cat.CreateIngredient(s.as(s.smiths, "ann@example.com"), catalog.Ingredient{Name: "zz mcp flour", Aisle: "baking"})
	if err != nil {
		t.Fatal(err)
	}
	s.flour = ing.ID
	shop := shopping.New(d, s.rec, cat)
	draft := ingest.Draft{Name: ingest.Field[string]{Value: "Drafted pie", Confidence: 1}, Source: ingest.Source{Kind: store.SnapshotText, Content: []byte("a page")}}
	cfg := config.Config{Ingestion: config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t", AllowLoopbackFetch: true}, MCP: limits}
	s.media = media.NewLocal(t.TempDir())
	pictures := apirecipes.NewPictures(d, s.media, s.rec, cfg.Ingestion, slog.New(slog.DiscardHandler))
	svc, err := apirecipes.New(apirecipes.Deps{Recipes: s.rec, Catalog: cat, Members: hs, Pictures: pictures})
	if err != nil {
		t.Fatal(err)
	}
	// NewDeps is what the router uses, so every tool here reaches the catalog
	// through the catalog service bound to the credential of its request
	// (ADR-00043), whichever kind of credential it is (ADR-00031).
	audit := mcp.AuditByKind{mcp.KindAPIKey: mcp.KeyAudit{Keys: s.keys}, mcp.KindOAuth: mcp.GrantAudit{Grants: s.oauth}}
	deps, err := mcp.NewDeps(svc, s.rec, shop, hs, cat, audit, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	deps.Now = func() time.Time { return s.now }
	if !service {
		deps.Drafter = fakeDrafter{draft}
	}
	if drafter != nil {
		deps.Drafter = drafter(cat)
	}
	h, err := mcp.Routes(deps)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	member := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, _ := mcp.CredentialFrom(r.Context())
			hh := household.Household{ID: c.HouseholdID, Name: c.HouseholdName}
			if c.HouseholdID == 0 {
				var err error
				if hh, err = hs.ForEmail(r.Context(), c.Email); err != nil {
					http.Error(w, "not a member", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), hh)))
		})
	}
	verifier := mcp.OAuthVerifier{Tokens: s.oauth, Next: mcp.KeyVerifier{Keys: s.keys}}
	// Package web puts the site on the context before Bearer; so does this.
	withSite := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(mcp.WithSite(r.Context(), testSite)))
		})
	}
	s.srv = httptest.NewServer(withSite(mcp.Bearer(verifier, log)(member(h))))
	t.Cleanup(s.srv.Close)
	return s
}

// as returns a context for seeding as a member of h.
// draft reads the pending draft for token as a member of h.
func (s *site) draft(h household.Household, email, token string) (ingest.Draft, bool) {
	s.t.Helper()
	d, err := s.drafts.Get(s.as(h, email), token)
	if err != nil && !errors.Is(err, ingest.ErrDraftGone) {
		s.t.Fatal(err)
	}
	return d, err == nil
}

func (s *site) as(h household.Household, email string) context.Context {
	s.t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		s.t.Fatal(err)
	}
	var got context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	return household.WithContext(got, h)
}

type bearer struct{ secret string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.secret)
	return http.DefaultTransport.RoundTrip(r)
}

func (s *site) connect(secret string) *sdk.ClientSession {
	s.t.Helper()
	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: s.srv.URL, HTTPClient: &http.Client{Transport: bearer{secret}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		s.t.Fatalf("connect: %v", err)
	}
	s.t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func (s *site) personalKey(email string) (string, apikey.Principal) {
	s.t.Helper()
	secret, err := s.keys.CreatePersonal(context.Background(), email, "test", 0)
	if err != nil {
		s.t.Fatal(err)
	}
	p, err := s.keys.Lookup(context.Background(), secret)
	if err != nil {
		s.t.Fatal(err)
	}
	return secret, p
}

// call runs a tool and decodes its structured result into out; it returns the
// tool's error text, empty on success.
func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		var msg strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				msg.WriteString(tc.Text)
			}
		}
		return msg.String()
	}
	if out != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s: decode %s: %v", name, raw, err)
		}
	}
	return ""
}

func (s *site) recipe(h household.Household, email, name string, lines ...recipe.IngredientLine) int64 {
	s.t.Helper()
	id, err := s.rec.Create(s.as(h, email), recipe.Recipe{Name: name, Components: []recipe.Component{{Lines: lines, Steps: []recipe.Step{{ID: "s1", Text: "Mix."}}}}})
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s *site) flourLine(cups int64) recipe.IngredientLine {
	return recipe.IngredientLine{
		Ingredient: recipe.IngredientID(s.flour), Rule: quantity.RuleLinear, Raw: "flour",
		Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(cups)), Unit: quantity.MustUnit("cup")},
	}
}

type pageOf[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func TestToolsAreListed(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, _ := s.personalKey("ann@example.com")
	res, err := s.connect(secret).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"search_recipes", "list_recipes", "get_recipe", "import_recipe", "create_recipe_draft", "propose_recipe_edit", "get_recipe_proposal",
		"get_recipe_draft", "list_recipe_drafts", "revise_recipe_draft",
		"list_shopping_lists", "get_shopping_list", "create_shopping_list", "add_recipe_to_shopping_list",
		"add_manual_shopping_item", "check_shopping_item", "get_pantry", "get_house_style"} {
		if !got[want] {
			t.Errorf("tool %s is not listed", want)
		}
	}
}

func TestSearchAndGetRecipeReadOnlyTheCredentialsHousehold(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	mine := s.recipe(s.smiths, "ann@example.com", "Smith pancakes", s.flourLine(2))
	theirs := s.recipe(s.joneses, "jo@example.com", "Jones pancakes")
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)

	var found pageOf[struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}]
	if msg := call(t, cs, "search_recipes", map[string]any{"query": "pancakes"}, &found); msg != "" {
		t.Fatal(msg)
	}
	if len(found.Items) != 1 || found.Items[0].ID != mine {
		t.Errorf("search = %+v, want only the Smiths' recipe %d", found.Items, mine)
	}
	var got struct {
		Name       string `json:"name"`
		Components []struct {
			Ingredients []struct {
				Text string `json:"text"`
			} `json:"ingredients"`
			Steps []struct{ Text string } `json:"steps"`
		} `json:"components"`
	}
	if msg := call(t, cs, "get_recipe", map[string]any{"id": mine}, &got); msg != "" {
		t.Fatal(msg)
	}
	if got.Name != "Smith pancakes" || len(got.Components) != 1 || got.Components[0].Ingredients[0].Text != "2 cup zz mcp flour" || got.Components[0].Steps[0].Text != "Mix." {
		t.Errorf("get_recipe = %+v", got)
	}
	// Another household's recipe is not found, as if it did not exist.
	if msg := call(t, cs, "get_recipe", map[string]any{"id": theirs}, nil); msg != "not found" {
		t.Errorf("get_recipe of another household's = %q, want not found", msg)
	}
}

func TestAHouseholdKeyActsForItsHousehold(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.joneses, "jo@example.com", "Jones stew")
	secret, err := s.keys.CreateHousehold(context.Background(), s.joneses.ID, "jo@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	var page pageOf[struct {
		ID int64 `json:"id"`
	}]
	if msg := call(t, s.connect(secret), "list_recipes", nil, &page); msg != "" || len(page.Items) != 1 || page.Items[0].ID != id {
		t.Errorf("list_recipes = %+v %q", page, msg)
	}
}

func TestListToolsReturnOnePageWithACursor(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	for i := 0; i < 7; i++ {
		s.recipe(s.smiths, "ann@example.com", fmt.Sprintf("Soup %02d", i))
	}
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var seen []string
	cursor := ""
	for pages := 0; ; pages++ {
		var p pageOf[struct {
			Name string `json:"name"`
		}]
		args := map[string]any{"limit": 3}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if msg := call(t, cs, "list_recipes", args, &p); msg != "" {
			t.Fatal(msg)
		}
		if len(p.Items) > 3 {
			t.Fatalf("page %d holds %d items, want at most 3", pages, len(p.Items))
		}
		for _, it := range p.Items {
			seen = append(seen, it.Name)
		}
		if cursor = p.NextCursor; cursor == "" {
			if pages != 2 {
				t.Errorf("last page was number %d, want 3 pages for 7 recipes", pages+1)
			}
			break
		}
	}
	if len(seen) != 7 {
		t.Errorf("saw %d recipes across pages, want 7: %v", len(seen), seen)
	}
	// A page never exceeds the maximum, whatever the client asks.
	for i := 7; i < mcp.MaxPageSize+5; i++ {
		s.recipe(s.smiths, "ann@example.com", fmt.Sprintf("Soup %02d", i))
	}
	var big pageOf[struct{}]
	if msg := call(t, cs, "list_recipes", map[string]any{"limit": 1000}, &big); msg != "" || len(big.Items) != mcp.MaxPageSize || big.NextCursor == "" {
		t.Errorf("limit 1000 gave %d items (cursor %q) %q, want %d and a cursor", len(big.Items), big.NextCursor, msg, mcp.MaxPageSize)
	}
	if msg := call(t, cs, "list_recipes", map[string]any{"cursor": "bogus"}, nil); !strings.Contains(msg, "invalid cursor") {
		t.Errorf("bad cursor: %q", msg)
	}
}

func TestShoppingWritesApplyAtOnceAndRecordTheKey(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	rid := s.recipe(s.smiths, "ann@example.com", "Bread", s.flourLine(2))
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)

	var list struct {
		ID int64 `json:"id"`
	}
	if msg := call(t, cs, "create_shopping_list", map[string]any{"name": "Week"}, &list); msg != "" {
		t.Fatal(msg)
	}
	if msg := call(t, cs, "add_recipe_to_shopping_list", map[string]any{"list_id": list.ID, "recipe_id": rid, "scale": "2"}, nil); msg != "" {
		t.Fatal(msg)
	}
	if msg := call(t, cs, "add_manual_shopping_item", map[string]any{"list_id": list.ID, "text": "paper towels"}, nil); msg != "" {
		t.Fatal(msg)
	}
	var got struct {
		Items []struct {
			Name    string   `json:"name"`
			Amounts []string `json:"amounts"`
			Key     string   `json:"key"`
			Checked bool     `json:"checked"`
		} `json:"items"`
	}
	if msg := call(t, cs, "get_shopping_list", map[string]any{"id": list.ID}, &got); msg != "" {
		t.Fatal(msg)
	}
	// Applied at once: the list already holds the scaled recipe and the item.
	if len(got.Items) != 2 {
		t.Fatalf("items = %+v", got.Items)
	}
	var flourKey string
	for _, it := range got.Items {
		if it.Name == "zz mcp flour" && len(it.Amounts) == 1 && it.Amounts[0] == "4 cup" {
			flourKey = it.Key
		}
	}
	if flourKey == "" {
		t.Fatalf("no 4 cups of flour in %+v", got.Items)
	}
	if msg := call(t, cs, "check_shopping_item", map[string]any{"list_id": list.ID, "key": flourKey, "checked": true}, nil); msg != "" {
		t.Fatal(msg)
	}
	call(t, cs, "get_shopping_list", map[string]any{"id": list.ID}, &got)
	for _, it := range got.Items {
		if it.Key == flourKey && !it.Checked {
			t.Error("the flour is not ticked off")
		}
	}

	writes, err := s.keys.Writes(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, w := range writes {
		if w.HouseholdID != s.smiths.ID {
			t.Errorf("write %+v recorded against household %d", w, w.HouseholdID)
		}
		tools = append(tools, w.Tool)
	}
	want := "create_shopping_list,add_recipe_to_shopping_list,add_manual_shopping_item,check_shopping_item"
	if strings.Join(tools, ",") != want {
		t.Errorf("recorded writes = %v, want %s", tools, want)
	}
	// Reads are not writes.
	var lists pageOf[struct{}]
	call(t, cs, "list_shopping_lists", nil, &lists)
	if after, _ := s.keys.Writes(context.Background(), key); len(after) != len(writes) {
		t.Errorf("a read was recorded as a write")
	}
}

func TestAnotherHouseholdsListAndRecipeCannotBeTouched(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	theirs := s.recipe(s.joneses, "jo@example.com", "Jones stew")
	jo, _ := s.personalKey("jo@example.com")
	var list struct {
		ID int64 `json:"id"`
	}
	call(t, s.connect(jo), "create_shopping_list", map[string]any{"name": "Jones"}, &list)

	ann, annKey := s.personalKey("ann@example.com")
	cs := s.connect(ann)
	for name, args := range map[string]map[string]any{
		"get_shopping_list":           {"id": list.ID},
		"add_manual_shopping_item":    {"list_id": list.ID, "text": "x"},
		"check_shopping_item":         {"list_id": list.ID, "key": "t:x", "checked": true},
		"add_recipe_to_shopping_list": {"list_id": list.ID, "recipe_id": theirs},
	} {
		if msg := call(t, cs, name, args, nil); msg == "" {
			t.Errorf("%s on another household's list succeeded", name)
		}
	}
	if w, _ := s.keys.Writes(context.Background(), annKey); len(w) != 0 {
		t.Errorf("refused writes were recorded: %+v", w)
	}
}

func TestARecipeWriteIsADraftAndChangesNoRecipe(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	before := s.recipe(s.smiths, "ann@example.com", "Existing")
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)

	var out struct {
		ReviewPath string `json:"review_path"`
		Name       string `json:"name"`
	}
	if msg := call(t, cs, "import_recipe", map[string]any{"text": "Pie\n\nIngredients\n2 cups flour\n\nInstructions\n1. Bake."}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Name != "Drafted pie" || !strings.HasPrefix(out.ReviewPath, "/ingest/") {
		t.Fatalf("import = %+v", out)
	}
	token := strings.TrimPrefix(out.ReviewPath, "/ingest/")
	if _, ok := s.draft(s.smiths, "ann@example.com", token); !ok {
		t.Error("the draft is not waiting for the Smiths' review")
	}
	if _, ok := s.draft(s.joneses, "jo@example.com", token); ok {
		t.Error("the draft is visible to another household")
	}
	if msg := call(t, cs, "create_recipe_draft", map[string]any{"name": "Toast", "ingredients": []string{"1 slice bread"}, "steps": []string{"Toast it."}}, &out); msg != "" {
		t.Fatal(msg)
	}
	// Nothing was saved: the library still holds only the recipe that was there.
	all, err := s.rec.Search(s.as(s.smiths, "ann@example.com"), store.SearchQuery{})
	if err != nil || len(all) != 1 || all[0].ID != before {
		t.Errorf("recipes after two drafts = %+v, %v; want only the original", all, err)
	}
	if w, _ := s.keys.Writes(context.Background(), key); len(w) != 2 || w[0].Tool != "import_recipe" || w[1].Tool != "create_recipe_draft" {
		t.Errorf("recorded writes = %+v", w)
	}
	for name, args := range map[string]map[string]any{
		"import_recipe":       {},
		"create_recipe_draft": {"name": "x", "ingredients": []string{}, "steps": []string{"a"}},
	} {
		if msg := call(t, cs, name, args, nil); msg == "" {
			t.Errorf("%s with no input succeeded", name)
		}
	}
	if msg := call(t, cs, "import_recipe", map[string]any{"url": "file:///etc/passwd"}, nil); !strings.Contains(msg, "http") {
		t.Errorf("a non-http url: %q", msg)
	}
}

func TestPantryIsReadable(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	shop := shopping.New(s.d, s.rec, catalog.New(s.d))
	if err := shop.AddToPantry(s.as(s.smiths, "ann@example.com"), recipe.IngredientID(s.flour)); err != nil {
		t.Fatal(err)
	}
	secret, _ := s.personalKey("ann@example.com")
	var out struct {
		Ingredients []string `json:"ingredients"`
	}
	if msg := call(t, s.connect(secret), "get_pantry", nil, &out); msg != "" || len(out.Ingredients) != 1 || out.Ingredients[0] != "zz mcp flour" {
		t.Errorf("pantry = %+v %q", out, msg)
	}
}

func rawPost(t *testing.T, url, secret string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	_, _ = io.Copy(io.Discard, res.Body)
	return res
}

func TestABurstPastTheLimitGets429WithRetryAfter(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 1, Burst: 3})
	a, _ := s.personalKey("ann@example.com")
	j, _ := s.personalKey("jo@example.com")
	for i := 0; i < 3; i++ {
		if res := rawPost(t, s.srv.URL, a); res.StatusCode == http.StatusTooManyRequests {
			t.Fatalf("request %d of a burst of 3 was limited", i+1)
		}
	}
	res := rawPost(t, s.srv.URL, a)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") != "1" {
		t.Errorf("4th request = %d, Retry-After %q; want 429 and 1", res.StatusCode, res.Header.Get("Retry-After"))
	}
	// Each credential has its own bucket.
	if res := rawPost(t, s.srv.URL, j); res.StatusCode == http.StatusTooManyRequests {
		t.Error("another credential was limited by the first one's burst")
	}
	// The bucket drains at the rate.
	s.now = s.now.Add(2 * time.Second)
	if res := rawPost(t, s.srv.URL, a); res.StatusCode == http.StatusTooManyRequests {
		t.Error("still limited after the bucket drained")
	}
}

// wantChallenge is the 401 header of every refusal: Bearer, pointing at the
// Protected Resource Metadata (RFC 9728).
const wantChallenge = `Bearer resource_metadata="https://gobbler.example/.well-known/oauth-protected-resource/mcp"`

func TestEveryRefusedCredentialIs401(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	ctx := context.Background()
	revoked, _ := s.keys.CreatePersonal(ctx, "ann@example.com", "gone", 0)
	ks, _ := s.keys.ListPersonal(ctx, "ann@example.com")
	if err := s.keys.RevokePersonal(ctx, "ann@example.com", ks[0].ID); err != nil {
		t.Fatal(err)
	}
	expired, _ := s.keys.CreatePersonal(ctx, "ann@example.com", "short", time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	for name, secret := range map[string]string{"none": "", "unknown": "gob_x", "revoked": revoked, "expired": expired} {
		res := rawPost(t, s.srv.URL, secret)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != wantChallenge {
			t.Errorf("%s: %d %q, want 401 %s", name, res.StatusCode, res.Header.Get("WWW-Authenticate"), wantChallenge)
		}
	}
	// A valid key whose owner is in no household is refused, not served.
	orphan, _ := s.keys.CreatePersonal(ctx, "nobody@example.com", "k", 0)
	if res := rawPost(t, s.srv.URL, orphan); res.StatusCode != http.StatusForbidden {
		t.Errorf("owner with no household: %d, want 403", res.StatusCode)
	}
}

func TestRoutesRefuseIncompleteDeps(t *testing.T) {
	if _, err := mcp.Routes(mcp.Deps{}); err == nil {
		t.Error("Routes accepted empty deps")
	}
}

// create_recipe_draft reads an agent's recipe with the same rules as pasted
// text (ADR-00006): the draft holds the lines and steps it was given.
func TestCreateRecipeDraftGoesThroughTheRuleBasedPipeline(t *testing.T) {
	s := newSiteWith(t, config.MCP{Rate: 100, Burst: 100}, func(cat *catalog.Store) mcp.Drafter {
		p, err := ingest.New(config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t"}, ingest.Deps{Catalog: cat})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	secret, _ := s.personalKey("ann@example.com")
	var out struct {
		ReviewPath string `json:"review_path"`
	}
	msg := call(t, s.connect(secret), "create_recipe_draft", map[string]any{
		"name": "Flour toast", "ingredients": []string{"2 cups zz mcp flour", "1 pinch mystery spice"}, "steps": []string{"Mix well.", "Bake for 10 minutes."},
	}, &out)
	if msg != "" {
		t.Fatal(msg)
	}
	d, ok := s.draft(s.smiths, "ann@example.com", strings.TrimPrefix(out.ReviewPath, "/ingest/"))
	if !ok {
		t.Fatal("no draft is waiting")
	}
	var lines, steps int
	for _, c := range d.Components {
		lines, steps = lines+len(c.Lines), steps+len(c.Steps)
	}
	if d.Name.Value != "Flour toast" || lines != 2 || steps != 2 {
		t.Errorf("draft = %q with %d lines and %d steps, want Flour toast, 2 and 2", d.Name.Value, lines, steps)
	}
}

// One item the agent supplies is one line of the composed text: a newline
// inside it cannot split it, and an item that reads like a section heading is
// still an ingredient or a step.
func TestAnAgentsItemsStayOneLineEach(t *testing.T) {
	s := newSiteWith(t, config.MCP{Rate: 100, Burst: 100}, func(cat *catalog.Store) mcp.Drafter {
		p, err := ingest.New(config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t"}, ingest.Deps{Catalog: cat})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
	secret, _ := s.personalKey("ann@example.com")
	var out struct {
		ReviewPath string `json:"review_path"`
	}
	msg := call(t, s.connect(secret), "create_recipe_draft", map[string]any{
		"name":        "Odd\nrecipe",
		"ingredients": []string{"2 cups\nzz mcp flour", "Instructions", "1 pinch salt\n\nInstructions\n1. Fake step"},
		"steps":       []string{"Mix\nwell.", "Ingredients"},
	}, &out)
	if msg != "" {
		t.Fatal(msg)
	}
	d, ok := s.draft(s.smiths, "ann@example.com", strings.TrimPrefix(out.ReviewPath, "/ingest/"))
	if !ok {
		t.Fatal("no draft is waiting")
	}
	var lines, steps int
	for _, c := range d.Components {
		lines, steps = lines+len(c.Lines), steps+len(c.Steps)
	}
	if lines != 3 || steps != 2 || d.Name.Value != "Odd recipe" {
		t.Errorf("draft %q has %d lines and %d steps, want Odd recipe with 3 and 2", d.Name.Value, lines, steps)
	}
}

// token makes a grant for email and returns its access token, bound to the
// resource. The grant is approved and exchanged as the OAuth endpoints do.
func (s *site) token(email, resource string) (string, oauth.Principal) {
	s.t.Helper()
	ctx := context.Background()
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	code, err := s.oauth.IssueCode(ctx, oauth.AuthRequest{
		Owner: email, ClientID: "gobc_test", ClientName: "Test client", RedirectURI: "https://app.example/cb",
		CodeChallenge: oauth.Challenge(verifier), Resource: resource,
	})
	if err != nil {
		s.t.Fatal(err)
	}
	toks, err := s.oauth.ExchangeCode(ctx, oauth.ExchangeRequest{Code: code, ClientID: "gobc_test", RedirectURI: "https://app.example/cb", Verifier: verifier, Resource: resource})
	if err != nil {
		s.t.Fatal(err)
	}
	p, err := s.oauth.Verify(ctx, toks.Access, resource)
	if err != nil {
		s.t.Fatal(err)
	}
	return toks.Access, p
}

const mcpResource = "https://gobbler.example/mcp"

func TestOAuthTokenActsAsItsOwnerAndWritesRecordTheGrant(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	tok, grant := s.token("ann@example.com", mcpResource)
	cs := s.connect(tok)

	var list struct {
		ID int64 `json:"id"`
	}
	if msg := call(t, cs, "create_shopping_list", map[string]any{"name": "Via OAuth"}, &list); msg != "" {
		t.Fatal(msg)
	}
	var lists pageOf[struct {
		Name string `json:"name"`
	}]
	call(t, cs, "list_shopping_lists", nil, &lists)
	found := false
	for _, l := range lists.Items {
		found = found || l.Name == "Via OAuth"
	}
	if !found {
		t.Errorf("the token's owner does not see the list it made: %+v", lists.Items)
	}
	writes, err := s.oauth.Writes(context.Background(), grant)
	if err != nil || len(writes) != 1 || writes[0].Tool != "create_shopping_list" || writes[0].HouseholdID != s.smiths.ID || writes[0].GrantID != grant.GrantID {
		t.Errorf("grant writes = %+v, %v; want one create_shopping_list in the Smiths household", writes, err)
	}
	// An API key keeps working beside tokens.
	secret, _ := s.personalKey("ann@example.com")
	call(t, s.connect(secret), "list_shopping_lists", nil, &lists)
}

func TestOAuthTokenRefusals(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	ctx := context.Background()

	otherAudience, _ := s.token("ann@example.com", "https://elsewhere.example/mcp")
	expired, eg := s.token("ann@example.com", mcpResource)
	if _, err := s.d.ExecContext(ctx, `UPDATE oauth_tokens SET expires_at = '2000-01-01T00:00:00.000000000Z' WHERE grant_id = ?`, eg.GrantID); err != nil {
		t.Fatal(err)
	}
	revoked, rg := s.token("ann@example.com", mcpResource)
	if err := s.oauth.RevokeGrant(ctx, "ann@example.com", rg.GrantID); err != nil {
		t.Fatal(err)
	}

	for name, secret := range map[string]string{
		"another audience": otherAudience, "expired": expired, "revoked": revoked,
		"unknown": oauth.AccessTokenPrefix + "not-a-real-token", "none": "",
	} {
		res := rawPost(t, s.srv.URL, secret)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") != wantChallenge {
			t.Errorf("%s: %d %q, want 401 %s", name, res.StatusCode, res.Header.Get("WWW-Authenticate"), wantChallenge)
		}
	}
}

func TestOAuthTokenWithNoKnownAudienceIsRefused(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	tok, _ := s.token("ann@example.com", mcpResource)
	v := mcp.OAuthVerifier{Tokens: s.oauth, Next: mcp.KeyVerifier{Keys: s.keys}}
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, mcp.ErrInvalidCredential) {
		t.Errorf("a token with no site on the context: %v, want ErrInvalidCredential", err)
	}
	// Even a store that accepts any audience is not asked without an origin.
	lax := mcp.OAuthVerifier{Tokens: acceptsAnyAudience{s.oauth}, Next: mcp.KeyVerifier{Keys: s.keys}}
	if _, err := lax.Verify(context.Background(), tok); !errors.Is(err, mcp.ErrInvalidCredential) {
		t.Errorf("a token with no origin, lax store: %v, want ErrInvalidCredential", err)
	}
	if _, err := v.Verify(mcp.WithSite(context.Background(), testSite), tok); err != nil {
		t.Errorf("the same token with the site: %v", err)
	}
}

func TestOAuthTokenIsConfinedToItsOwnersHousehold(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	theirs := s.recipe(s.joneses, "jo@example.com", "Jones stew")
	tok, _ := s.token("ann@example.com", mcpResource)
	if msg := call(t, s.connect(tok), "get_recipe", map[string]any{"id": theirs}, nil); msg != "not found" {
		t.Errorf("ann's token read the Joneses' recipe: %q", msg)
	}
	jo, _ := s.token("jo@example.com", mcpResource)
	if msg := call(t, s.connect(jo), "get_recipe", map[string]any{"id": theirs}, nil); msg != "" {
		t.Errorf("jo's token cannot read jo's recipe: %q", msg)
	}
}

func TestOAuthTokenOfAnOutsiderIsForbidden(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	tok, _ := s.token("nobody@example.com", mcpResource)
	if res := rawPost(t, s.srv.URL, tok); res.StatusCode != http.StatusForbidden {
		t.Errorf("a token whose owner is in no household: %d, want 403", res.StatusCode)
	}
}

func TestEachGrantHasItsOwnRateLimit(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 1, Burst: 2})
	// Two grants of one person: the limit is per grant, not per owner.
	a, _ := s.token("ann@example.com", mcpResource)
	b, _ := s.token("ann@example.com", mcpResource)
	limited := false
	for range 5 {
		limited = limited || rawPost(t, s.srv.URL, a).StatusCode == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("a token was never limited")
	}
	if rawPost(t, s.srv.URL, b).StatusCode == http.StatusTooManyRequests {
		t.Error("another grant was limited by the first one's burst")
	}
}

// acceptsAnyAudience is a token store that checks the token against the
// audience it was issued for, whatever the caller asks.
type acceptsAnyAudience struct{ *oauth.Store }

func (a acceptsAnyAudience) Verify(ctx context.Context, token, _ string) (oauth.Principal, error) {
	return a.Store.Verify(ctx, token, mcpResource)
}

// The audit logs a write with the credential as it was verified: revoked
// between the verifier and the audit, a key and a grant are still logged, each
// in its own table and under its own snapshot.
func TestWritesOfCredentialsRevokedMidRequestAreStillLogged(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	ctx := mcp.WithSite(context.Background(), testSite)
	verifier := mcp.OAuthVerifier{Tokens: s.oauth, Next: mcp.KeyVerifier{Keys: s.keys}}
	audit := mcp.AuditByKind{mcp.KindAPIKey: mcp.KeyAudit{Keys: s.keys}, mcp.KindOAuth: mcp.GrantAudit{Grants: s.oauth}}

	secret, key := s.personalKey("ann@example.com")
	token, grant := s.token("ann@example.com", mcpResource)
	ck, err := verifier.Verify(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := verifier.Verify(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.keys.RevokePersonal(ctx, "ann@example.com", key.KeyID); err != nil {
		t.Fatal(err)
	}
	if err := s.oauth.RevokeGrant(ctx, "ann@example.com", grant.GrantID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []mcp.Credential{ck, ct} {
		if err := audit.RecordWrite(ctx, c, s.smiths.ID, "check_shopping_item", "list 1"); err != nil {
			t.Fatalf("%s: %v", c.Subject(), err)
		}
	}
	if w, err := s.keys.Writes(ctx, key); err != nil || len(w) != 1 {
		t.Errorf("key writes = %+v, %v", w, err)
	}
	if w, err := s.oauth.Writes(ctx, grant); err != nil || len(w) != 1 {
		t.Errorf("grant writes = %+v, %v", w, err)
	}
}

// ADR-00035 obligation 14, MCP half: a household holds at most 20 pending
// drafts, the 21st import is refused with a message saying drafts are waiting
// for review, nothing is dropped, and another household is unaffected.
func TestTheTwentyFirstDraftIsRefusedOverMCP(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 1000, Burst: 1000})
	secret, key := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	var tokens []string
	for i := 0; i < store.MaxPendingDrafts; i++ {
		var out struct {
			ReviewPath string `json:"review_path"`
		}
		if msg := call(t, cs, "import_recipe", map[string]any{"text": fmt.Sprintf("Pie %d\n\nIngredients\n2 cups flour", i)}, &out); msg != "" {
			t.Fatalf("draft %d: %s", i+1, msg)
		}
		tokens = append(tokens, strings.TrimPrefix(out.ReviewPath, "/ingest/"))
	}
	before := fetches.Load()
	msg := call(t, cs, "import_recipe", map[string]any{"text": "One more pie"}, nil)
	if !strings.Contains(msg, "waiting for review") {
		t.Fatalf("the 21st draft = %q, want a refusal saying drafts are waiting for review", msg)
	}
	if msg := call(t, cs, "create_recipe_draft", map[string]any{"name": "Toast", "ingredients": []string{"1 slice bread"}, "steps": []string{"Toast it."}}, nil); !strings.Contains(msg, "waiting for review") {
		t.Errorf("create_recipe_draft past the limit = %q, want the same refusal", msg)
	}
	if msg := call(t, cs, "import_recipe", map[string]any{"url": "https://example.test/pie"}, nil); !strings.Contains(msg, "waiting for review") {
		t.Errorf("import_recipe of a URL past the limit = %q, want the same refusal", msg)
	}
	if got := fetches.Load(); got != before {
		t.Errorf("%d fetches or readings ran for drafts that were going to be refused", got-before)
	}
	for i, tok := range tokens {
		if _, ok := s.draft(s.smiths, "ann@example.com", tok); !ok {
			t.Errorf("draft %d was dropped to make room", i+1)
		}
	}
	other, _ := s.personalKey("jo@example.com")
	if msg := call(t, s.connect(other), "import_recipe", map[string]any{"text": "Their pie"}, nil); msg != "" {
		t.Errorf("another household's first draft: %s", msg)
	}
	if w, _ := s.keys.Writes(context.Background(), key); len(w) != store.MaxPendingDrafts {
		t.Errorf("%d writes recorded, want %d: a refused draft is not a write", len(w), store.MaxPendingDrafts)
	}
}

// A draft records who made it: the credential as snapshotted when it was
// verified, the person it acts as and the tool.
func TestADraftRecordsItsProvenance(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, key := s.personalKey("ann@example.com")
	var out struct {
		ReviewPath string `json:"review_path"`
	}
	if msg := call(t, s.connect(secret), "import_recipe", map[string]any{"text": "Pie\n\nIngredients\n2 cups flour"}, &out); msg != "" {
		t.Fatal(msg)
	}
	p, err := s.rec.GetProposal(s.as(s.smiths, "ann@example.com"), strings.TrimPrefix(out.ReviewPath, "/ingest/"))
	if err != nil {
		t.Fatal(err)
	}
	c := p.Credential
	if p.Person != "ann@example.com" || p.Tool != "import_recipe" || c == nil {
		t.Fatalf("provenance = %+v, want ann, import_recipe and a credential", p.Provenance)
	}
	if c.Kind != mcp.KindAPIKey || c.ID != key.KeyID || c.Ref != key.Prefix || c.Owner != "ann@example.com" || !c.CreatedAt.Equal(key.CreatedAt) {
		t.Errorf("credential = %+v, want the key %d (%s) as it was when verified", c, key.KeyID, key.Prefix)
	}
}

// A household key has no person, only its household (ADR-00031). It reads and
// writes that household's shopping lists, and the write is recorded against it.
func TestAHouseholdKeyUsesItsHouseholdsShoppingLists(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, err := s.keys.CreateHousehold(context.Background(), s.joneses.ID, "jo@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	cs := s.connect(secret)
	var list struct {
		ID int64 `json:"id"`
	}
	if msg := call(t, cs, "create_shopping_list", map[string]any{"name": "Joneses week"}, &list); msg != "" {
		t.Fatalf("create_shopping_list with a household key: %q", msg)
	}
	if msg := call(t, cs, "add_manual_shopping_item", map[string]any{"list_id": list.ID, "text": "paper towels"}, nil); msg != "" {
		t.Fatalf("add_manual_shopping_item with a household key: %q", msg)
	}
	var got struct {
		Name  string     `json:"name"`
		Items []struct{} `json:"items"`
	}
	if msg := call(t, cs, "get_shopping_list", map[string]any{"id": list.ID}, &got); msg != "" || got.Name != "Joneses week" || len(got.Items) != 1 {
		t.Errorf("get_shopping_list = %+v %q", got, msg)
	}
	var pantry struct {
		Ingredients []string `json:"ingredients"`
	}
	if msg := call(t, cs, "get_pantry", nil, &pantry); msg != "" {
		t.Errorf("get_pantry with a household key: %q", msg)
	}
	// The Smiths cannot see it.
	secretA, _ := s.personalKey("ann@example.com")
	var lists pageOf[struct {
		ID int64 `json:"id"`
	}]
	if msg := call(t, s.connect(secretA), "list_shopping_lists", nil, &lists); msg != "" || len(lists.Items) != 0 {
		t.Errorf("the Smiths see %+v %q", lists, msg)
	}
}
