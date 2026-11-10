package ingest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// The draft document is a draft in a JSON form of its own, as the revision
// document is for a recipe: quantities hold unexported rationals, and the
// stored form must not change when the domain struct does. It is what a
// durable draft is stored as (ADR-00035). It holds everything a draft is except
// the source's kind and content, which are the draft's source snapshot (written
// once, so a draft's source never changes), and the photo
// behind an OCR source, which is never stored.
//
// The store reads one thing of this document, the catalog ingredient ids, and
// finds them at components[].lines[].ingredient (store/proposal_doc.go). The
// field is named for that reason; TestDraftDocumentIngredientsSurviveMerge
// holds this end of the contract.

type docField[T any] struct {
	V   T       `json:"v"`
	C   float64 `json:"c,omitempty"`
	LLM bool    `json:"llm,omitempty"`
}

type docUnit struct {
	Name      string `json:"name"`
	System    string `json:"system,omitempty"`
	Dimension string `json:"dimension,omitempty"`
	Factor    string `json:"factor,omitempty"`
}

type docAmount struct {
	Qty        *string  `json:"qty,omitempty"`
	Unit       *docUnit `json:"unit,omitempty"`
	HasPackage bool     `json:"has_package,omitempty"`
	PackageQty *string  `json:"package_qty,omitempty"`
	PackageU   *docUnit `json:"package_unit,omitempty"`
}

type docQualifier struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type docLine struct {
	Raw        string         `json:"raw,omitempty"`
	Name       string         `json:"name,omitempty"`
	Ingredient int64          `json:"ingredient,omitempty"`
	Amount     docAmount      `json:"amount"`
	ToTaste    bool           `json:"to_taste,omitempty"`
	Optional   bool           `json:"optional,omitempty"`
	Qualifiers []docQualifier `json:"qualifiers,omitempty"`
	Confidence float64        `json:"confidence"`
	Issues     []string       `json:"issues,omitempty"`
	FromLLM    bool           `json:"from_llm,omitempty"`
}

type docStep struct {
	Text         string  `json:"text,omitempty"`
	DurationNS   int64   `json:"duration_ns,omitempty"`
	Confidence   float64 `json:"confidence"`
	FromLLM      bool    `json:"from_llm,omitempty"`
	Passive      bool    `json:"passive,omitempty"`
	DependsOn    []int   `json:"depends_on,omitempty"`
	GraphCue     bool    `json:"graph_cue,omitempty"`
	GraphFromLLM bool    `json:"graph_from_llm,omitempty"`
	Kind         string  `json:"stage_kind,omitempty"`
	Title        string  `json:"title,omitempty"`
	GraphByUser  bool    `json:"graph_by_user,omitempty"`
}

type docAuthorNote struct {
	Title      string   `json:"title,omitempty"`
	Text       string   `json:"text"`
	Confidence float64  `json:"confidence"`
	Issues     []string `json:"issues,omitempty"`
	FromLLM    bool     `json:"from_llm,omitempty"`
}

type docComponent struct {
	Name    string    `json:"name,omitempty"`
	Lines   []docLine `json:"lines,omitempty"`
	Steps   []docStep `json:"steps,omitempty"`
	FromLLM bool      `json:"from_llm,omitempty"`
}

type docEquipment struct {
	Name      string `json:"name"`
	Equipment int64  `json:"equipment,omitempty"`
	FromLLM   bool   `json:"from_llm,omitempty"`
}

type docYield struct {
	Qty  *string `json:"qty,omitempty"`
	Unit string  `json:"unit,omitempty"`
}

type docReimport struct {
	Recipe      int64 `json:"recipe"`
	BaseVersion int64 `json:"base_version"`
}

