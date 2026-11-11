package settings

import "github.com/kaecyra/gobbler/internal/video"

// YouTubeSection is the YouTube integration's part of the page (ADR-00054): the
// Data API key as a secret and nothing else. With no key saved, a YouTube link
// is ingested as any page. Wiring registers it on Integrations.Sections.
func YouTubeSection() Section {
	return Section{
		Integration: video.Integration,
		Title:       "YouTube",
		Secrets:     []SecretField{{Name: video.FieldAPIKey, Label: "Data API key"}},
	}
}
