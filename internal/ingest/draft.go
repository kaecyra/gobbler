package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// Source is the ingested input, kept so it can be stored as the recipe's
// snapshot, byte for byte as it arrived (ADR-00004). The pipeline sets it and
// nothing edits it: Confirm refuses a draft whose Content no longer matches
// SHA256.
type Source struct {
	Kind store.SnapshotKind
	// URL is where a page was finally served from; empty for pasted text.
	URL     string
	Content []byte
	SHA256  string
}

func newSource(kind store.SnapshotKind, url string, content []byte) Source {
	sum := sha256.Sum256(content)
	return Source{Kind: kind, URL: url, Content: content, SHA256: hex.EncodeToString(sum[:])}
}

// intact reports whether Content is still what was ingested.
func (s Source) intact() bool {
	sum := sha256.Sum256(s.Content)
	return s.SHA256 == hex.EncodeToString(sum[:])
}

// Field is one value of a draft with the confidence the rules have in it, 0 to
// 1. A field the source did not state has the zero Value and zero Confidence.
type Field[T any] struct {
	Value      T
	Confidence float64
}

// Line is one ingredient line of a draft. Ingredient is zero when the name did
// not match the catalog; the line is then covered by a Proposed entry.
type Line struct {
	// Raw is the line as written in the source.
	Raw        string
	Name       string
	Ingredient recipe.IngredientID
	Amount     quantity.Amount
	// ToTaste marks "salt to taste": no quantity, never scaled.
	ToTaste    bool
	Optional   bool
	Qualifiers []recipe.Qualifier
	// Confidence is 0 to 1; Issues says why it is below 1.
	Confidence float64
	Issues     []string
}

// Step is one instruction of a draft. Duration is zero when unknown.
type Step struct {
	Text       string
	Duration   time.Duration
	Confidence float64
}

// Component is an ordered part of a draft recipe; Name is empty for the single
// unnamed component.
type Component struct {
	Name  string
	Lines []Line
	Steps []Step
}

// ProposedIngredient is an ingredient the catalog does not have, proposed from
// an unmatched line. It is created, with reviewed false, only when a confirmed
// draft is saved (ADR-00008); ingesting creates nothing.
type ProposedIngredient struct {
	Name string
	// Aisle is where it will be filed until someone reviews it.
	Aisle string
}

// ProposedUnit is a unit the catalog does not have. Like a proposed
// ingredient, it is created unreviewed on confirmed save.
type ProposedUnit struct {
	Name string
}

// Draft is the rule-based reading of one input, shown on the review screen.
// It carries a confidence for every field and line and an overall score.
// Nothing here is stored until it is confirmed (Confirm) and saved.
type Draft struct {
	// ParserVersion is the ParserVersion that produced the draft.
	ParserVersion string
	Source        Source
	// Method says how the recipe was found: JSON-LD, microdata, a site
	// extractor or text sectioning.
	Method extract.Method

	Name        Field[string]
	Author      Field[string]
	Description Field[string]
	Yield       Field[recipe.Yield]
	Servings    Field[quantity.Quantity]
	Calories    Field[int]
	Prep        Field[time.Duration]
	Cook        Field[time.Duration]
	Total       Field[time.Duration]

	Components []Component

	// Overall is 0 to 1. Threshold is the configured threshold the draft was
	// judged against and BelowThreshold is Overall < Threshold: the signal for
	// the LLM fallback.
	Overall        float64
	Threshold      float64
	BelowThreshold bool

	// Issues says in words why Overall is below 1, for the review screen.
	Issues []string

	Proposed      []ProposedIngredient
	ProposedUnits []ProposedUnit
}
