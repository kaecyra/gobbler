package llmusage

import (
	"context"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/settings"
)

// Configured reads the models the text and vision tasks are set to use from the
// saved LLM settings (ADR-00056).
type Configured struct {
	Values llm.Reader
}

var _ ModelSource = Configured{}

// Configured returns each task's provider and model, once, skipping a task
// with either unset or a provider gobbler does not offer.
func (c Configured) Configured(ctx context.Context) ([]ModelRef, error) {
	var refs []ModelRef
	seen := map[ModelRef]bool{}
	for _, t := range llm.Tasks {
		provider, err := c.value(ctx, llm.ProviderField(t))
		if err != nil {
			return nil, err
		}
		model, err := c.value(ctx, llm.ModelField(t))
		if err != nil {
			return nil, err
		}
		ref := ModelRef{Provider: provider, Model: model}
		if llm.KeyField(provider) == "" || model == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs, nil
}

func (c Configured) value(ctx context.Context, name string) (string, error) {
	v, err := c.Values.Value(ctx, settings.Field{Integration: llm.Integration, Name: name})
	if err != nil && !errors.Is(err, settings.ErrUnset) {
		return "", fmt.Errorf("read LLM %s: %w", name, err)
	}
	return v, nil
}