type docDraft struct {
	ParserVersion string `json:"parser_version,omitempty"`
	SourceURL     string `json:"source_url,omitempty"`
	Method        string `json:"method,omitempty"`

	Name        docField[string]   `json:"name"`
	Author      docField[string]   `json:"author"`
	Description docField[string]   `json:"description"`
	Yield       docField[docYield] `json:"yield"`
	Servings    docField[*string]  `json:"servings"`
	Calories    docField[int]      `json:"calories"`
	Prep        docField[int64]    `json:"prep"`
	Cook        docField[int64]    `json:"cook"`
	Total       docField[int64]    `json:"total"`

	VideoID  string       `json:"video_id,omitempty"`
	Picture  string       `json:"picture,omitempty"`
	Reimport *docReimport `json:"reimport,omitempty"`
	MealType string       `json:"meal_type,omitempty"`
	// MealTypeLLM and CuisineLLM mark an agent's revision of them (ADR-00048);
	// a document written before them reads as unmarked.
	MealTypeLLM bool   `json:"meal_type_llm,omitempty"`
	CuisineLLM  bool   `json:"cuisine_llm,omitempty"`
	SpiceLevel  *int   `json:"spice_level,omitempty"`
	Cuisine     string `json:"cuisine,omitempty"`

	Components []docComponent `json:"components"`
	Equipment  []docEquipment `json:"equipment,omitempty"`

	AuthorNotes []docAuthorNote `json:"author_notes,omitempty"`

	Overall        float64  `json:"overall"`
	Threshold      float64  `json:"threshold"`
	BelowThreshold bool     `json:"below_threshold,omitempty"`
	UsedLLM        bool     `json:"used_llm,omitempty"`
	LLMError       string   `json:"llm_error,omitempty"`
	Issues         []string `json:"issues,omitempty"`

	Proposed []ProposedIngredient `json:"proposed,omitempty"`
}

func encodeQ(q quantity.Quantity) (*string, error) {
	s, ok, err := quantity.EncodeQty(q)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil //nolint:nilnil // nil text is the document form of "no quantity"
	}
	return &s, nil
}

func decodeQ(s *string) (quantity.Quantity, error) {
	if s == nil {
		return quantity.Absent(), nil
	}
	return quantity.DecodeQty(*s)
}

func encodeUnit(u quantity.Unit) *docUnit {
	if u.IsZero() {
		return nil
	}
	return &docUnit{Name: u.Name, System: string(u.System), Dimension: string(u.Dimension), Factor: quantity.EncodeRat(u.Factor)}
}

func decodeUnit(d *docUnit) (quantity.Unit, error) {
	if d == nil {
		return quantity.Unit{}, nil
	}
	f, err := quantity.ParseRat(d.Factor)
	if err != nil {
		return quantity.Unit{}, fmt.Errorf("unit %q factor: %w", d.Name, err)
	}
	return quantity.Unit{Name: d.Name, System: quantity.System(d.System), Dimension: quantity.Dimension(d.Dimension), Factor: f}, nil
}

