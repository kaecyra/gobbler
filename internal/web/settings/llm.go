package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kaecyra/gobbler/internal/llm"
	store "github.com/kaecyra/gobbler/internal/settings"
)

// ModelLister lists the models a provider offers for a task, called with the
// provider's saved key (ADR-00056). *router.Router satisfies it. It returns an
// error wrapping llm.ErrNotConfigured when no key is saved, and any other error
// when the listing itself failed.
type ModelLister interface {
	Models(ctx context.Context, provider string, task llm.Task) ([]llm.Model, error)
}

// LLMDeps are what the LLM section reads: the saved values, for the provider a
// model list is for, and the lister.
type LLMDeps struct {
	Values ValueStore
	Models ModelLister
}

var providerLabels = map[string]string{llm.ProviderClaude: "Claude", llm.ProviderGemini: "Gemini"}

var keyLabels = map[string]string{llm.ProviderClaude: "Claude API key", llm.ProviderGemini: "Gemini API key"}

func taskLabel(t llm.Task) string { return strings.ToUpper(string(t)[:1]) + string(t)[1:] }

// LLMSection is the LLM integration's part of the page (ADR-00056): for each
// of the text and vision tasks a provider and a model, one timeout, and each
// provider's API key as a secret. The model is chosen from the provider's own
// listing, narrowed to what the task can use; it is not typed.
//
// Changing a task's provider clears its model, and the page then lists the new
// provider's. A model that is not in the listing is refused. A listing that
// cannot be had leaves the saved model as it is. Wiring registers it on
// Integrations.Sections.
func LLMSection(d LLMDeps) Section {
	var fields []PlainField
	for _, t := range llm.Tasks {
		fields = append(fields,
			PlainField{Name: llm.ProviderField(t), Label: taskLabel(t) + " provider", Choices: providerChoices},
			PlainField{Name: llm.ModelField(t), Label: taskLabel(t) + " model", Choices: d.modelChoices(t)},
		)
	}
	fields = append(fields, PlainField{
		Name: llm.FieldTimeout, Label: "Timeout", Placeholder: llm.DefaultTimeout.String(),
		Hint: "How long one call may take, up to " + llm.MaxTimeout.String() + ". Optional.",
	})
	var secrets []SecretField
	for _, p := range llm.Providers {
		secrets = append(secrets, SecretField{Name: llm.KeyField(p), Label: keyLabels[p]})
	}
	return Section{Integration: llm.Integration, Title: "LLM", Fields: fields, Secrets: secrets, Validate: d.validate}
}

func providerChoices(context.Context) Choices {
	c := Choices{Options: []Choice{{Value: "", Label: "None"}}}
	for _, p := range llm.Providers {
		c.Options = append(c.Options, Choice{Value: p, Label: providerLabels[p]})
	}
	return c
}

// saved reads a saved plain value; unset is "".
func (d LLMDeps) saved(ctx context.Context, name string) (string, error) {
	v, err := d.Values.Value(ctx, store.Field{Integration: llm.Integration, Name: name})
	if err != nil && !errors.Is(err, store.ErrUnset) {
		return "", fmt.Errorf("read LLM %s: %w", name, err)
	}
	return v, nil
}

// listing returns the models the saved provider of task offers.
func (d LLMDeps) listing(ctx context.Context, provider string, t llm.Task) ([]llm.Model, error) {
	return d.Models.Models(ctx, provider, t)
}

// modelChoices are the model options of task t: what its saved provider lists.
func (d LLMDeps) modelChoices(t llm.Task) func(context.Context) Choices {
	return func(ctx context.Context) Choices {
		provider, err := d.saved(ctx, llm.ProviderField(t))
		if err != nil {
			return Choices{Problem: "The saved provider could not be read: " + err.Error()}
		}
		choose := Choice{Value: "", Label: "Choose a model"}
		if provider == "" {
			return Choices{Options: []Choice{choose}}
		}
		models, err := d.listing(ctx, provider, t)
		switch {
		case errors.Is(err, llm.ErrNotConfigured):
			return Choices{Problem: "Save the " + keyLabels[provider] + " to list its models."}
		case err != nil:
			return Choices{Problem: "The models could not be listed: " + err.Error()}
		}
		c := Choices{Options: []Choice{choose}}
		for _, m := range models {
			label := m.Name
			if label != m.ID {
				label += " (" + m.ID + ")"
			}
			c.Options = append(c.Options, Choice{Value: m.ID, Label: label})
		}
		return c
	}
}

// validate checks what was submitted and settles each task's model against its
// provider: a cleared or changed provider clears the model, and a model other
// than the saved one must be in the provider's listing.
func (d LLMDeps) validate(ctx context.Context, values map[string]string, _ map[string]bool) string {
	if msg := llm.ValidTimeout(values[llm.FieldTimeout]); msg != "" {
		return msg
	}
	for _, t := range llm.Tasks {
		pf, mf := llm.ProviderField(t), llm.ModelField(t)
		provider := values[pf]
		if provider != "" && !slices.Contains(llm.Providers, provider) {
			return taskLabel(t) + " provider is not one of the providers offered."
		}
		savedProvider, err := d.saved(ctx, pf)
		if err != nil {
			return err.Error()
		}
		model, posted := values[mf]
		switch {
		case provider == "" || provider != savedProvider:
			// The model belonged to the old provider (or to no provider).
			values[mf] = ""
		case !posted || model == "":
			// No input was offered, or none chosen: nothing to check.
		default:
			savedModel, err := d.saved(ctx, mf)
			if err != nil {
				return err.Error()
			}
			if model == savedModel {
				continue // a saved model may stay although the listing dropped it
			}
			models, err := d.listing(ctx, provider, t)
			if err != nil {
				return "The " + t.String() + " model was not saved: the models could not be listed."
			}
			if !slices.ContainsFunc(models, func(m llm.Model) bool { return m.ID == model }) {
				return taskLabel(t) + " model is not one " + providerLabels[provider] + " offers for this task."
			}
		}
	}
	return ""
}
