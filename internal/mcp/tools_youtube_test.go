package mcp_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
)

// youTubeVideo is the video the agent links; youTubeLink is its watch page,
// which no test fetches.
const (
	youTubeVideo = "mcpVideo123"
	youTubeLink  = "https://www.youtube.com/watch?v=" + youTubeVideo
)

// fakeDescriptions is the YouTube Data API boundary: it answers every video
// with one description and records the IDs it was asked for.
type fakeDescriptions struct {
	desc  video.Description
	mu    sync.Mutex
	asked []string
}

func (f *fakeDescriptions) Fetch(_ context.Context, id string) (video.Description, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	return f.desc, nil
}

func (f *fakeDescriptions) askedFor() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// describedSite is the endpoint drafting through the real ingestion service
// with descriptions as its YouTube lookup. The description has no links, so a
// draft from it fetches no page.
func describedSite(t *testing.T) (*site, *fakeDescriptions) {
	t.Helper()
	desc := &fakeDescriptions{desc: video.Description{
		Title: "Video Toast",
		Text:  "Ingredients\n- 2 cups zz mcp flour\n\nInstructions\n1. Mix the flour.\n2. Bake.\n",
	}}
	return newSiteDescribing(t, config.MCP{Rate: 1000, Burst: 1000}, nil, true, desc), desc
}

// ADR-00054 decision 10: an agent's YouTube link is drafted from the video's
// description, as the web import's is, and the video stays on the draft.
func TestImportOfAYouTubeLinkReadsTheDescription(t *testing.T) {
	s, desc := describedSite(t)
	secret, _ := s.personalKey("ann@example.com")
	d, _ := s.importAndHold(t, secret, youTubeLink)

	if got := desc.askedFor(); !slices.Equal(got, []string{youTubeVideo}) {
		t.Errorf("lookup asked for %v, want the video once", got)
	}
	if d.Source.Kind != store.SnapshotText || d.Source.URL != youTubeLink {
		t.Errorf("source = %s %q, want the description as text under the YouTube link", d.Source.Kind, d.Source.URL)
	}
	if d.Name.Value != "Video Toast" || len(d.Components) == 0 {
		t.Errorf("draft name %q, %d components; want the description's recipe", d.Name.Value, len(d.Components))
	}
	if d.VideoID != youTubeVideo {
		t.Errorf("VideoID = %q, want the linked video kept", d.VideoID)
	}
}

// preview_import drafts a YouTube link through the same lookup, so the preview
// is the draft an import would hold.
func TestPreviewOfAYouTubeLinkReadsTheDescription(t *testing.T) {
	s, desc := describedSite(t)
	secret, _ := s.personalKey("ann@example.com")
	var got map[string]any
	if msg := call(t, s.connect(secret), "preview_import", map[string]any{"url": youTubeLink}, &got); msg != "" {
		t.Fatal(msg)
	}
	if asked := desc.askedFor(); !slices.Equal(asked, []string{youTubeVideo}) {
		t.Errorf("lookup asked for %v, want the video once", asked)
	}
	if name, _ := got["name"].(map[string]any); name["value"] != "Video Toast" {
		t.Errorf("preview name = %v, want the description's recipe", got["name"])
	}
}