// EncodeDraft renders d as the document a durable draft is stored as. The
// source's content and photo are not in it.
func EncodeDraft(d Draft) ([]byte, error) {
	out := docDraft{
		ParserVersion: d.ParserVersion, SourceURL: d.Source.URL, Method: string(d.Method),
		Name:        docField[string]{d.Name.Value, d.Name.Confidence, d.Name.FromLLM},
		Author:      docField[string]{d.Author.Value, d.Author.Confidence, d.Author.FromLLM},
		Description: docField[string]{d.Description.Value, d.Description.Confidence, d.Description.FromLLM},
		Calories:    docField[int]{d.Calories.Value, d.Calories.Confidence, d.Calories.FromLLM},
		Prep:        docField[int64]{int64(d.Prep.Value), d.Prep.Confidence, d.Prep.FromLLM},
		Cook:        docField[int64]{int64(d.Cook.Value), d.Cook.Confidence, d.Cook.FromLLM},
		Total:       docField[int64]{int64(d.Total.Value), d.Total.Confidence, d.Total.FromLLM},
		VideoID:     d.VideoID, Picture: string(d.Picture), MealType: string(d.MealType), SpiceLevel: d.SpiceLevel, Cuisine: string(d.Cuisine),
		MealTypeLLM: d.MealTypeFromLLM, CuisineLLM: d.CuisineFromLLM,
		Overall: d.Overall, Threshold: d.Threshold, BelowThreshold: d.BelowThreshold,
		UsedLLM: d.UsedLLM, LLMError: d.LLMError, Issues: d.Issues, Proposed: d.Proposed,
	}
	if d.Reimport != nil {
		out.Reimport = &docReimport{Recipe: d.Reimport.RecipeID, BaseVersion: d.Reimport.BaseVersion}
	}
	yq, err := encodeQ(d.Yield.Value.Qty)
	if err != nil {
		return nil, fmt.Errorf("yield: %w", err)
	}
	out.Yield = docField[docYield]{docYield{Qty: yq, Unit: d.Yield.Value.Unit}, d.Yield.Confidence, d.Yield.FromLLM}
	sq, err := encodeQ(d.Servings.Value)
	if err != nil {
		return nil, fmt.Errorf("servings: %w", err)
	}
	out.Servings = docField[*string]{sq, d.Servings.Confidence, d.Servings.FromLLM}
	for ci, c := range d.Components {
		dc := docComponent{Name: c.Name, FromLLM: c.FromLLM}
		for li, l := range c.Lines {
			dl, err := encodeLine(l)
			if err != nil {
				return nil, fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
			dc.Lines = append(dc.Lines, dl)
		}
		for _, s := range c.Steps {
			dc.Steps = append(dc.Steps, docStep{
				Text: s.Text, DurationNS: int64(s.Duration), Confidence: s.Confidence, FromLLM: s.FromLLM, Passive: s.Passive,
				DependsOn: s.DependsOn, GraphCue: s.GraphCue, GraphFromLLM: s.GraphFromLLM, Kind: string(s.Kind), Title: s.Title,
				GraphByUser: s.GraphByUser,
			})
		}
		out.Components = append(out.Components, dc)
	}
	for _, e := range d.Equipment {
		out.Equipment = append(out.Equipment, docEquipment{Name: e.Name, Equipment: int64(e.Equipment), FromLLM: e.FromLLM})
	}
	for _, n := range d.AuthorNotes {
		out.AuthorNotes = append(out.AuthorNotes, docAuthorNote(n))
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("encode draft document: %w", err)
	}
	return b, nil
}

func encodeLine(l Line) (docLine, error) {
	dl := docLine{
		Raw: l.Raw, Name: l.Name, Ingredient: int64(l.Ingredient), ToTaste: l.ToTaste, Optional: l.Optional,
		Confidence: l.Confidence, Issues: l.Issues, FromLLM: l.FromLLM,
		Amount: docAmount{Unit: encodeUnit(l.Amount.Unit), HasPackage: l.Amount.HasPackage},
	}
	var err error
	if dl.Amount.Qty, err = encodeQ(l.Amount.Qty); err != nil {
		return docLine{}, err
	}
	if l.Amount.HasPackage {
		if dl.Amount.PackageQty, err = encodeQ(l.Amount.Package.Qty); err != nil {
			return docLine{}, fmt.Errorf("package: %w", err)
		}
		dl.Amount.PackageU = encodeUnit(l.Amount.Package.Unit)
	}
	for _, q := range l.Qualifiers {
		dl.Qualifiers = append(dl.Qualifiers, docQualifier{Kind: string(q.Kind), Text: q.Text})
	}
	return dl, nil
}

// DecodeDraft is the inverse of EncodeDraft. The source's content comes from
// the draft's snapshot; the photo is never stored, so Source.Image is empty.
func DecodeDraft(doc []byte, snap store.Snapshot) (Draft, error) {
	var in docDraft
	if err := json.Unmarshal(doc, &in); err != nil {
		return Draft{}, fmt.Errorf("decode draft document: %w", err)
	}
	d := Draft{
		ParserVersion: in.ParserVersion, Method: extract.Method(in.Method),
		Source: Source{Kind: snap.Kind, URL: in.SourceURL, Content: snap.Content, SHA256: snap.SHA256},
		Name:   Field[string]{in.Name.V, in.Name.C, in.Name.LLM}, Author: Field[string]{in.Author.V, in.Author.C, in.Author.LLM},
		Description: Field[string]{in.Description.V, in.Description.C, in.Description.LLM},
		Calories:    Field[int]{in.Calories.V, in.Calories.C, in.Calories.LLM},
		Prep:        Field[time.Duration]{time.Duration(in.Prep.V), in.Prep.C, in.Prep.LLM},
		Cook:        Field[time.Duration]{time.Duration(in.Cook.V), in.Cook.C, in.Cook.LLM},
		Total:       Field[time.Duration]{time.Duration(in.Total.V), in.Total.C, in.Total.LLM},
		VideoID:     in.VideoID, Picture: PictureChoice(in.Picture), MealType: recipe.MealType(in.MealType), SpiceLevel: in.SpiceLevel, Cuisine: recipe.Cuisine(in.Cuisine),
		MealTypeFromLLM: in.MealTypeLLM, CuisineFromLLM: in.CuisineLLM,
		Overall: in.Overall, Threshold: in.Threshold, BelowThreshold: in.BelowThreshold,
		UsedLLM: in.UsedLLM, LLMError: in.LLMError, Issues: in.Issues, Proposed: in.Proposed,
	}
	if in.Reimport != nil {
		d.Reimport = &Reimport{RecipeID: in.Reimport.Recipe, BaseVersion: in.Reimport.BaseVersion}
	}
	var err error
	yq, err := decodeQ(in.Yield.V.Qty)
	if err != nil {
		return Draft{}, fmt.Errorf("yield: %w", err)
	}
	d.Yield = Field[recipe.Yield]{recipe.Yield{Qty: yq, Unit: in.Yield.V.Unit}, in.Yield.C, in.Yield.LLM}
	sq, err := decodeQ(in.Servings.V)
	if err != nil {
		return Draft{}, fmt.Errorf("servings: %w", err)
	}
	d.Servings = Field[quantity.Quantity]{sq, in.Servings.C, in.Servings.LLM}
	for ci, dc := range in.Components {
		c := Component{Name: dc.Name, FromLLM: dc.FromLLM}
		for li, dl := range dc.Lines {
			l, err := decodeLine(dl)
			if err != nil {
				return Draft{}, fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
			c.Lines = append(c.Lines, l)
		}
		for _, ds := range dc.Steps {
			c.Steps = append(c.Steps, Step{
				Text: ds.Text, Duration: time.Duration(ds.DurationNS), Confidence: ds.Confidence, FromLLM: ds.FromLLM, Passive: ds.Passive,
				DependsOn: ds.DependsOn, GraphCue: ds.GraphCue, GraphFromLLM: ds.GraphFromLLM, Kind: recipe.StageKind(ds.Kind), Title: ds.Title,
				GraphByUser: ds.GraphByUser,
			})
		}
		d.Components = append(d.Components, c)
	}
	for _, de := range in.Equipment {
		d.Equipment = append(d.Equipment, Equipment{Name: de.Name, Equipment: recipe.EquipmentID(de.Equipment), FromLLM: de.FromLLM})
	}
	for _, dn := range in.AuthorNotes {
		d.AuthorNotes = append(d.AuthorNotes, AuthorNote(dn))
	}
	return d, nil
}

func decodeLine(dl docLine) (Line, error) {
	l := Line{
		Raw: dl.Raw, Name: dl.Name, Ingredient: recipe.IngredientID(dl.Ingredient), ToTaste: dl.ToTaste, Optional: dl.Optional,
		Confidence: dl.Confidence, Issues: dl.Issues, FromLLM: dl.FromLLM,
	}
	var err error
	if l.Amount.Qty, err = decodeQ(dl.Amount.Qty); err != nil {
		return Line{}, err
	}
	if l.Amount.Unit, err = decodeUnit(dl.Amount.Unit); err != nil {
		return Line{}, err
	}
	if dl.Amount.HasPackage {
		l.Amount.HasPackage = true
		if l.Amount.Package.Qty, err = decodeQ(dl.Amount.PackageQty); err != nil {
			return Line{}, fmt.Errorf("package: %w", err)
		}
		if l.Amount.Package.Unit, err = decodeUnit(dl.Amount.PackageU); err != nil {
			return Line{}, err
		}
	}
	for _, q := range dl.Qualifiers {
		l.Qualifiers = append(l.Qualifiers, recipe.Qualifier{Kind: recipe.QualifierKind(q.Kind), Text: q.Text})
	}
	return l, nil
}
