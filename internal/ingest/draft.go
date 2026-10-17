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
	// Image is the uploaded photo behind an OCR source, kept in memory only so
	// a vision-capable provider can be shown the page itself (ADR-00006). It is
	// never part of the snapshot: Content is the OCR text, and nothing here is
	// stored. ImageMIME is its media type.
	Image     []byte
	ImageMIME string
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
	// FromLLM marks a value the LLM supplied rather than the rules read
	// (ADR-00006 obligation 4).
	FromLLM bool
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
	// FromLLM marks a line the LLM supplied.
	FromLLM bool
}

// Step is one instruction of a draft. Duration is zero when unknown.
//
// Steps are numbered from 1 in reading order across the whole draft, through
// every component; Save gives the step with number n the id "s<n>". DependsOn
// uses those numbers (ADR-00020 decision 1).
type Step struct {
	Text       string
	Duration   time.Duration
	Confidence float64
	// FromLLM marks a step the LLM supplied.
	FromLLM bool
	// Passive marks a step the cook can leave unattended (baking, resting).
	Passive bool
	// DependsOn lists the numbers of the steps that must finish before this one
	// starts.
	DependsOn []int
	// GraphCue is true when a rule matched this step's text (a passive word or a
	// parallel cue), so Passive and DependsOn are the rules' reading. A step
	// with none of GraphCue, GraphFromLLM and GraphByUser has the default
	// graph: a chain from the step before it.
	GraphCue bool
	// GraphFromLLM marks a step whose Passive and DependsOn the LLM supplied.
	GraphFromLLM bool
	// Kind is what the step is doing, for the time ribbon (ADR-00022): proposed
	// by rule from the text where the text is unambiguous, else empty. Empty is
	// unknown, never an error (ADR-00006).
	Kind recipe.StageKind
	// Title is a short heading the source gave the step, empty when it had none.
	Title string
	// GraphByUser marks a step whose Passive and DependsOn the user set on the
	// review screen. It is settled: a retry with the LLM does not ask about it.
	GraphByUser bool
}

// Component is an ordered part of a draft recipe; Name is empty for the single
// unnamed component.
type Component struct {
	Name  string
	Lines []Line
	Steps []Step
	// FromLLM marks a component the LLM supplied.
	FromLLM bool
}

// ProposedIngredient is an ingredient the catalog does not have, proposed from
// an unmatched line. It is created, with reviewed false, only when a confirmed
// draft is saved (ADR-00008); ingesting creates nothing.
type ProposedIngredient struct {
	Name string
	// Aisle is where it will be filed until someone reviews it.
	Aisle string
}

// Equipment is one piece of equipment a draft found: named by the source's own
// tool list, by the LLM, or by a catalog name or alias in the recipe text.
// Equipment is zero when the catalog does not have it; it is then covered by a
// ProposedEquipment entry.
type Equipment struct {
	// Name is the catalog's name when matched, else the name as read, trimmed,
	// lower-case and with its qualifier ("(optional)") dropped.
	Name      string
	Equipment recipe.EquipmentID
	// FromLLM marks equipment the LLM supplied.
	FromLLM bool
}

// ProposedEquipment is equipment the catalog does not have, proposed from the
// source's tool list or the LLM's answer. It is created, with reviewed false,
// only when a confirmed draft is saved (ADR-00008); ingesting creates nothing.
type ProposedEquipment struct {
	Name string
}

// Draft is the reading of one input shown on the review screen: the rules'
// reading, with the gaps the LLM filled marked FromLLM when UsedLLM is set.
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

	// VideoID is the YouTube video the page names in its schema.org markup,
	// the 11-character ID (ADR-00021 decision 5); empty when there is none or
	// it is on another host. Only the rules read it, so it carries no
	// confidence: the review screen shows it and the user may change it.
	VideoID string

	// MealType is the recipe's meal type when the source's category names one
	// unambiguously, else empty. SpiceLevel is nil: ingest never sets it, but
	// the review screen may, and a confirmed draft keeps it (ADR-00022).
	MealType   recipe.MealType
	SpiceLevel *int

	Components []Component

	// Equipment is what the recipe uses, in reading order, once each.
	Equipment []Equipment

	// Overall is 0 to 1. Threshold is the configured threshold the draft was
	// judged against and BelowThreshold is Overall < Threshold: the signal for
	// the LLM fallback.
	Overall        float64
	Threshold      float64
	BelowThreshold bool

	// UsedLLM is true when the LLM filled the gaps in the rule-based reading
	// (ADR-00020 decision 2): the values it supplied carry FromLLM, and every
	// other value is the rules'. LLMError is why an LLM attempt failed and the
	// rule-based draft was kept; it is an error, not a low score, and never
	// blocks saving.
	UsedLLM  bool
	LLMError string

	// Issues says in words why Overall is below 1, for the review screen.
	Issues []string

	Proposed []ProposedIngredient
}

// ProposedEquipment lists what confirming d would add to the catalog: the
// equipment the catalog lacks, in order.
func (d Draft) ProposedEquipment() []ProposedEquipment {
	var out []ProposedEquipment
	for _, e := range d.Equipment {
		if e.Equipment == 0 {
			out = append(out, ProposedEquipment{Name: e.Name})
		}
	}
	return out
}
