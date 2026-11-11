package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm/router"
	"github.com/kaecyra/gobbler/internal/settings"
	"github.com/kaecyra/gobbler/internal/video"
)

// secretReader is what the YouTube key reader needs from the settings store;
// *settings.Store satisfies it.
type secretReader interface {
	Secret(ctx context.Context, f settings.Field) (string, error)
}

// youtubeKey reads the YouTube Data API key from the integration settings on
// every call, so a change on the Deployment page is in force on the next one
// (ADR-00054, ADR-00057).
type youtubeKey struct {
	store secretReader
	log   *slog.Logger
}

// APIKey returns the saved key. A key that is not saved is video.ErrNoKey, the
// normal off state. One that is saved and cannot be opened is also off, but
// logged: the Deployment page marks it unreadable and an operator should be
// able to find out why descriptions are not read.
func (k youtubeKey) APIKey(ctx context.Context) (string, error) {
	key, err := k.store.Secret(ctx, settings.Field{Integration: video.Integration, Name: video.FieldAPIKey})
	switch {
	case err == nil:
		return key, nil
	case errors.Is(err, settings.ErrUnset):
		return "", video.ErrNoKey
	case errors.Is(err, settings.ErrUnreadable):
		k.log.ErrorContext(ctx, "the saved YouTube Data API key cannot be opened; video descriptions are not read until it is entered again", "error", err.Error())
		return "", fmt.Errorf("%w: it cannot be opened", video.ErrNoKey)
	}
	return "", fmt.Errorf("read the YouTube Data API key: %w", err)
}

// llmWithDescriptions is the LLM router that also carries the YouTube
// description lookup. The ingest wiring passes the provider down to the
// pipeline, which takes the lookup from it (ingest.DescriptionAware); it embeds
// the concrete router so the provider's optional interfaces stay visible.
type llmWithDescriptions struct {
	*router.Router
	src ingest.DescriptionSource
}

func (l llmWithDescriptions) Descriptions() ingest.DescriptionSource { return l.src }

// newDescriptions builds the YouTube description lookup over the saved key.
func newDescriptions(cfg config.Config, st secretReader, log *slog.Logger) *video.Descriptions {
	return &video.Descriptions{Keys: youtubeKey{store: st, log: log}, BaseURL: cfg.Video.APIHost}
}
