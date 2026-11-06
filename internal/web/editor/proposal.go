package editor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// fieldProposal is the form field, and the query parameter that opens the
// editor on a proposal: the token of the pending proposal being reviewed
// (ADR-00035). Saving the form with it set accepts the proposal.
const fieldProposal = "proposal"

// proposalStaleMessage is shown when a proposal's recipe was changed after the
// proposal was made, so it cannot be saved.
const proposalStaleMessage = "This proposal is out of date: the recipe was changed after it was made, so nothing was saved. " +
	"Reject it from the recipe page, or ask for a new one."

// proposalView is the summary above the form of a proposal under review.
type proposalView struct {
	Token string
	// By says who proposed it, and Reason why, if they said.
	By, Reason string
	// Changes are the differences from the recipe as it is now, one line each.
	Changes []string
}

// openProposal loads the pending edit proposal token for recipe id, which is at
// version. A proposal that is not one of this recipe's, is no longer pending or
// is stale is refused with a message to show, and a non-nil error is a failure
// to read it.
func (h *handler) openProposal(ctx context.Context, id, version int64, token string) (p apirecipes.Proposal, refusal string, err error) {
	p, err = h.Recipes.GetProposal(ctx, caller.FromContext(ctx), token)
	if err != nil {
		return apirecipes.Proposal{}, "", fmt.Errorf("load proposal: %w", err)
	}
	if p.Kind != apirecipes.ProposalEdit || p.RecipeID != id {
		return apirecipes.Proposal{}, "", fmt.Errorf("proposal %s is not a proposal for recipe %d: %w", token, id, layout.ErrNotFound)
	}
	switch {
	case p.State != apirecipes.ProposalPending:
		return p, finishedMessage(p.State), nil
	case p.StaleAt(version):
		return p, proposalStaleMessage, nil
	}
	return p, "", nil
}

func finishedMessage(s apirecipes.ProposalState) string {
	switch s {
	case apirecipes.ProposalAccepted:
		return "This proposal was already saved."
	case apirecipes.ProposalRejected:
		return "This proposal was rejected."
	case apirecipes.ProposalSuperseded:
		return "A newer proposal replaced this one."
	}
	return "This proposal is no longer open for review."
}

// refuse answers 409 with a message that says what to do instead; the generic
// error page shows only the sentinel's text.
func (h *handler) refuse(w http.ResponseWriter, r *http.Request, message string) {
	layout.Render(w, r, http.StatusConflict, layout.ErrorPage(http.StatusConflict, message))
}

// summaryOf builds the summary of p against the live recipe it would
// change, from what the caller already loaded.
func summaryOf(live recipe.Recipe, p apirecipes.Proposal, names lookups) *proposalView {
	return &proposalView{Token: p.Token, By: p.Describe(), Reason: p.Reason, Changes: summarize(live, *p.Edit, names)}
}

// openForReview refuses, with the message to show, when the form's proposal can
// no longer be saved, and returns the proposal otherwise. ok is false when it has
// already answered.
func (h *handler) openForReview(w http.ResponseWriter, r *http.Request, id, version int64, token string) (apirecipes.Proposal, bool) {
	p, refusal, err := h.openProposal(r.Context(), id, version, token)
	switch {
	case err != nil:
		layout.WriteError(w, r, err)
		return apirecipes.Proposal{}, false
	case refusal != "":
		h.refuse(w, r, refusal)
		return apirecipes.Proposal{}, false
	}
	return p, true
}

// acceptFailure is the user's answer to AcceptEdit failing in a way the form
// can explain: ok is false for any other error.
func acceptFailure(err error) (message string, ok bool) {
	switch {
	case errors.Is(err, apirecipes.ErrStaleProposal):
		return proposalStaleMessage, true
	case errors.Is(err, apirecipes.ErrNotPending):
		return "This proposal is no longer open for review, so nothing was saved.", true
	}
	return "", false
}

const maxSummaryText = 100

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxSummaryText {
		return string(r[:maxSummaryText]) + "..."
	}
	return s
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return clip(s)
}

// lineText names an ingredient line for a summary: its amount and ingredient.
func lineText(l recipe.IngredientLine, names lookups) string {
	parts := []string{qtyText(l.Amount.Qty), l.Amount.Unit.Name, names.ingredient[int64(l.Ingredient)]}
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return clip(strings.Join(out, " "))
}

// summarize says in plain sentences what next changes in cur, one line each:
// the fields, then the parts, ingredients, steps and equipment (ADR-00035).
func summarize(cur, next recipe.Recipe, names lookups) []string {
	d := recipe.Compare(cur, next)
	var out []string
	for _, f := range d.Fields {
		out = append(out, fmt.Sprintf("%s: %s, now %s", f.Label, orNone(f.Old), orNone(f.New)))
	}
	for _, c := range d.Components {
		switch c.Kind {
		case recipe.Added:
			out = append(out, fmt.Sprintf("Part added: %s", partName(c.NewName)))
		case recipe.Removed:
			out = append(out, fmt.Sprintf("Part removed: %s", partName(c.OldName)))
		default:
			out = append(out, fmt.Sprintf("Part renamed: %s, now %s", partName(c.OldName), partName(c.NewName)))
		}
	}
	for _, c := range d.Lines {
		switch c.Kind {
		case recipe.Added:
			out = append(out, fmt.Sprintf("Ingredient added: %s", lineText(*c.New, names)))
		case recipe.Removed:
			out = append(out, fmt.Sprintf("Ingredient removed: %s", lineText(*c.Old, names)))
		default:
			if c.Edited {
				out = append(out, fmt.Sprintf("Ingredient changed: %s, now %s", lineText(*c.Old, names), lineText(*c.New, names)))
			} else {
				out = append(out, fmt.Sprintf("Ingredient moved: %s", lineText(*c.Old, names)))
			}
		}
	}
	for _, c := range d.Steps {
		switch c.Kind {
		case recipe.Added:
			out = append(out, fmt.Sprintf("Step added at %d: %s", c.NewNumber, clip(c.New.Text)))
		case recipe.Removed:
			out = append(out, fmt.Sprintf("Step %d removed: %s", c.OldNumber, clip(c.Old.Text)))
		default:
			if c.Edited {
				out = append(out, fmt.Sprintf("Step %d changed: %s, now %s", c.OldNumber, clip(c.Old.Text), clip(c.New.Text)))
			} else {
				out = append(out, fmt.Sprintf("Step %d moved to %d", c.OldNumber, c.NewNumber))
			}
		}
	}
	for _, c := range d.Equipment {
		switch c.Kind {
		case recipe.Added:
			out = append(out, fmt.Sprintf("Equipment added: %s", names.equipment[int64(c.New.Equipment)]))
		case recipe.Removed:
			out = append(out, fmt.Sprintf("Equipment removed: %s", names.equipment[int64(c.Old.Equipment)]))
		default:
			out = append(out, fmt.Sprintf("Equipment changed: %s", names.equipment[int64(c.New.Equipment)]))
		}
	}
	return out
}

func partName(n string) string {
	if n == "" {
		return "(unnamed)"
	}
	return clip(n)
}
