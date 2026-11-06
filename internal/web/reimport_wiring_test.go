package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// ADR-00042 obligation 7, through the real wiring: an agent's propose_recipe_edit
// over MCP supersedes a pending re-import, and the re-import's picture candidates
// (rows and objects) go with it. The store behind the MCP endpoint is the one
// mountMCP builds, so this fails if that store is not given the remover.
func TestAnMCPEditProposalSupersedingAReimportLeavesNoCandidates(t *testing.T) {
	cfg := devConfig()
	cfg.Ingestion = config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "test"}
	cfg.MCP = config.MCP{Rate: 100, Burst: 100}
	d := testDB(t)
	m := media.NewLocal(t.TempDir())
	r := testRouter(t, cfg, Deps{DB: d, Media: m})
	do(r, http.MethodGet, "/healthz", auth.DevEmail)
	if rec := do(r, http.MethodGet, "/", auth.DevEmail); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d", rec.Code)
	}

	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, auth.DevEmail)
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	h, err := household.New(d).ForEmail(ctx, auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	ctx = household.WithContext(ctx, h)

	flour, err := catalog.New(d).CreateIngredient(ctx, catalog.Ingredient{Name: "wiring flour", Aisle: "baking"})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d)
	id, err := st.Create(ctx, recipe.Recipe{Name: "Toast", SourceURL: "https://example.test/toast", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Ingredient: recipe.IngredientID(flour.ID), Rule: quantity.RuleLinear, Raw: "flour", Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(1))}}},
		Steps: []recipe.Step{{ID: "a", Text: "Toast."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.ProposeDraft(ctx, store.DraftInput{
		Document:   []byte(fmt.Sprintf(`{"reimport":{"recipe":%d,"base_version":1},"components":[]}`, id)),
		Source:     store.SnapshotInput{Kind: store.SnapshotHTML, Content: []byte("<html>again</html>")},
		Provenance: store.Provenance{Person: auth.DevEmail},
	})
	if err != nil {
		t.Fatal(err)
	}
	const key = "drafts/77/1.webp"
	if err := m.Put(ctx, key, []byte("candidate"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO draft_candidates (household_id, proposal_id, position, source_url, key, width, height)
		SELECT household_id, id, 1, 'https://img.test/a.jpg', ?, 1066, 300 FROM proposals WHERE token = ?`, key, token); err != nil {
		t.Fatal(err)
	}

	secret, err := apikey.New(d).CreatePersonal(context.Background(), auth.DevEmail, "agent", 0)
	if err != nil {
		t.Fatal(err)
	}
	call := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"propose_recipe_edit","arguments":{"id":%d,"version":1,"reason":"fewer words","recipe":{"name":"Agent toast","components":[{"ingredients":[{"text":"1 cup flour","ingredient_id":%d}],"steps":[{"text":"Toast it."}]}]}}}}`, id, flour.ID)
	mreq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(call))
	mreq.Header.Set("Content-Type", "application/json")
	mreq.Header.Set("Accept", "application/json, text/event-stream")
	mreq.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, mreq)
	var out struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Token string `json:"token"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Result.IsError || out.Result.StructuredContent.Token == "" {
		t.Fatalf("propose_recipe_edit = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}

	if p, err := st.GetProposal(ctx, token); err != nil || p.State != store.ProposalSuperseded {
		t.Fatalf("re-import = %+v (%v), want superseded by the agent's edit", p.State, err)
	}
	var rows int
	if err := d.QueryRow(`SELECT COUNT(*) FROM draft_candidates`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("%d candidate rows left (%v), want none", rows, err)
	}
	if _, err := m.Get(ctx, key); err == nil {
		t.Errorf("candidate object %s is still stored", key)
	}
}
