// Package router is the ingest.Provider the app wires in. Each call reads the
// saved LLM settings (llm.Load), builds the provider the call's task names,
// and makes the call, so a setting saved on the Deployment page is in force on
// the next call with no restart (ADR-00056). The text task and the vision task
// are set apart: Claude can answer one and Gemini the other.
//
// Each call it makes is recorded with its tokens, latency, outcome and the
// household it ran for (ADR-00058). A record that cannot be stored is logged
// and does not fail the call. It also lists a provider's models for the
// Deployment page, with the stored key.
package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm"
	"github.com/kaecyra/gobbler/internal/llm/claude"
	"github.com/kaecyra/gobbler/internal/llm/gemini"
	"github.com/kaecyra/gobbler/internal/llmusage"
)

// listTimeout bounds one model listing, which the Deployment page waits for.
const listTimeout = 15 * time.Second

// recordTimeout bounds storing one call's record.
const recordTimeout = 5 * time.Second

// Recorder stores the record of one LLM call (ADR-00058).
// *llmusage.Store satisfies it.
type Recorder interface {
	Record(ctx context.Context, c llmusage.Call) error
}

// Router implements ingest.Provider and ingest.Availability over the saved
// settings. It holds no per-call state and is safe for concurrent use.
type Router struct {
	settings llm.Reader
	log      *slog.Logger
	claude   []claude.Option
	gemini   []gemini.Option
	recorder Recorder
}

var (
	_ ingest.Provider     = (*Router)(nil)
	_ ingest.Availability = (*Router)(nil)
)

// Option adjusts a Router; tests use it to point the SDKs at fake servers.
type Option func(*Router)

// WithClaudeOptions passes opts to every Claude provider the router builds and
// every Claude listing it makes.
func WithClaudeOptions(opts ...claude.Option) Option {
	return func(r *Router) { r.claude = append(r.claude, opts...) }
}

// WithGeminiOptions passes opts to every Gemini provider the router builds and
// every Gemini listing it makes.
func WithGeminiOptions(opts ...gemini.Option) Option {
	return func(r *Router) { r.gemini = append(r.gemini, opts...) }
}

// WithRecorder records every call the router makes. Without it none is.
func WithRecorder(rec Recorder) Option {
	return func(r *Router) { r.recorder = rec }
}

// New returns a Router over the saved settings in s. A nil log means
// slog.Default().
func New(s llm.Reader, log *slog.Logger, opts ...Option) *Router {
	if log == nil {
		log = slog.Default()
	}
	r := &Router{settings: s, log: log}
	for _, o := range opts {
		o(r)
	}
	return r
}

func taskOf(t ingest.Task) llm.Task {
	if t == ingest.TaskVision {
		return llm.TaskVision
	}
	return llm.TaskText
}

// Available reports whether a call for task can be made now. A task with no
// provider, no model or no key that opens is off. A saved value that cannot be
// used counts as available, so the call fails with the reason in front of the
// person instead of the LLM quietly being off.
func (r *Router) Available(ctx context.Context, task ingest.Task) bool {
	_, err := llm.Load(ctx, r.settings, taskOf(task))
	return err == nil || !errors.Is(err, llm.ErrNotConfigured)
}

// provider builds the provider the task names from the settings saved now, and
// returns those settings.
func (r *Router) provider(ctx context.Context, task llm.Task) (ingest.Provider, llm.Settings, error) {
	s, err := llm.Load(ctx, r.settings, task)
	if errors.Is(err, llm.ErrNotConfigured) {
		return nil, s, fmt.Errorf("%w: %w", ingest.ErrNoProvider, err)
	}
	if err != nil {
		return nil, s, fmt.Errorf("load %s LLM settings: %w", task, err)
	}
	var p ingest.Provider
	switch s.Provider {
	case llm.ProviderClaude:
		p, err = claude.New(ctx, s, r.log, r.claude...)
	case llm.ProviderGemini:
		p, err = gemini.New(ctx, s, r.log, r.gemini...)
	default:
		err = fmt.Errorf("saved %s provider %q is not one of %v", task, s.Provider, llm.Providers)
	}
	return p, s, err
}

