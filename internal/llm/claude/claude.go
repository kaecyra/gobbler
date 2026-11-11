// Package claude is the Anthropic Claude implementation of ingest.Provider
// (ADR-00007). It calls the Messages API through the official Go SDK and makes
// the model fill the ingestion schema by calling a single tool whose input
// schema is that schema; it uses no other provider feature, so it stays
// interchangeable with the Gemini provider.
//
// The model, the API key and the timeout are the llm.Settings passed to New,
// read from the saved settings on each call (package router); nothing here
// names a model. A response that is
// not exactly the schema is an error, never a partial draft (ADR-00006).
package claude

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/llm"
)

const (
	providerName = "claude"
	toolName     = "record_recipe"
	// maxOutputTokens bounds one structured recipe; a response cut off by it
	// is an error, not a draft.
	maxOutputTokens = 8192
)

// Option adjusts a Provider; tests use it to point the SDK at a fake server.
type Option func(*[]option.RequestOption)

// WithBaseURL sends requests to baseURL instead of the Anthropic API.
func WithBaseURL(baseURL string) Option {
	return func(o *[]option.RequestOption) { *o = append(*o, option.WithBaseURL(baseURL)) }
}

// Provider implements ingest.Provider over the Anthropic Messages API.
type Provider struct {
	client  anthropic.Client
	model   string
	timeout time.Duration
	log     *slog.Logger
}

var _ ingest.Provider = (*Provider)(nil)

// New builds a Provider from the configured settings, the same shape as
// gemini.New. It fails when the key, a model or the timeout is missing, so a
// misconfiguration surfaces when the call is made, with the reason. It
// never reads the environment; ctx is unused and exists so every provider
// constructor has one signature.
func New(_ context.Context, cfg llm.Settings, log *slog.Logger, opts ...Option) (*Provider, error) {
	switch {
	case cfg.Provider != llm.ProviderClaude:
		return nil, fmt.Errorf("claude: configured provider is %q", cfg.Provider)
	case !cfg.APIKey.IsSet():
		return nil, errors.New("claude: API key is required")
	case cfg.Model == "":
		return nil, errors.New("claude: model is required")
	case cfg.Timeout <= 0:
		return nil, errors.New("claude: timeout must be positive")
	}
	reqOpts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey.Reveal()),
		// One attempt: the call timeout covers the whole call, and a failure
		// is shown on the review screen rather than retried behind it.
		option.WithMaxRetries(0),
	}
	for _, o := range opts {
		o(&reqOpts)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Provider{
		client:  anthropic.NewClient(reqOpts...),
		model:   cfg.Model,
		timeout: cfg.Timeout,
		log:     log,
	}, nil
}

// Structure asks Claude for the recipe in req and returns it validated. The
// timeout and cancellation errors wrap context.DeadlineExceeded and
// context.Canceled; a backend failure wraps neither.
func (p *Provider) Structure(ctx context.Context, req ingest.Request) (ingest.Structured, error) {
	model := p.model
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	start := time.Now()
	s, usage, err := p.call(ctx, model, req)
	attrs := []slog.Attr{
		slog.String("provider", providerName),
		slog.String("model", model),
		slog.Int64("latency_ms", time.Since(start).Milliseconds()),
	}
	llm.ReportUsage(ctx, llm.Usage{Input: usage.InputTokens, Output: usage.OutputTokens})
	if err != nil {
		p.log.LogAttrs(ctx, slog.LevelWarn, "llm call failed", append(attrs, slog.String("error", err.Error()))...)
		return ingest.Structured{}, err
	}
	attrs = append(attrs,
		slog.Int64("input_tokens", usage.InputTokens),
		slog.Int64("output_tokens", usage.OutputTokens))
	p.log.LogAttrs(ctx, slog.LevelInfo, "llm call", attrs...)
	return s, nil
}

