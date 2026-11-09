package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
)

// get_house_style (ADR-00048): an agent reads the household's house style and
// no tool writes it.

type houseStyle struct {
	Style string `json:"style"`
}

func (s *site) setHouseStyle(h household.Household, style string) {
	s.t.Helper()
	if err := household.New(s.d).SetHouseStyle(context.Background(), h.ID, style); err != nil {
		s.t.Fatal(err)
	}
}

func (s *site) houseStyle(h household.Household) string {
	s.t.Helper()
	got, err := household.New(s.d).HouseStyle(context.Background(), h.ID)
	if err != nil {
		s.t.Fatal(err)
	}
	return got
}

func TestGetHouseStyleReturnsTheCredentialsHouseholdsStyle(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	const style = "Weights in grams.\nOne step per action."
	s.setHouseStyle(s.smiths, style)

	secret, _ := s.personalKey("ann@example.com")
	var got houseStyle
	if msg := call(t, s.connect(secret), "get_house_style", nil, &got); msg != "" || got.Style != style {
		t.Fatalf("get_house_style = %+v %q, want %q", got, msg, style)
	}
	// Another household's credential reads its own, empty, style.
	other, _ := s.personalKey("jo@example.com")
	got = houseStyle{Style: "unset"}
	if msg := call(t, s.connect(other), "get_house_style", nil, &got); msg != "" || got.Style != "" {
		t.Fatalf("get_house_style for the Joneses = %+v %q, want empty", got, msg)
	}
}

func TestAHouseholdKeyReadsTheHouseStyle(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	s.setHouseStyle(s.joneses, "Metric only.")
	secret, err := s.keys.CreateHousehold(context.Background(), s.joneses.ID, "jo@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got houseStyle
	if msg := call(t, s.connect(secret), "get_house_style", nil, &got); msg != "" || got.Style != "Metric only." {
		t.Fatalf("get_house_style with a household key = %+v %q", got, msg)
	}
}

// No tool writes the house style: get_house_style is the only tool that names
// it, it is marked read-only, and no tool takes a style as input. Calling it
// leaves the style as it was.
func TestNoMCPToolChangesTheHouseStyle(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	s.setHouseStyle(s.smiths, "Keep this.")
	secret, _ := s.personalKey("ann@example.com")
	cs := s.connect(secret)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range res.Tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		for name := range schema.Properties {
			if strings.Contains(strings.ToLower(name), "style") {
				t.Errorf("tool %s takes %q as input", tool.Name, name)
			}
		}
		if !strings.Contains(tool.Name, "style") {
			continue
		}
		if tool.Name != "get_house_style" {
			t.Errorf("tool %s names the house style; only get_house_style may", tool.Name)
			continue
		}
		found = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("get_house_style annotations = %+v, want read-only", tool.Annotations)
		}
	}
	if !found {
		t.Fatal("get_house_style is not listed")
	}
	// An unknown argument may be ignored or refused; either way nothing is
	// written, so the result is not checked.
	call(t, cs, "get_house_style", map[string]any{"style": "Overwritten."}, nil)
	if got := s.houseStyle(s.smiths); got != "Keep this." {
		t.Fatalf("house style after the MCP calls = %q, want it unchanged", got)
	}
}

func TestTheServerTellsAnAgentToReadTheHouseStyle(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	secret, _ := s.personalKey("ann@example.com")
	init := s.connect(secret).InitializeResult()
	if init == nil || !strings.Contains(init.Instructions, "get_house_style") {
		t.Fatalf("server instructions do not name get_house_style: %+v", init)
	}
}
