package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
)

// The import tools draft through the ingestion service NewDeps builds, which
// admits the credential's Caller before the pipeline runs (ADR-00043). Each
// kind of credential is admitted and its draft lands in its own household
// (ADR-00031): a household key, a personal key and an OAuth grant.
func TestImportThroughTheServiceServesEveryKindOfCredential(t *testing.T) {
	s := newSiteDrafting(t, config.MCP{Rate: 100, Burst: 100}, nil, true)
	ctx := context.Background()
	houseKey, err := s.keys.CreateHousehold(ctx, s.joneses.ID, "jo@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	personal, _ := s.personalKey("ann@example.com")
	grant, _ := s.token("ann@example.com", mcpResource)

	for _, tc := range []struct {
		name   string
		secret string
		house  household.Household
		member string
	}{
		{"household key", houseKey, s.joneses, "jo@example.com"},
		{"personal key", personal, s.smiths, "ann@example.com"},
		{"oauth grant", grant, s.smiths, "ann@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out struct {
				ReviewPath string `json:"review_path"`
			}
			text := "Toast for " + tc.name + "\n\nIngredients\n- 2 cups zz mcp flour\n\nInstructions\n1. Bake.\n"
			if msg := call(t, s.connect(tc.secret), "import_recipe", map[string]any{"text": text}, &out); msg != "" {
				t.Fatal(msg)
			}
			d, ok := s.draft(tc.house, tc.member, strings.TrimPrefix(out.ReviewPath, "/ingest/"))
			if !ok {
				t.Fatal("no draft is waiting in the credential's household")
			}
			// The catalog lookups ran in the credential's household: the Smiths'
			// own flour matched for them, and the Joneses cannot see it.
			matched := len(d.Components) > 0 && len(d.Components[0].Lines) > 0 && d.Components[0].Lines[0].Ingredient != 0
			if want := tc.house.ID == s.smiths.ID; matched != want {
				t.Errorf("ingredient matched = %v, want %v", matched, want)
			}
		})
	}
}
