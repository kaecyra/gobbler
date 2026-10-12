// Package gemini is the Gemini (Google Gen AI API) implementation of
// ingest.Provider (ADR-00007). It uses the official Go SDK's structured
// output: the request carries a response schema and a JSON MIME type, so the
// model answers with an ingest.Structured, and the answer is validated again
// before use, because the schema constrains the model and does not guarantee
// the response (an invalid response is an error, never a partial draft).
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/ingest"
)

// providerName is the value logged under the provider key.
const providerName = "gemini"

// ErrInvalidResponse marks a response that is not a valid ingest.Structured:
// the same sentinel ingest.DecodeStructured wraps.
var ErrInvalidResponse = ingest.ErrInvalidStructured

const systemPrompt = `You turn recipe source material into structured data.
Return only the recipe the source contains; never invent ingredients, quantities or steps.
Keep each ingredient line exactly as written, with its quantity and qualifiers.
Put each separately headed part of the recipe (for example "For the sauce") in its own component, naming it; use one unnamed component when there are no parts.
Keep times and yield as written in the source.
A rule-based reading of the source is included as a draft: use it where it is right and correct it where it is wrong.`

// Provider calls the Gemini API. It is safe for concurrent use.
type Provider struct {
	client      *genai.Client
	textModel   string
	visionModel string
	timeout     time.Duration
	log         *slog.Logger
}

// Option adjusts a Provider; tests use it to point the SDK at a fake backend.
type Option func(*genai.ClientConfig)

// WithBaseURL sends requests to baseURL instead of the Gemini API.
func WithBaseURL(baseURL string) Option {
	return func(c *genai.ClientConfig) { c.HTTPOptions.BaseURL = baseURL }
}

// New builds a Provider from the configured settings, the same shape as
// claude.New. It fails when the key, a model or the timeout is
// missing, so a misconfiguration surfaces at startup rather than on the first
// ingest.
func New(ctx context.Context, cfg config.LLM, log *slog.Logger, opts ...Option) (*Provider, error) {
	switch {
	case cfg.Provider != config.ProviderGemini:
		return nil, fmt.Errorf("gemini: configured provider is %q", cfg.Provider)
	case !cfg.APIKey.IsSet():
		return nil, errors.New("gemini: API key is required")
	case cfg.TextModel == "":
		return nil, errors.New("gemini: text model is required")
	case cfg.VisionModel == "":
		return nil, errors.New("gemini: vision model is required")
	case cfg.Timeout <= 0:
		return nil, errors.New("gemini: timeout must be positive")
	}
	cc := &genai.ClientConfig{APIKey: cfg.APIKey.Reveal(), Backend: genai.BackendGeminiAPI}
	for _, o := range opts {
		o(cc)
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return nil, fmt.Errorf("gemini: build client: %w", err)
	}
	return &Provider{
		client:      client,
		textModel:   cfg.TextModel,
		visionModel: cfg.VisionModel,
		timeout:     cfg.Timeout,
		log:         log,
	}, nil
}

// Structure asks Gemini for the recipe in req. A request with an image uses the
// vision model, any other the text model.
func (p *Provider) Structure(ctx context.Context, req ingest.Request) (ingest.Structured, error) {
	model := p.textModel
	if len(req.Image) > 0 {
		model = p.visionModel
	}
	start := time.Now()
	out, usage, err := p.call(ctx, model, req)
	attrs := []any{
		slog.String("provider", providerName),
		slog.String("model", model),
		slog.Int64("latency_ms", time.Since(start).Milliseconds()),
	}
	if err != nil {
		p.log.ErrorContext(ctx, "llm call failed", append(attrs, slog.String("error", err.Error()))...)
		return ingest.Structured{}, err
	}
	p.log.InfoContext(ctx, "llm call", append(attrs,
		slog.Int("input_tokens", int(usage.PromptTokenCount)),
		// Thinking tokens are billed as output, so they are counted in it.
		slog.Int("output_tokens", int(usage.CandidatesTokenCount+usage.ThoughtsTokenCount)))...)
	return out, nil
}

func (p *Provider) call(ctx context.Context, model string, req ingest.Request) (ingest.Structured, genai.GenerateContentResponseUsageMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cfg := &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser),
		ResponseMIMEType:  "application/json",
		ResponseSchema:    responseSchema(),
	}
	resp, err := p.client.Models.GenerateContent(ctx, model, []*genai.Content{userContent(req)}, cfg)
	if err != nil {
		// The SDK may report a deadline or cancellation as a transport error;
		// the context is the authority on which it was.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ingest.Structured{}, genai.GenerateContentResponseUsageMetadata{}, fmt.Errorf("gemini call to %s: %w", model, ctxErr)
		}
		return ingest.Structured{}, genai.GenerateContentResponseUsageMetadata{}, fmt.Errorf("gemini call to %s: %w", model, err)
	}
	text, err := answerText(resp)
	if err != nil {
		return ingest.Structured{}, genai.GenerateContentResponseUsageMetadata{}, err
	}
	out, err := decode(text)
	if err != nil {
		return ingest.Structured{}, genai.GenerateContentResponseUsageMetadata{}, err
	}
	var usage genai.GenerateContentResponseUsageMetadata
	if resp.UsageMetadata != nil {
		usage = *resp.UsageMetadata
	}
	return out, usage, nil
}

