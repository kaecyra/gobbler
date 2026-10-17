package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// The revision document is the recipe in a JSON form of its own, not a
// marshalled recipe.Recipe: quantities hold unexported rationals, and the
// stored form must not change when the domain struct does. Quantities use the
// column encoding, units are catalog ids (as on live lines, so a unit rename
// leaves old revisions readable), durations are nanoseconds.

type docQualifier struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type docLine struct {
	Qty           *string        `json:"qty,omitempty"`
	UnitID        int64          `json:"unit_id,omitempty"`
	HasPackage    bool           `json:"has_package,omitempty"`
	PackageQty    *string        `json:"package_qty,omitempty"`
	PackageUnitID int64          `json:"package_unit_id,omitempty"`
	Ingredient    int64          `json:"ingredient,omitempty"`
	Qualifiers    []docQualifier `json:"qualifiers,omitempty"`
	Optional      bool           `json:"optional,omitempty"`
	Rule          string         `json:"rule"`
	Raw           string         `json:"raw,omitempty"`
}

type docStep struct {
	ID         string   `json:"id"`
	Text       string   `json:"text,omitempty"`
	DurationNS int64    `json:"duration_ns,omitempty"`
	Passive    bool     `json:"passive,omitempty"`
	Title      string   `json:"title,omitempty"`
	Kind       string   `json:"stage_kind,omitempty"`
	DependsOn  []string `json:"depends_on,omitempty"`
}

type docComponent struct {
	Name  string    `json:"name,omitempty"`
	Lines []docLine `json:"lines,omitempty"`
	Steps []docStep `json:"steps,omitempty"`
}

type docEquipment struct {
	Equipment int64    `json:"equipment"`
	Qty       *string  `json:"qty,omitempty"`
	Steps     []string `json:"steps,omitempty"`
}

type docSpecial struct {
	Label      string `json:"label"`
	DurationNS int64  `json:"duration_ns"`
}

