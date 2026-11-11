package claude

import (
	"context"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/llm"
)

// listPageSize is the most models one page of the listing returns.
const listPageSize = 1000

// ListModels returns the models the Anthropic API lists for key that task can
// use (ADR-00056): active ones whose listing marks structured output as
// supported, and for the vision task image input as well. gobbler's provider
// constrains its answer with a tool call and the listing reports nothing about
// tool use, so structured output is the nearest capability it does report. The
// order is the API's. A failure to list is an error and no partial list.
func ListModels(ctx context.Context, key config.Secret, task llm.Task, opts ...Option) ([]llm.Model, error) {
	if !key.IsSet() {
		return nil, errors.New("claude: API key is required to list models")
	}
	reqOpts := []option.RequestOption{option.WithAPIKey(key.Reveal()), option.WithMaxRetries(0)}
	for _, o := range opts {
		o(&reqOpts)
	}
	client := anthropic.NewClient(reqOpts...)
	var out []llm.Model
	pager := client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: param.NewOpt(int64(listPageSize))})
	for pager.Next() {
		m := pager.Current()
		if m.Lifecycle != anthropic.ModelInfoLifecycleActive || !m.Capabilities.StructuredOutputs.Supported {
			continue
		}
		if task == llm.TaskVision && !m.Capabilities.ImageInput.Supported {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		out = append(out, llm.Model{ID: m.ID, Name: name})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("claude: list models: %w", err)
	}
	return out, nil
}