// userContent is the request's source plus a compact reading of the rule-based
// draft. The draft's source snapshot is not sent: the model has the source
// already, and the snapshot can be a whole page.
func userContent(req ingest.Request) *genai.Content {
	var parts []*genai.Part
	if len(req.Image) > 0 {
		parts = append(parts, genai.NewPartFromBytes(req.Image, req.ImageMIME))
	}
	var b strings.Builder
	if req.Text != "" {
		b.WriteString("Source text:\n")
		b.WriteString(req.Text)
		b.WriteString("\n\n")
	}
	b.WriteString("Rule-based draft:\n")
	b.WriteString(draftSummary(req.RuleDraft))
	parts = append(parts, genai.NewPartFromText(b.String()))
	return genai.NewContentFromParts(parts, genai.RoleUser)
}

type draftView struct {
	Name       string          `json:"name,omitempty"`
	Components []componentView `json:"components,omitempty"`
	Issues     []string        `json:"issues,omitempty"`
}

type componentView struct {
	Name        string   `json:"name,omitempty"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}

func draftSummary(d ingest.Draft) string {
	v := draftView{Name: d.Name.Value, Issues: d.Issues}
	for _, c := range d.Components {
		cv := componentView{Name: c.Name}
		for _, l := range c.Lines {
			cv.Ingredients = append(cv.Ingredients, l.Raw)
		}
		for _, s := range c.Steps {
			cv.Steps = append(cv.Steps, s.Text)
		}
		v.Components = append(v.Components, cv)
	}
	b, err := json.Marshal(v)
	if err != nil {
		// Plain strings and slices always marshal; reaching here is a bug.
		panic(fmt.Sprintf("gemini: marshal draft summary: %v", err))
	}
	return string(b)
}

// responseSchema is ingest.Structured as a Gemini response schema. Only the
// recipe name and the components with their ingredients are required: a source
// may have no author, times or steps.
func responseSchema() *genai.Schema {
	str := func(desc string) *genai.Schema { return &genai.Schema{Type: genai.TypeString, Description: desc} }
	list := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeArray, Description: desc, Items: &genai.Schema{Type: genai.TypeString}}
	}
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name":        str("Recipe title."),
			"author":      str("Author, when the source names one."),
			"description": str("Short description, when the source has one."),
			"yield":       str("Yield as written, such as '4 servings'."),
			"prep_time":   str("Preparation time as written."),
			"cook_time":   str("Cooking time as written."),
			"total_time":  str("Total time as written."),
			"components": {
				Type:        genai.TypeArray,
				Description: "Separately headed parts of the recipe; one unnamed component when there are none.",
				MinItems:    genai.Ptr(int64(1)),
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"name":        str("Heading of this part; empty for the unnamed component."),
						"ingredients": list("Ingredient lines exactly as written."),
						"steps":       list("Method steps in order."),
					},
					Required: []string{"ingredients"},
				},
			},
		},
		Required: []string{"name", "components"},
	}
}

// answerText returns the single text answer, or an error when the model gave
// none: a blocked prompt, no candidate, or a candidate that stopped early.
func answerText(resp *genai.GenerateContentResponse) (string, error) {
	if len(resp.Candidates) == 0 {
		if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
			return "", fmt.Errorf("gemini response: %w: prompt blocked (%s)", ErrInvalidResponse, resp.PromptFeedback.BlockReason)
		}
		return "", fmt.Errorf("gemini response: %w: no candidates", ErrInvalidResponse)
	}
	c := resp.Candidates[0]
	if c.FinishReason != "" && c.FinishReason != genai.FinishReasonStop {
		return "", fmt.Errorf("gemini response: %w: finish reason %s", ErrInvalidResponse, c.FinishReason)
	}
	if c.Content == nil {
		return "", fmt.Errorf("gemini response: %w: empty candidate", ErrInvalidResponse)
	}
	var b strings.Builder
	for _, part := range c.Content.Parts {
		if part.Thought {
			continue
		}
		b.WriteString(part.Text)
	}
	return b.String(), nil
}

// decode reads the model's JSON through the shared strict decoder.
func decode(text string) (ingest.Structured, error) {
	out, err := ingest.DecodeStructured([]byte(text))
	if err != nil {
		return ingest.Structured{}, fmt.Errorf("gemini response: %w", err)
	}
	return out, nil
}
