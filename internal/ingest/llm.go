package ingest

import "context"

// Provider is the LLM provider this package consumes (ADR-00007): given the
// source and the rule-based draft, it returns the recipe in the ingestion
// schema. Each provider (Claude, Gemini) implements it in its own package with
// its own SDK; this package never imports one. Implementations apply the
// configured timeout and model, validate the response against the schema and
// return an error for an invalid one, never a partial Structured.
type Provider interface {
	Structure(ctx context.Context, req Request) (Structured, error)
}

// Request is what a provider is asked to structure.
type Request struct {
	// Text is the source text: pasted text, a page's main text or OCR output.
	Text string
	// Image is the source image for a vision-capable model, empty for text
	// input. ImageMIME is its media type.
	Image     []byte
	ImageMIME string
	// RuleDraft is the rule-based result the provider should improve on.
	RuleDraft Draft
}

// Structured is the ingestion schema a provider fills: the same raw shape the
// rule-based path reads, before ingredient lines are parsed. Time fields and
// ingredient lines are strings as written, so a provider answer goes through
// the same line parser and scoring as any other input.
type Structured struct {
	Name        string                `json:"name"`
	Author      string                `json:"author,omitempty"`
	Description string                `json:"description,omitempty"`
	Yield       string                `json:"yield,omitempty"`
	PrepTime    string                `json:"prep_time,omitempty"`
	CookTime    string                `json:"cook_time,omitempty"`
	TotalTime   string                `json:"total_time,omitempty"`
	Components  []StructuredComponent `json:"components"`
}

// StructuredComponent is one named part of a provider's recipe; Name is empty
// for the unnamed component.
type StructuredComponent struct {
	Name        string   `json:"name,omitempty"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}
