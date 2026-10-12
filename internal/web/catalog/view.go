package catalog

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	domain "github.com/kaecyra/gobbler/internal/catalog"
)

// Form fields that can carry a problem. fieldForm is the whole form.
const (
	fieldForm    = "form"
	fieldName    = "name"
	fieldAisle   = "aisle"
	fieldDensity = "density"
	fieldUSDA    = "usda"
)

const (
	nameMessage      = "Enter a name for the ingredient."
	aisleMessage     = "Enter the aisle this ingredient is found in."
	densityMessage   = "Enter the density as grams per millilitre, a number above zero, for example 0.53."
	usdaMessage      = "Enter the USDA FoodData Central id as a whole number above zero."
	selfMergeMessage = "An ingredient cannot be merged into itself. Choose a different ingredient."
)

// listView is the catalog page: ingredients awaiting review first, then every
// ingredient, both narrowed by the filter.
type listView struct {
	Query      string
	Unreviewed []row
	All        []row
	// Total is the number of ingredients before the filter, so an empty
	// result can say whether the catalog or the filter is what is empty.
	Total int
}

type row struct {
	ID       int64
	Name     string
	Aisle    string
	Aliases  string
	Reviewed bool
}

func newListView(query string, unreviewed, all []domain.Ingredient) listView {
	return listView{
		Query:      query,
		Unreviewed: rowsOf(filter(unreviewed, query)),
		All:        rowsOf(filter(all, query)),
		Total:      len(all),
	}
}

// filter keeps the ingredients whose name, aisle or any alias contains the
// query, ignoring case. An empty query keeps all.
func filter(in []domain.Ingredient, query string) []domain.Ingredient {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return in
	}
	var out []domain.Ingredient
	for _, i := range in {
		if strings.Contains(i.Name, q) || strings.Contains(i.Aisle, q) ||
			slices.ContainsFunc(i.Aliases, func(a string) bool { return strings.Contains(a, q) }) {
			out = append(out, i)
		}
	}
	return out
}

func rowsOf(in []domain.Ingredient) []row {
	out := make([]row, len(in))
	for n, i := range in {
		out[n] = row{ID: i.ID, Name: i.Name, Aisle: i.Aisle, Aliases: strings.Join(i.Aliases, ", "), Reviewed: i.Reviewed}
	}
	return out
}

// formView is the edit form: what the fields hold (as text, so a rejected
// entry comes back as typed) and the problems found.
type formView struct {
	ID       int64
	Name     string
	Aisle    string
	Aliases  string // one per line
	Density  string
	USDAID   string
	Reviewed bool
	Problems map[string]string
	// Aisles are the aisles already in use, offered as suggestions.
	Aisles []string
}

func formFrom(in domain.Ingredient) formView {
	f := formView{ID: in.ID, Name: in.Name, Aisle: in.Aisle, Aliases: strings.Join(in.Aliases, "\n"), Reviewed: in.Reviewed}
	if in.DensityGPerML != nil {
		f.Density = strconv.FormatFloat(*in.DensityGPerML, 'g', -1, 64)
	}
	if in.USDAID != nil {
		f.USDAID = strconv.FormatInt(*in.USDAID, 10)
	}
	return f
}

func (f formView) Problem(field string) string { return f.Problems[field] }

// ingredient builds the ingredient the form describes. A problem found here is
// the page's to report; the catalog checks the rest (aisle, qualifiers,
// collisions) and its verdict is shown inline too.
func (f formView) ingredient() (domain.Ingredient, map[string]string) {
	problems := map[string]string{}
	in := domain.Ingredient{ID: f.ID, Name: f.Name, Aisle: f.Aisle, Reviewed: f.Reviewed}
	if strings.TrimSpace(f.Name) == "" {
		problems[fieldName] = nameMessage
	}
	if strings.TrimSpace(f.Aisle) == "" {
		problems[fieldAisle] = aisleMessage
	}
	for _, a := range strings.Split(f.Aliases, "\n") {
		if a = strings.TrimSpace(a); a != "" {
			in.Aliases = append(in.Aliases, a)
		}
	}
	if s := strings.TrimSpace(f.Density); s != "" {
		d, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(d) || math.IsInf(d, 0) || d <= 0 {
			problems[fieldDensity] = densityMessage
		} else {
			in.DensityGPerML = &d
		}
	}
	if s := strings.TrimSpace(f.USDAID); s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			problems[fieldUSDA] = usdaMessage
		} else {
			in.USDAID = &id
		}
	}
	return in, problems
}

// conflictDetail marks the part of a conflict error that names the colliding
// entry. The rest of the chain can hold driver text, which is not shown.
const conflictDetail = " is already used by "

// usdaColumn is how a UNIQUE violation names the USDA id column in the
// catalog's error chain; it tells a USDA id clash from a name or alias clash.
const usdaColumn = "usda_fdc_id"

const usdaTakenMessage = "That USDA FoodData Central id already belongs to another ingredient."

// inlineMessage returns the field and text to show for an error the catalog
// raised about the submitted values. ok is false for any other error.
func inlineMessage(err error) (field, msg string, ok bool) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		return fieldForm, err.Error(), true
	case errors.Is(err, domain.ErrConflict):
		if text := err.Error(); strings.Contains(text, conflictDetail) {
			return fieldForm, text, true
		} else if strings.Contains(text, usdaColumn) {
			return fieldUSDA, usdaTakenMessage, true
		}
		return fieldForm, domain.ErrConflict.Error() + ". Choose a different name or alias.", true
	}
	return "", "", false
}

func aislesOf(all []domain.Ingredient) []string {
	var out []string
	for _, i := range all {
		out = append(out, i.Aisle)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// chooserView is the first merge step: pick the ingredient to keep.
type chooserView struct {
	Source  domain.Ingredient
	Targets []domain.Ingredient
	Problem string
}

func newChooserView(src domain.Ingredient, all []domain.Ingredient, problem string) chooserView {
	v := chooserView{Source: src, Problem: problem}
	for _, i := range all {
		if i.ID != src.ID {
			v.Targets = append(v.Targets, i)
		}
	}
	return v
}

// confirmView is the second merge step: what the merge will do.
type confirmView struct {
	Source, Target domain.Ingredient
	// Lines is the number of recipe lines the merge re-points, as counted by
	// the catalog.
	Lines int64
}

// formFromRequest reads the edit form of ingredient id from a parsed request.
func formFromRequest(id int64, r *http.Request) formView {
	return formView{
		ID:       id,
		Name:     r.PostForm.Get("name"),
		Aisle:    r.PostForm.Get("aisle"),
		Aliases:  r.PostForm.Get("aliases"),
		Density:  r.PostForm.Get("density"),
		USDAID:   r.PostForm.Get("usda_id"),
		Reviewed: r.PostForm.Get("reviewed") != "",
	}
}
