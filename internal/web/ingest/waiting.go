package ingest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// The drafts waiting for review are listed on the import page, and confirming
// or discarding one moves on to the oldest still waiting (ADR-00049). The page
// a person lands on says what they just did: the redirect names the draft that
// ended by its token in doneParam, never by its name, and the page reads the
// name back from the household's own ended draft. A token that names no ended
// draft of the household shows nothing.

// doneParam is the query parameter naming the draft a person just confirmed or
// discarded.
const doneParam = "done"

// maxTokenLen bounds a token read from the query; a real one is 32 hex digits.
const maxTokenLen = 64

// waitingRow is one draft in the import page's list.
type waitingRow struct {
	Path, Name string
	// Source is where the draft was read from: the address's host, or what
	// was given instead of an address.
	Source string
	// Age is how long ago the draft was made.
	Age string
	// Revised is set when an agent revised the draft (ADR-00048).
	Revised bool
	// ReimportOf names the recipe a re-import replaces; empty for a draft of
	// a new recipe.
	ReimportOf string
}

// doneView is the page-level message after a draft was confirmed or
// discarded.
type doneView struct {
	Saved bool
	Name  string
	// RecipePath is the saved recipe's page; empty for a discarded draft.
	RecipePath string
	// Waiting is how many drafts are still waiting for review, the one the
	// page shows included.
	Waiting int
	// OnReview is set on a waiting draft's review page, which is the oldest
	// one; the import page lists them instead.
	OnReview bool
}

// waitingSentence says how many drafts are still waiting, on the page the
// person landed on: the oldest one's review page, or the import page.
func (d doneView) waitingSentence() string {
	switch {
	case d.Waiting == 0:
		return "No drafts are waiting for review."
	case !d.OnReview && d.Waiting == 1:
		return "1 draft is waiting for review."
	case !d.OnReview:
		return strconv.Itoa(d.Waiting) + " drafts are waiting for review."
	case d.Waiting == 1:
		return "This is the last draft waiting for review."
	}
	return strconv.Itoa(d.Waiting) + " drafts are waiting for review, and this is the oldest."
}

// rowsOf turns the pending drafts into the import page's list, ages counted
// from now.
func rowsOf(pending []apirecipes.DraftSummary, now time.Time) []waitingRow {
	rows := make([]waitingRow, len(pending))
	for i, p := range pending {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = "Untitled draft"
		}
		row := waitingRow{
			Path: "/ingest/" + p.Token, Name: name, Source: sourceLabel(p),
			Age: layout.Ago(p.CreatedAt.In(now.Location()), now), Revised: p.Reason != "",
		}
		if p.Reimport {
			row.ReimportOf = p.ReimportName
			if row.ReimportOf == "" {
				row.ReimportOf = "a saved recipe"
			}
		}
		rows[i] = row
	}
	return rows
}

// sourceLabel is where a draft was read from: the host of its address, or
// "Photo" or "Pasted text" for a draft with no address. A draft an agent wrote
// over MCP is read as pasted text, so it says so.
func sourceLabel(p apirecipes.DraftSummary) string {
	if host := layout.SourceHost(p.SourceURL); host != "" {
		return host
	}
	if p.SourceKind == apirecipes.SnapshotOCR {
		return "Photo"
	}
	return "Pasted text"
}

// pending lists the household's drafts waiting for review, oldest first.
func (h *handler) pending(ctx context.Context) ([]apirecipes.DraftSummary, error) {
	return h.recipes.PendingDrafts(ctx, caller.FromContext(ctx))
}

// done reads the message for the draft named in r's doneParam, with waiting
// drafts still waiting. It is nil when the parameter is absent or names no
// draft of the household that has been confirmed or discarded. The message is
// a courtesy on a page that works without it, so a failed read is logged and
// the page is shown without it.
func (h *handler) done(r *http.Request, waiting int) *doneView {
	token := r.URL.Query().Get(doneParam)
	if token == "" || len(token) > maxTokenLen {
		return nil
	}
	e, err := h.recipes.EndedDraft(r.Context(), caller.FromContext(r.Context()), token)
	switch {
	case errors.Is(err, apirecipes.ErrNotFound):
		return nil
	case err != nil:
		h.log.WarnContext(r.Context(), "read the ended draft for the page message", "error", err.Error())
		return nil
	}
	v := &doneView{Saved: e.Saved, Name: e.Name, Waiting: waiting}
	if v.Name == "" {
		v.Name = "the draft"
	}
	if e.Saved && e.RecipeID != 0 {
		v.RecipePath = recipepath.View(e.RecipeID, e.Name)
	}
	return v
}

// doneOnReview is the message on a review page a person was sent to after
// ending another draft, or nil. The drafts are counted only when the page was
// asked to carry one.
func (h *handler) doneOnReview(r *http.Request) *doneView {
	if !r.URL.Query().Has(doneParam) {
		return nil
	}
	pending, err := h.pending(r.Context())
	if err != nil {
		h.log.WarnContext(r.Context(), "count the drafts waiting for the page message", "error", err.Error())
		return nil
	}
	v := h.done(r, len(pending))
	if v != nil {
		v.OnReview = true
	}
	return v
}

// next is where a person goes once the draft under token has been confirmed or
// discarded: the oldest draft still waiting, with the message naming the one
// that ended, or fallback when none is waiting. The draft has ended already,
// so a failure to list the others is logged and answered with fallback rather
// than an error page for something that went through.
func (h *handler) next(r *http.Request, token, fallback string) string {
	pending, err := h.pending(r.Context())
	if err != nil {
		h.log.ErrorContext(r.Context(), "list the drafts waiting after one ended", "error", err.Error())
		return fallback
	}
	if len(pending) == 0 {
		return fallback
	}
	return "/ingest/" + pending[0].Token + "?" + url.Values{doneParam: {token}}.Encode()
}
