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
// saved by the section's form, cleared like any secret, and never shown again.
func TestYouTubeSectionKeepsTheKeyAsASecret(t *testing.T) {
	h, s := youtubeHandler(t)
	body := page(h)
	for _, want := range []string{`data-testid="integration-youtube"`, `data-testid="secret-youtube-api_key"`, "Data API key", "Not set"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Count(body, `<input`) != 1 {
		t.Error("the section has inputs besides the key")
	}
	if rec := postForm(h, "/values/youtube", true, video.FieldAPIKey, "AIza-sekrit"); rec.Code != 200 {
		t.Fatalf("save = %d", rec.Code)
	}
	got, err := s.Secret(context.Background(), store.Field{Integration: video.Integration, Name: video.FieldAPIKey})
	if err != nil || got != "AIza-sekrit" {
		t.Fatalf("stored = %q, %v", got, err)
	}
	if body := page(h); strings.Contains(body, "AIza-sekrit") || !strings.Contains(body, " Saved</span>") {
		t.Error("the saved key reached the page, or the page does not say it is set")
	}
	postForm(h, "/secrets/youtube/api_key/clear", false)
	if _, err := s.Secret(context.Background(), store.Field{Integration: video.Integration, Name: video.FieldAPIKey}); err == nil {
		t.Error("the key is still stored after clearing")
	}
}
