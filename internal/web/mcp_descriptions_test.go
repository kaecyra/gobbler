package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/video"
)

// oneDescription is the YouTube Data API boundary: every video has the same
// description, and the IDs asked for are recorded.
type oneDescription struct{ asked []string }

func (o *oneDescription) Fetch(_ context.Context, id string) (video.Description, error) {
	o.asked = append(o.asked, id)
	return video.Description{Title: "Router Toast", Text: "Ingredients\n- 1 cup flour\n\nInstructions\n1. Bake.\n"}, nil
}

// ADR-00054 decision 10, through the real wiring: the router hands the MCP
// endpoint the same description lookup as the web import, so an agent's
// preview of a YouTube link is read from the video's description.
func TestTheMCPEndpointGetsTheDescriptionLookup(t *testing.T) {
	cfg := devConfig()
	cfg.Ingestion = config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "test"}
	cfg.MCP = config.MCP{Rate: 100, Burst: 100}
	d := testDB(t)
	desc := &oneDescription{}
	r := testRouter(t, cfg, Deps{DB: d, Descriptions: desc})
	if rec := do(r, http.MethodGet, "/", auth.DevEmail); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d", rec.Code)
	}
	secret, err := apikey.New(d).CreatePersonal(context.Background(), auth.DevEmail, "agent", 0)
	if err != nil {
		t.Fatal(err)
	}

	const call = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"preview_import","arguments":{"url":"https://www.youtube.com/watch?v=routerVid01"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(call))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var out struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Name struct {
					Value string `json:"value"`
				} `json:"name"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Result.IsError {
		t.Fatalf("preview_import = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	if len(desc.asked) != 1 || desc.asked[0] != "routerVid01" {
		t.Errorf("lookup asked for %v, want the video once", desc.asked)
	}
	if got := out.Result.StructuredContent.Name.Value; got != "Router Toast" {
		t.Errorf("preview name = %q, want the description's recipe", got)
	}
}
