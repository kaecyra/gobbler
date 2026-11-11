// Package llm holds the LLM settings an operator saves on the Deployment page
// (ADR-00056, ADR-00057) and reads them back for the providers. There are two
// tasks, text and vision, and each names its own provider and model. A task
// with no provider, or no model, has no LLM. The two provider API keys are
// secrets in the settings store; the timeout is shared by both tasks.
//
// This package knows no SDK. The provider packages (claude, gemini) take a
// Settings; package router picks the provider a task names for each call.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/settings"
)

// Integration is the name the LLM settings have in the settings store.
const Integration = "llm"

// The LLM providers a task can name (ADR-00007).
const (
	ProviderClaude = "claude"
	ProviderGemini = "gemini"
)

// Providers lists the provider names in the order the page offers them.
var Providers = []string{ProviderClaude, ProviderGemini}

// Task is one kind of LLM work.
type Task string

const (
	// TaskText is structuring text: the gap-fill and the retry.
	TaskText Task = "text"
	// TaskVision is anything that sends an image: reading a photo's page into
	// text (ADR-00053) and structuring from an image.
	TaskVision Task = "vision"
)

func (t Task) String() string { return string(t) }

// Tasks lists the tasks in the order the page shows them.
var Tasks = []Task{TaskText, TaskVision}

// The LLM fields in the settings store. The two keys are secrets; the others
// are plain values.
const (
	FieldTextProvider   = "text_provider"
	FieldTextModel      = "text_model"
	FieldVisionProvider = "vision_provider"
	FieldVisionModel    = "vision_model"
	FieldTimeout        = "timeout"
	FieldAnthropicKey   = "anthropic_key"
	FieldGeminiKey      = "gemini_key"
)

// ProviderField and ModelField are the store fields holding task's provider
// and model.
func ProviderField(t Task) string {
	if t == TaskVision {
		return FieldVisionProvider
	}
	return FieldTextProvider
}

// ModelField is the store field holding task's model.
func ModelField(t Task) string {
	if t == TaskVision {
		return FieldVisionModel
	}
	return FieldTextModel
}

// KeyField is the store field holding provider's API key, or "" for a name
// that is not a provider.
func KeyField(provider string) string {
	switch provider {
	case ProviderClaude:
		return FieldAnthropicKey
	case ProviderGemini:
		return FieldGeminiKey
	}
	return ""
}

// DefaultTimeout is how long one call may take when no timeout is saved.
const DefaultTimeout = 60 * time.Second

// MaxTimeout bounds the saved timeout. A photo upload makes two calls in one
// request, a transcription and then a gap-fill, so the web server's write
// timeout is sized from this value (internal/web, newHTTPServer): raising it
// without raising that is the bug the bound exists to prevent.
const MaxTimeout = 2 * time.Minute

// ErrNotConfigured means the task has no LLM: no provider or no model is
// saved, or the provider's key is unset or unreadable. Nothing was attempted.
var ErrNotConfigured = errors.New("no LLM is configured for this task")

// Settings are the settings one provider is built from for one task.
type Settings struct {
	// Provider is ProviderClaude or ProviderGemini.
	Provider string
	// Model is the provider's model identifier for the task.
	Model   string
	APIKey  config.Secret
	Timeout time.Duration
}

// Model is one entry of a provider's model listing.
type Model struct {
	// ID is what a call names.
	ID string
	// Name is what the page shows; it may equal ID.
	Name string
}

// Reader is what Load reads from the settings store. *settings.Store
// satisfies it.
type Reader interface {
	Value(ctx context.Context, f settings.Field) (string, error)
	Secret(ctx context.Context, f settings.Field) (string, error)
}

func field(name string) settings.Field { return settings.Field{Integration: Integration, Name: name} }

// Load reads the settings for task. It returns ErrNotConfigured, wrapped, when
// the task has no LLM (see ErrNotConfigured). A saved value that cannot be
// used, such as an unknown provider or a timeout that is not a duration, is an
// error: the Deployment page does not save one, so it is a fault to be seen.
// Every call reads the store, so a change is in force on the next call.
func Load(ctx context.Context, r Reader, task Task) (Settings, error) {
	provider, err := r.Value(ctx, field(ProviderField(task)))
	if errors.Is(err, settings.ErrUnset) {
		return Settings{}, fmt.Errorf("no %s provider saved: %w", task, ErrNotConfigured)
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read %s provider: %w", task, err)
	}
	if KeyField(provider) == "" {
		return Settings{}, fmt.Errorf("saved %s provider %q is not one of %s", task, provider, strings.Join(Providers, ", "))
	}
	model, err := r.Value(ctx, field(ModelField(task)))
	if errors.Is(err, settings.ErrUnset) {
		return Settings{}, fmt.Errorf("no %s model saved: %w", task, ErrNotConfigured)
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read %s model: %w", task, err)
	}
	apiKey, err := LoadKey(ctx, r, provider)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{Provider: provider, Model: model, APIKey: apiKey, Timeout: DefaultTimeout}
	raw, err := r.Value(ctx, field(FieldTimeout))
	switch {
	case errors.Is(err, settings.ErrUnset):
	case err != nil:
		return Settings{}, fmt.Errorf("read LLM timeout: %w", err)
	default:
		if s.Timeout, err = time.ParseDuration(raw); err != nil || s.Timeout <= 0 || s.Timeout > MaxTimeout {
			return Settings{}, fmt.Errorf("saved LLM timeout %q is not a duration up to %s", raw, MaxTimeout)
		}
	}
	return s, nil
}

// LoadKey reads provider's API key. It returns ErrNotConfigured, wrapped, when
// the key is unset or cannot be opened (ADR-00057: the integration is off).
func LoadKey(ctx context.Context, r Reader, provider string) (config.Secret, error) {
	name := KeyField(provider)
	if name == "" {
		return config.Secret{}, fmt.Errorf("%q is not one of %s", provider, strings.Join(Providers, ", "))
	}
	v, err := r.Secret(ctx, field(name))
	switch {
	case errors.Is(err, settings.ErrUnset), errors.Is(err, settings.ErrUnreadable):
		return config.Secret{}, fmt.Errorf("%s API key unavailable: %w: %w", provider, err, ErrNotConfigured)
	case err != nil:
		return config.Secret{}, fmt.Errorf("read %s API key: %w", provider, err)
	}
	return config.NewSecret(v), nil
}

// ValidTimeout reports the problem with a timeout as typed on the page, or ""
// when it is blank (the default applies) or a duration up to MaxTimeout.
func ValidTimeout(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 || d > MaxTimeout {
		return "Timeout must be a duration such as 60s, up to " + MaxTimeout.String() + "."
	}
	return ""
}