// Transcribe asks Claude to read the photo in req into text, with the vision
// model. Timeouts and cancellation wrap the context errors as Structure's do.
func (p *Provider) Transcribe(ctx context.Context, req ingest.TranscribeRequest) (string, error) {
	if len(req.Image) == 0 {
		return "", errors.New("claude: no image to transcribe")
	}
	model := p.model
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	start := time.Now()
	text, usage, err := p.transcribe(ctx, model, req)
	attrs := []slog.Attr{
		slog.String("provider", providerName),
		slog.String("model", model),
		slog.String("task", "transcribe"),
		slog.Int64("latency_ms", time.Since(start).Milliseconds()),
	}
	llm.ReportUsage(ctx, llm.Usage{Input: usage.InputTokens, Output: usage.OutputTokens})
	if err != nil {
		p.log.LogAttrs(ctx, slog.LevelWarn, "llm call failed", append(attrs, slog.String("error", err.Error()))...)
		return "", err
	}
	attrs = append(attrs,
		slog.Int64("input_tokens", usage.InputTokens),
		slog.Int64("output_tokens", usage.OutputTokens))
	p.log.LogAttrs(ctx, slog.LevelInfo, "llm call", attrs...)
	return text, nil
}

func (p *Provider) transcribe(ctx context.Context, model string, req ingest.TranscribeRequest) (string, anthropic.Usage, error) {
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxOutputTokens,
		System:    []anthropic.TextBlockParam{{Text: transcribePrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(transcribeBlocks(req)...)},
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", anthropic.Usage{}, fmt.Errorf("claude call to %s: %w", model, ctxErr)
		}
		return "", anthropic.Usage{}, fmt.Errorf("claude call to %s: %w", model, err)
	}
	// A response came back, so its tokens were billed whatever follows
	// (ADR-00058).
	text, err := transcript(resp)
	if err != nil {
		return "", resp.Usage, fmt.Errorf("claude response from %s: %w", model, err)
	}
	return text, resp.Usage, nil
}

// transcript is the text of a Messages response, or an error when the model
// declined, was cut off, or said nothing.
func transcript(resp *anthropic.Message) (string, error) {
	switch resp.StopReason {
	case anthropic.StopReasonMaxTokens:
		return "", errors.New("output cut off at the token limit")
	case anthropic.StopReasonRefusal:
		return "", errors.New("the model declined the request")
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", errors.New("the model returned no text")
	}
	return text, nil
}

func (p *Provider) call(ctx context.Context, model string, req ingest.Request) (ingest.Structured, anthropic.Usage, error) {
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: maxOutputTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(userBlocks(req)...)},
		Tools: []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        toolName,
			Description: anthropic.String("Record the recipe found in the source in the ingestion schema."),
			InputSchema: recipeSchema(),
		}}},
	})
	if err != nil {
		// The call context ended: report why, so callers can tell a timeout
		// or a cancellation from a backend failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ingest.Structured{}, anthropic.Usage{}, fmt.Errorf("claude call to %s: %w", model, ctxErr)
		}
		return ingest.Structured{}, anthropic.Usage{}, fmt.Errorf("claude call to %s: %w", model, err)
	}
	// A response came back, so its tokens were billed whatever follows
	// (ADR-00058).
	s, err := decode(resp)
	if err != nil {
		return ingest.Structured{}, resp.Usage, fmt.Errorf("claude response from %s: %w", model, err)
	}
	return s, resp.Usage, nil
}

// decode turns a Messages response into a validated Structured.
func decode(resp *anthropic.Message) (ingest.Structured, error) {
	switch resp.StopReason {
	case anthropic.StopReasonMaxTokens:
		return ingest.Structured{}, errors.New("output cut off at the token limit")
	case anthropic.StopReasonRefusal:
		return ingest.Structured{}, errors.New("the model declined the request")
	}
	for _, block := range resp.Content {
		tu, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok || tu.Name != toolName {
			continue
		}
		return ingest.DecodeStructured(tu.Input)
	}
	return ingest.Structured{}, fmt.Errorf("the model did not call %s", toolName)
}
