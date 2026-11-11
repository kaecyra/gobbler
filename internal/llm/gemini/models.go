package gemini

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/genai"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/llm"
)

// generateAction is the supported action that marks a model as one that
// generates content.
const generateAction = "generateContent"

// ListModels returns the models the Gemini API lists for key that support
// content generation (ADR-00056). Gemini reports no separate image-input flag,
// so the text and vision tasks get the same list. The order is the API's, and
// a model's ID is its name without the "models/" prefix. A failure to list is
// an error and no partial list.
func ListModels(ctx context.Context, key config.Secret, _ llm.Task, opts ...Option) ([]llm.Model, error) {
	if !key.IsSet() {
		return nil, errors.New("gemini: API key is required to list models")
	}
	cc := &genai.ClientConfig{APIKey: key.Reveal(), Backend: genai.BackendGeminiAPI}
	for _, o := range opts {
		o(cc)
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("gemini: build client: %w", err)
	}
	var out []llm.Model
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			return nil, fmt.Errorf("gemini: list models: %w", err)
		}
		if !slices.Contains(m.SupportedActions, generateAction) {
			continue
		}
		id := strings.TrimPrefix(m.Name, "models/")
		name := m.DisplayName
		if name == "" {
			name = id
		}
		out = append(out, llm.Model{ID: id, Name: name})
	}
	return out, nil
}