// call runs do, which makes the call on the provider s names, and records it.
// Only a call that was made is recorded: a task with no LLM, or settings that
// cannot build a provider, never reach it. The record is stored whatever the
// call's context did, and a record that fails is logged and leaves the call's
// own result as it is (ADR-00007, ADR-00058).
func (r *Router) call(ctx context.Context, task llm.Task, s llm.Settings, do func(ctx context.Context) error) error {
	if r.recorder == nil {
		return do(ctx)
	}
	start := time.Now()
	ctx, report := llm.WithUsageReport(ctx)
	err := do(ctx)
	u := report.Usage()
	c := llmusage.Call{
		At: start, Task: task, Provider: s.Provider, Model: s.Model,
		Input: u.Input, Output: u.Output, Latency: time.Since(start),
	}
	if h, ok := household.FromContext(ctx); ok {
		c.HouseholdID = h.ID
	}
	if err != nil {
		c.Err = redact(err.Error(), s.APIKey)
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if rerr := r.recorder.Record(rctx, c); rerr != nil {
		r.log.ErrorContext(ctx, "llm call not recorded; the call's result stands",
			"task", task.String(), "provider", s.Provider, "model", s.Model, "error", redact(rerr.Error(), s.APIKey))
	}
	return err
}

// redact removes key from msg. A provider that echoes the key in an error must
// not put it on a page or in a record (ADR-00057).
func redact(msg string, key config.Secret) string {
	if !key.IsSet() {
		return msg
	}
	return strings.ReplaceAll(msg, key.Reveal(), "[REDACTED]")
}

// Structure sends req to the provider its task names: the vision task's when
// it carries an image, the text task's otherwise.
func (r *Router) Structure(ctx context.Context, req ingest.Request) (ingest.Structured, error) {
	task := taskOf(req.Task())
	p, s, err := r.provider(ctx, task)
	if err != nil {
		return ingest.Structured{}, err
	}
	var out ingest.Structured
	err = r.call(ctx, task, s, func(ctx context.Context) (err error) {
		out, err = p.Structure(ctx, req)
		return err
	})
	return out, err
}

// Transcribe reads the photo with the vision task's provider.
func (r *Router) Transcribe(ctx context.Context, req ingest.TranscribeRequest) (string, error) {
	p, s, err := r.provider(ctx, llm.TaskVision)
	if err != nil {
		return "", err
	}
	var text string
	err = r.call(ctx, llm.TaskVision, s, func(ctx context.Context) (err error) {
		text, err = p.Transcribe(ctx, req)
		return err
	})
	return text, err
}

// ErrNoKey means the provider's API key is unset or unreadable, so there is
// nothing to list with.
var ErrNoKey = errors.New("no API key saved for this provider")

// Models lists the models provider offers for task, with the saved API key
// (ADR-00056). It returns ErrNoKey, wrapped, when the key is unset or
// unreadable. A listing that fails is an error and no list.
func (r *Router) Models(ctx context.Context, provider string, task llm.Task) ([]llm.Model, error) {
	keyField := llm.KeyField(provider)
	if keyField == "" {
		return nil, fmt.Errorf("%q is not a provider", provider)
	}
	key, err := llm.LoadKey(ctx, r.settings, provider)
	if err != nil {
		if errors.Is(err, llm.ErrNotConfigured) {
			return nil, fmt.Errorf("%w: %w", ErrNoKey, err)
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	var models []llm.Model
	if provider == llm.ProviderClaude {
		models, err = claude.ListModels(ctx, key, task, r.claude...)
	} else {
		models, err = gemini.ListModels(ctx, key, task, r.gemini...)
	}
	if err != nil {
		// The message reaches the Deployment page; a provider that echoes the
		// key in an error must not put it there (ADR-00057).
		return nil, errors.New(redact(err.Error(), key))
	}
	return models, nil
}
