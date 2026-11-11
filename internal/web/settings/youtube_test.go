package settings

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	store "github.com/kaecyra/gobbler/internal/settings"
	"github.com/kaecyra/gobbler/internal/video"
)

func youtubeHandler(t *testing.T) (h http.Handler, s *store.Store) {
	t.Helper()
	_, s, _ = integrationsFixture(t, bytes.Repeat([]byte{1}, store.KeyBytes))
	return Routes(Deps{Backup: &fakeBackup{}, Integrations: Integrations{Store: s, Sections: []Section{YouTubeSection()}}}), s
}

// ADR-00054: the YouTube section is the Data API key and nothing else; it is
// saved, replaced and cleared like any secret, and never shown again.
func TestYouTubeSectionKeepsTheKeyAsASecret(t *testing.T) {
	h, s := youtubeHandler(t)
	body := page(h)
	for _, want := range []string{`data-testid="integration-youtube"`, `data-testid="secret-youtube-api_key"`, "Data API key", "Not set"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, `data-testid="values-youtube"`) {
		t.Error("the section has a plain-values form; it holds only the key")
	}
	if rec := post(h, "/secrets/youtube/api_key", true, "AIza-sekrit"); rec.Code != 200 {
		t.Fatalf("save = %d", rec.Code)
	}
	got, err := s.Secret(context.Background(), store.Field{Integration: video.Integration, Name: video.FieldAPIKey})
	if err != nil || got != "AIza-sekrit" {
		t.Fatalf("stored = %q, %v", got, err)
	}
	if body := page(h); strings.Contains(body, "AIza-sekrit") || !strings.Contains(body, "Set") {
		t.Error("the saved key reached the page, or the page does not say it is set")
	}
	post(h, "/secrets/youtube/api_key/clear", false, "")
	if _, err := s.Secret(context.Background(), store.Field{Integration: video.Integration, Name: video.FieldAPIKey}); err == nil {
		t.Error("the key is still stored after clearing")
	}
}
