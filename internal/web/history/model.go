package history

import (
	"context"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
)

// timeLayout is how a revision time is shown; the store keeps UTC.
const timeLayout = "2006-01-02 15:04 UTC"

// listView is the history page: the current version, then the saved earlier
// versions newest first.
type listView struct {
	ID      int64
	Name    string
	Current currentRow
	Rows    []revisionRow
	Origin  *originView
}

type currentRow struct {
	Version int64
	When    string
	Who     string
}

type revisionRow struct {
	Version int64
	// When and Who are the time and author of the edit that replaced this
	// version.
	When, Who string
	// Restored is the version whose restore replaced this one, 0 when an
	// ordinary edit did.
	Restored int64
}

// originView says what a clone was copied from.
type originView struct {
	RecipeID int64
	Version  int64
}

func buildList(id int64, name string, info store.Info, revs []store.Revision) listView {
	v := listView{
		ID: id, Name: name,
		Current: currentRow{Version: info.Version, When: info.UpdatedAt.UTC().Format(timeLayout), Who: info.UpdatedBy},
	}
	if info.ClonedFrom != nil {
		v.Origin = &originView{RecipeID: info.ClonedFrom.RecipeID, Version: info.ClonedFrom.Version}
	}
	for _, r := range revs {
		row := revisionRow{Version: r.Version, When: r.CreatedAt.UTC().Format(timeLayout), Who: r.Author}
		if r.Restored != nil {
			row.Restored = *r.Restored
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

// versionView is the read-only document of one version, already formatted.
type versionView struct {
	ID          int64
	Version     int64
	Current     bool
	Name        string
	Description string
	Author      string
	SourceURL   string
	Tags        []string
	Rating      *int
	Calories    *int
	Servings    string
	Yield       string

	Times []timeRow
	Total string
	// TotalNote says where the total came from, so the page shows which rule
	// produced it (ADR-00004).
	TotalNote  string
	Components []componentView
	Equipment  []string
}

type timeRow struct{ Label, Text string }

type componentView struct {
	Name      string
	Lines     []string
	Steps     []stepView
	FirstStep int
}

type stepView struct {
	Text string
	// Note is the duration and the passive marker.
	Note string
	// After lists the numbers of the steps this one waits for.
	After []string
}

// buildVersion formats doc for display. Quantities are shown as stored: a past
// version is a record, so there is no scaling or conversion here.
func buildVersion(ctx context.Context, cat Catalog, id, version, currentVersion int64, doc recipe.Recipe) (versionView, error) {
	v := versionView{
		ID: id, Version: version, Current: version == currentVersion,
		Name: doc.Name, Description: doc.Description, Author: doc.Author, SourceURL: doc.SourceURL,
		Tags: doc.Tags, Rating: doc.Rating, Calories: doc.CaloriesPerServing,
		Servings: doc.Servings.String(),
		Yield:    strings.TrimSpace(doc.Yield.Qty.String() + " " + doc.Yield.Unit),
	}
	times, err := display.BuildTimes(doc)
	if err != nil {
		return versionView{}, err
	}
	for _, r := range times.Rows {
		v.Times = append(v.Times, timeRow{Label: r.Label, Text: r.Text})
	}
	v.Total, v.TotalNote = times.Total, times.TotalNote

	number := display.StepNumbers(doc)
	lines := display.NewLineBuilder(cat, display.Default())
	for _, c := range doc.Components {
		cv := componentView{Name: c.Name}
		if len(c.Steps) > 0 {
			cv.FirstStep = number[c.Steps[0].ID]
		}
		for _, l := range c.Lines {
			line, err := lines.Build(ctx, l)
			if err != nil {
				return versionView{}, err
			}
			cv.Lines = append(cv.Lines, line.Text)
		}
		for _, s := range c.Steps {
			sv := stepView{Text: s.Text, Note: display.StepNote(s)}
			for _, dep := range s.DependsOn {
				sv.After = append(sv.After, strconv.Itoa(number[dep]))
			}
			cv.Steps = append(cv.Steps, sv)
		}
		v.Components = append(v.Components, cv)
	}

	for _, link := range doc.Equipment {
		text, err := display.EquipmentText(ctx, cat, link)
		if err != nil {
			return versionView{}, err
		}
		if len(link.Steps) > 0 {
			var used []string
			for _, s := range link.Steps {
				used = append(used, strconv.Itoa(number[s]))
			}
			text += " (steps " + strings.Join(used, ", ") + ")"
		}
		v.Equipment = append(v.Equipment, text)
	}
	return v, nil
}

func joinSteps(nums []string) string { return strings.Join(nums, ", ") }

// hasLines reports whether any component has an ingredient line, so a version
// of steps alone shows no empty Ingredients heading.
func (v versionView) hasLines() bool {
	for _, c := range v.Components {
		if len(c.Lines) > 0 {
			return true
		}
	}
	return false
}
