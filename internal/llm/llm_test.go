package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/settings"
)

// fakeReader serves values by field name; names in secrets are read with
// Secret, the others with Value. A missing name is ErrUnset.
type fakeReader struct {
	values    map[string]string
	secrets   map[string]string
	secretErr error
}

func (f fakeReader) Value(_ context.Context, fl settings.Field) (string, error) {
	if v, ok := f.values[fl.Name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("read: %w", settings.ErrUnset)
}

func (f fakeReader) Secret(_ context.Context, fl settings.Field) (string, error) {
	if f.secretErr != nil {
		return "", f.secretErr
	}
	if v, ok := f.secrets[fl.Name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("read: %w", settings.ErrUnset)
}

func full() fakeReader {
	return fakeReader{
		values: map[string]string{
			FieldTextProvider: ProviderClaude, FieldTextModel: "text-m",
			FieldVisionProvider: ProviderGemini, FieldVisionModel: "vision-m",
		},
		secrets: map[string]string{FieldAnthropicKey: "ak", FieldGeminiKey: "gk"},
	}
}

// Each task reads its own provider and model, and the key of that provider.
func TestLoadReadsEachTasksOwnSettings(t *testing.T) {
	ctx := context.Background()
	text, err := Load(ctx, full(), TaskText)
	if err != nil || text.Provider != ProviderClaude || text.Model != "text-m" || text.APIKey.Reveal() != "ak" || text.Timeout != DefaultTimeout {
		t.Fatalf("text = %+v, %v", text, err)
	}
	vision, err := Load(ctx, full(), TaskVision)
	if err != nil || vision.Provider != ProviderGemini || vision.Model != "vision-m" || vision.APIKey.Reveal() != "gk" {
		t.Fatalf("vision = %+v, %v", vision, err)
	}
	r := full()
	r.values[FieldTimeout] = "45s"
	if s, err := Load(ctx, r, TaskText); err != nil || s.Timeout != 45*time.Second {
		t.Errorf("with timeout = %+v, %v", s, err)
	}
}

func TestLoadOffStates(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(*fakeReader){
		"no provider":    func(r *fakeReader) { delete(r.values, FieldTextProvider) },
		"no model":       func(r *fakeReader) { delete(r.values, FieldTextModel) },
		"no key":         func(r *fakeReader) { delete(r.secrets, FieldAnthropicKey) },
		"unreadable key": func(r *fakeReader) { r.secretErr = fmt.Errorf("read: %w", settings.ErrUnreadable) },
	} {
		t.Run(name, func(t *testing.T) {
			r := full()
			mutate(&r)
			if _, err := Load(ctx, r, TaskText); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("err = %v, want ErrNotConfigured", err)
			}
		})
	}
	// The other task is not affected.
	r := full()
	delete(r.values, FieldTextProvider)
	if _, err := Load(ctx, r, TaskVision); err != nil {
		t.Errorf("vision with text off: %v", err)
	}
}

// A saved value nothing can use is a fault, not an off switch.
func TestLoadFaultsAreErrorsNotOff(t *testing.T) {
	ctx := context.Background()
	for name, mutate := range map[string]func(*fakeReader){
		"unknown provider":   func(r *fakeReader) { r.values[FieldTextProvider] = "gpt" },
		"timeout not a time": func(r *fakeReader) { r.values[FieldTimeout] = "soon" },
		"timeout over max":   func(r *fakeReader) { r.values[FieldTimeout] = (MaxTimeout + time.Second).String() },
		"timeout zero":       func(r *fakeReader) { r.values[FieldTimeout] = "0s" },
		"secret read broke":  func(r *fakeReader) { r.secretErr = errors.New("disk on fire") },
	} {
		t.Run(name, func(t *testing.T) {
			r := full()
			mutate(&r)
			_, err := Load(ctx, r, TaskText)
			if err == nil || errors.Is(err, ErrNotConfigured) {
				t.Errorf("err = %v, want a fault that is not ErrNotConfigured", err)
			}
		})
	}
}

// The timeout is bounded at save as well as at load, so the web server's write
// timeout (sized from MaxTimeout) outlasts any call the page lets through.
func TestValidTimeoutBoundsTheSetting(t *testing.T) {
	for v, ok := range map[string]bool{
		"": true, "  ": true, "60s": true, "1m30s": true, MaxTimeout.String(): true,
		(MaxTimeout + time.Second).String(): false, "0s": false, "-5s": false, "soon": false, "10": false,
	} {
		if got := ValidTimeout(v) == ""; got != ok {
			t.Errorf("ValidTimeout(%q) accepted = %v, want %v", v, got, ok)
		}
	}
	if msg := ValidTimeout("1h"); !strings.Contains(msg, MaxTimeout.String()) {
		t.Errorf("message %q does not state the bound", msg)
	}
}

func TestKeyFieldNamesTheProvidersKey(t *testing.T) {
	if KeyField(ProviderClaude) == KeyField(ProviderGemini) || KeyField("gpt") != "" {
		t.Error("each provider has its own key field and an unknown one has none")
	}
}