type docRecipe struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	SourceURL   string         `json:"source_url,omitempty"`
	Author      string         `json:"author,omitempty"`
	VideoID     string         `json:"video_id,omitempty"`
	MealType    string         `json:"meal_type,omitempty"`
	SpiceLevel  *int           `json:"spice_level,omitempty"`
	YieldQty    *string        `json:"yield_qty,omitempty"`
	YieldUnit   string         `json:"yield_unit,omitempty"`
	Servings    *string        `json:"servings,omitempty"`
	Calories    *int           `json:"calories_per_serving,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Rating      *int           `json:"rating,omitempty"`
	PrepNS      int64          `json:"prep_ns,omitempty"`
	CookNS      int64          `json:"cook_ns,omitempty"`
	TotalNS     int64          `json:"total_ns,omitempty"`
	Special     []docSpecial   `json:"special_times,omitempty"`
	Components  []docComponent `json:"components"`
	Equipment   []docEquipment `json:"equipment,omitempty"`
}

// encodeDocument renders r as revision document JSON.
func encodeDocument(r recipe.Recipe, units *unitTable) ([]byte, error) {
	d := docRecipe{
		Name: r.Name, Description: r.Description, SourceURL: r.SourceURL, Author: r.Author, VideoID: r.VideoID,
		MealType: string(r.MealType), SpiceLevel: r.SpiceLevel, YieldUnit: r.Yield.Unit, Calories: r.CaloriesPerServing, Tags: r.Tags, Rating: r.Rating,
		PrepNS: int64(r.Times.Prep), CookNS: int64(r.Times.Cook), TotalNS: int64(r.Times.Total),
	}
	var err error
	if d.YieldQty, err = encodeQty(r.Yield.Qty); err != nil {
		return nil, fmt.Errorf("yield: %w", err)
	}
	if d.Servings, err = encodeQty(r.Servings); err != nil {
		return nil, fmt.Errorf("servings: %w", err)
	}
	for _, st := range r.Times.Special {
		d.Special = append(d.Special, docSpecial{Label: st.Label, DurationNS: int64(st.Duration)})
	}
	for ci, c := range r.Components {
		dc := docComponent{Name: c.Name}
		for li, l := range c.Lines {
			dl, err := encodeLine(l, units)
			if err != nil {
				return nil, fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
			dc.Lines = append(dc.Lines, dl)
		}
		for _, s := range c.Steps {
			ds := docStep{ID: string(s.ID), Text: s.Text, DurationNS: int64(s.Duration), Passive: s.Passive, Title: s.Title, Kind: string(s.Kind)}
			for _, dep := range s.DependsOn {
				ds.DependsOn = append(ds.DependsOn, string(dep))
			}
			dc.Steps = append(dc.Steps, ds)
		}
		d.Components = append(d.Components, dc)
	}
	for ei, e := range r.Equipment {
		de := docEquipment{Equipment: int64(e.Equipment)}
		if de.Qty, err = encodeQty(e.Qty); err != nil {
			return nil, fmt.Errorf("equipment %d: %w", ei+1, err)
		}
		for _, id := range e.Steps {
			de.Steps = append(de.Steps, string(id))
		}
		d.Equipment = append(d.Equipment, de)
	}
	return encodeRaw(d)
}

func encodeRaw(d docRecipe) ([]byte, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("encode revision document: %w", err)
	}
	return b, nil
}

func encodeLine(l recipe.IngredientLine, units *unitTable) (docLine, error) {
	dl := docLine{
		HasPackage: l.Amount.HasPackage, Ingredient: int64(l.Ingredient),
		Optional: l.Optional, Rule: string(l.Rule), Raw: l.Raw,
	}
	var err error
	if dl.Qty, err = encodeQty(l.Amount.Qty); err != nil {
		return docLine{}, err
	}
	if dl.UnitID, err = units.refID(l.Amount.Unit); err != nil {
		return docLine{}, err
	}
	if l.Amount.HasPackage {
		if dl.PackageUnitID, err = units.refID(l.Amount.Package.Unit); err != nil {
			return docLine{}, err
		}
		if dl.PackageQty, err = encodeQty(l.Amount.Package.Qty); err != nil {
			return docLine{}, fmt.Errorf("package: %w", err)
		}
	}
	for _, q := range l.Qualifiers {
		dl.Qualifiers = append(dl.Qualifiers, docQualifier{Kind: string(q.Kind), Text: q.Text})
	}
	return dl, nil
}

// decodeDocument parses revision document JSON without resolving units.
func decodeDocument(b []byte) (docRecipe, error) {
	var d docRecipe
	if err := json.Unmarshal(b, &d); err != nil {
		return docRecipe{}, fmt.Errorf("decode revision document: %w", err)
	}
	return d, nil
}

// ingredientIDs lists the distinct ingredient ids a document references.
func (d docRecipe) ingredientIDs() []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, c := range d.Components {
		for _, l := range c.Lines {
			if l.Ingredient != 0 && !seen[l.Ingredient] {
				seen[l.Ingredient] = true
				ids = append(ids, l.Ingredient)
			}
		}
	}
	return ids
}

// repointIngredient rewrites references to loser as winner.
func (d *docRecipe) repointIngredient(loser, winner int64) {
	for ci := range d.Components {
		for li := range d.Components[ci].Lines {
			if l := &d.Components[ci].Lines[li]; l.Ingredient == loser {
				l.Ingredient = winner
			}
		}
	}
}

// toRecipe rebuilds the domain document, resolving unit names against the
// catalog.
func (d docRecipe) toRecipe(units *unitTable) (recipe.Recipe, error) {
	r := recipe.Recipe{
		Name: d.Name, Description: d.Description, SourceURL: d.SourceURL, Author: d.Author, VideoID: d.VideoID,
		MealType: recipe.MealType(d.MealType), SpiceLevel: d.SpiceLevel,
		Yield:              recipe.Yield{Unit: d.YieldUnit},
		CaloriesPerServing: d.Calories, Tags: d.Tags, Rating: d.Rating,
		Times: recipe.Times{Prep: time.Duration(d.PrepNS), Cook: time.Duration(d.CookNS), Total: time.Duration(d.TotalNS)},
	}
	var err error
	if r.Yield.Qty, err = decodeQty(d.YieldQty); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Servings, err = decodeQty(d.Servings); err != nil {
		return recipe.Recipe{}, err
	}
	for _, st := range d.Special {
		r.Times.Special = append(r.Times.Special, recipe.SpecialTime{Label: st.Label, Duration: time.Duration(st.DurationNS)})
	}
	for _, dc := range d.Components {
		c := recipe.Component{Name: dc.Name}
		for _, dl := range dc.Lines {
			l, err := dl.line(units)
			if err != nil {
				return recipe.Recipe{}, err
			}
			c.Lines = append(c.Lines, l)
		}
		for _, ds := range dc.Steps {
			s := recipe.Step{ID: recipe.StepID(ds.ID), Text: ds.Text, Duration: time.Duration(ds.DurationNS), Passive: ds.Passive, Title: ds.Title, Kind: recipe.StageKind(ds.Kind)}
			for _, dep := range ds.DependsOn {
				s.DependsOn = append(s.DependsOn, recipe.StepID(dep))
			}
			c.Steps = append(c.Steps, s)
		}
		r.Components = append(r.Components, c)
	}
	for _, de := range d.Equipment {
		e := recipe.EquipmentLink{Equipment: recipe.EquipmentID(de.Equipment)}
		if e.Qty, err = decodeQty(de.Qty); err != nil {
			return recipe.Recipe{}, err
		}
		for _, id := range de.Steps {
			e.Steps = append(e.Steps, recipe.StepID(id))
		}
		r.Equipment = append(r.Equipment, e)
	}
	return r, nil
}

func (dl docLine) line(units *unitTable) (recipe.IngredientLine, error) {
	l := recipe.IngredientLine{
		Ingredient: recipe.IngredientID(dl.Ingredient), Optional: dl.Optional,
		Rule: quantity.Rule(dl.Rule), Raw: dl.Raw,
	}
	var err error
	if l.Amount.Qty, err = decodeQty(dl.Qty); err != nil {
		return recipe.IngredientLine{}, err
	}
	if l.Amount.Unit, err = units.byRef(dl.UnitID); err != nil {
		return recipe.IngredientLine{}, err
	}
	if dl.HasPackage {
		l.Amount.HasPackage = true
		if l.Amount.Package.Qty, err = decodeQty(dl.PackageQty); err != nil {
			return recipe.IngredientLine{}, err
		}
		if l.Amount.Package.Unit, err = units.byRef(dl.PackageUnitID); err != nil {
			return recipe.IngredientLine{}, err
		}
	}
	for _, q := range dl.Qualifiers {
		l.Qualifiers = append(l.Qualifiers, recipe.Qualifier{Kind: recipe.QualifierKind(q.Kind), Text: q.Text})
	}
	return l, nil
}
