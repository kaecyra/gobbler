package ingest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// The review screen is the recipe editor over the unsaved draft (ADR-00020
// decision 3): these tests drive it through the editor's own form.

func TestReviewIsTheEditorForm(t *testing.T) {
	f := newFixture(t)
	body := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()
	for _, want := range []string{
		`name="c.0.l.0.qty"`, `name="c.0.l.0.unit"`, `name="c.0.l.0.ing"`, `name="c.0.l.0.optional"`,
		`name="c.0.s.0.dur"`, `name="c.0.s.0.to"`, `value="add-line:0"`, `value="rm-line:0.0"`, `value="add-qual:0.0"`,
		`value="add-component"`, `value="add-equip"`, "Confirm and save", "Cancel",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("review lacks the editor control %s", want)
		}
	}
}

// The draft cannot keep these yet, so offering them would drop an edit
// silently on confirm (#230 turns the step graph on).
func TestReviewOffersNoControlTheDraftCannotKeep(t *testing.T) {
	f := newFixture(t)
	body := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()
	for _, absent := range []string{`name="c.0.s.0.dep"`, `name="c.0.s.0.passive"`, `type="checkbox" name="c.0.s.0`, `name="tags"`, `name="video"`, `name="source_url"`, `name="version"`, `<select id="c.0.l.0.rule"`, `name="e.0.qty"`, `name="e.0.step"`} {
		if strings.Contains(body, absent) {
			t.Errorf("review offers %s, which confirm would drop", absent)
		}
	}
}

func TestReviewFlagsLowConfidenceAndLLMItemsInWords(t *testing.T) {
	f := newFixture(t)
	f.drafter.draft.Equipment[2].FromLLM = true
	body := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()

	if got := strings.Count(body, "data-low"); got != 2 {
		t.Errorf("low items = %d, want the unmatched line and the weak step", got)
	}
	line := between(t, body, `<li class="item line low" data-low>`, "</li>")
	if !strings.Contains(line, "Check:") || !strings.Contains(line, "dragonfruit zest") || !strings.Contains(line, "not matched") {
		t.Errorf("low-confidence line is not flagged in words with its issue: %s", line)
	}
	if first := items(body, "line")[0]; strings.Contains(first, "Check:") {
		t.Errorf("a confident line is flagged: %s", first)
	}
	if !strings.Contains(between(t, body, `<li class="item equipment llm" data-llm>`, "</li>"), "LLM") {
		t.Error("LLM equipment is not marked in words")
	}
	if !strings.Contains(body, "Check this draft.") {
		t.Error("a below-threshold draft has no overall warning")
	}
}

// The unmatched line is offered as a new ingredient, ticked as the draft
// proposed; the matched line offers no such choice.
func TestReviewOffersAnUnmatchedLineAsANewIngredient(t *testing.T) {
	f := newFixture(t)
	body := f.do(http.MethodGet, "/"+f.ingestText(t), nil).Body.String()
	if !strings.Contains(body, `name="c.0.l.1.propose" value="1" checked`) {
		t.Error("the unmatched line is not offered as new, ticked")
	}
	if strings.Contains(body, `name="c.0.l.0.propose"`) {
		t.Error("a matched line offers a new-ingredient choice")
	}
	if !strings.Contains(body, `name="e.1.propose" value="1" checked`) || strings.Contains(body, `name="e.0.propose"`) {
		t.Error("only unmatched equipment is offered as new")
	}
}

// Editing an amount and a unit on the draft, then confirming, saves the
// edited values (ADR-00020 obligation 4).
func TestConfirmSavesAnEditedAmountAndUnit(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("c.0.l.0.qty", "3")
	form.Set("c.0.l.0.unit", "cup")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	got := f.store.recipes[0].Components[0].Lines[0].Amount
	if got.Qty.String() != "3" || got.Unit.Name != "cup" {
		t.Errorf("saved amount = %s %s, want 3 cup", got.Qty, got.Unit.Name)
	}
}

func TestConfirmSavesAnAddedLineARemovedLineAndAMovedStep(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("c.0.l.2.qty", "1")
	form.Set("c.0.l.2.unit", "cup")
	form.Set("c.0.l.2.ing", "butter")
	form.Set("c.0.l.2.raw", "")
	form.Set("c.0.l.2.rule", "linear")
	form.Del("c.0.l.1.ing")
	form.Del("c.0.l.1.raw")
	form.Del("c.0.l.1.propose")
	// A second component, and the weak step placed in it with the step's
	// component selector.
	form.Set("c.1.name", "Topping")
	form.Set("c.0.s.1.to", "1")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	comps := f.store.recipes[0].Components
	if len(comps) != 2 || comps[1].Name != "Topping" || len(comps[0].Steps) != 1 || len(comps[1].Steps) != 1 || comps[1].Steps[0].Text != "Bake until goldn" {
		t.Errorf("saved components = %+v, want the second step moved into Topping", comps)
	}
	lines := comps[0].Lines
	if len(lines) != 2 || lines[1].Amount.Unit.Name != "cup" {
		t.Errorf("saved lines = %+v, want the butter line and the added cup line (the blank line dropped)", lines)
	}
	if len(f.store.created) != 0 {
		t.Errorf("created %v, but the proposed line was removed", f.store.created)
	}
}

func TestConfirmKeepsAToTasteLine(t *testing.T) {
	f := newFixture(t)
	f.drafter.draft.Components[0].Lines[0].ToTaste = true
	token := f.ingestText(t)
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, `name="c.0.l.0.rule" value="to_taste"`) {
		t.Fatalf("a to-taste line does not carry its rule through the form: %s", body)
	}
	form := reviewForm()
	form.Set("c.0.l.0.rule", "to_taste")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if got := f.store.recipes[0].Components[0].Lines[0].Rule; got != quantity.RuleToTaste {
		t.Errorf("saved rule = %q, want to_taste", got)
	}
}

func TestConfirmRejectsWhatTheEditorRejects(t *testing.T) {
	tests := []struct {
		name string
		edit func(url.Values)
		want string
	}{
		{"bad quantity", func(f url.Values) { f.Set("c.0.l.0.qty", "lots") }, "is not a quantity"},
		{"unknown unit", func(f url.Values) { f.Set("c.0.l.0.unit", "zorb") }, "There is no unit called"},
		{"bad duration", func(f url.Values) { f.Set("c.0.s.0.dur", "soon") }, "is not a duration"},
		{"ingredient not in the catalog, not proposed", func(f url.Values) { f.Set("c.0.l.1.ing", "mystery"); f.Del("c.0.l.1.propose") }, "No ingredient in the catalog is called"},
		{"no ingredient lines", func(f url.Values) {
			f.Del("c.0.l.0.ing")
			f.Del("c.0.l.0.qty")
			f.Del("c.0.l.0.unit")
			f.Del("c.0.l.0.raw")
			f.Del("c.0.l.1.ing")
			f.Del("c.0.l.1.raw")
		}, "no ingredient lines"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			token := f.ingestText(t)
			form := reviewForm()
			form.Set("author", "Grace")
			tc.edit(form)
			rec := f.do(http.MethodPost, "/"+token+"/confirm", form)
			body := rec.Body.String()
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, tc.want) {
				t.Fatalf("status = %d, want 422 with %q; body: %s", rec.Code, tc.want, body)
			}
			if !strings.Contains(body, `value="Grace"`) {
				t.Error("the user's other edits were lost")
			}
			if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
				t.Error("a rejected form saved something")
			}
			if f.do(http.MethodGet, "/"+token, nil).Code != http.StatusOK {
				t.Error("the draft was lost")
			}
		})
	}
}

// A structural button changes what is shown and nothing else.
func TestStructuralButtonShowsTheFormAgainAndSavesNothing(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("author", "Grace")
	form.Set("action", "add-line:0")
	rec := f.do(http.MethodPost, "/"+token+"/confirm", form)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Line 3") || !strings.Contains(body, `value="Grace"`) {
		t.Fatalf("status = %d; body: %s", rec.Code, body)
	}
	if len(f.store.recipes) != 0 || len(f.store.created) != 0 {
		t.Error("a structural button saved something")
	}
	if f.do(http.MethodGet, "/"+token, nil).Code != http.StatusOK {
		t.Error("the draft was lost")
	}
}

func TestStructuralButtonNamingNothingIsRefused(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("action", "rm-line:0.9")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// A flag follows its line when the line moves, as the form is rebuilt from
// what was posted.
func TestFlagsSurviveAStructuralButton(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	first := f.do(http.MethodGet, "/"+token, nil).Body.String()
	form := reviewForm()
	for _, name := range []string{"c.0.l.1.mark", "c.0.s.1.mark"} {
		if !strings.Contains(first, `name="`+name+`" value="low"`) {
			t.Fatalf("form does not carry %s", name)
		}
		form.Set(name, "low")
	}
	form.Set("c.0.l.1.issue", "ingredient not matched")
	form.Set("action", "up-line:0.1")
	body := f.do(http.MethodPost, "/"+token+"/confirm", form).Body.String()
	lines := items(body, "line")
	if !strings.Contains(lines[0], "Check:") || !strings.Contains(lines[0], "not matched") {
		t.Errorf("the flag and its issue did not move with the line: %s", lines[0])
	}
	if strings.Contains(lines[1], "Check:") {
		t.Error("the flag stayed behind on the old position")
	}
}

func TestHTMXFormIsAFragmentAndConfirmRedirectsWithAHeader(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	post := func(form url.Values) (int, string, string) {
		req := newFormRequest("/"+token+"/confirm", form)
		req.Header.Set("HX-Request", "true")
		rec := f.serve(req)
		return rec.Code, rec.Body.String(), rec.Header().Get("HX-Redirect")
	}

	bad := reviewForm()
	bad.Set("name", "")
	code, body, _ := post(bad)
	if code != http.StatusOK || strings.Contains(body, "<html") || !strings.Contains(body, "A recipe needs a name.") {
		t.Errorf("rejected htmx form: status = %d, body = %s", code, body)
	}
	code, _, redirect := post(reviewForm())
	if code != http.StatusOK || redirect != "/recipes/1" {
		t.Errorf("confirmed htmx form: status = %d HX-Redirect = %q, want 200 /recipes/1", code, redirect)
	}
}

func TestConfirmSavesTheEditedRecipeWithItsSnapshot(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	form := reviewForm()
	form.Set("servings", "4")
	form.Set("calories", "320")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	r := f.store.recipes[0]
	if r.Servings.String() != "4" || r.CaloriesPerServing == nil || *r.CaloriesPerServing != 320 {
		t.Errorf("servings/calories not saved: %v %v", r.Servings, r.CaloriesPerServing)
	}
	if r.SourceURL != "https://example.test/pie" || f.store.snapshots != 1 {
		t.Errorf("the source (%q) or its snapshot (%d) was not kept", r.SourceURL, f.store.snapshots)
	}
}

// items splits the editor markup into the list items of one kind ("line",
// "step", "equipment"), each up to its closing tag.
func items(body, kind string) []string {
	var out []string
	for _, part := range strings.Split(body, `<li class="item `+kind)[1:] {
		item, _, _ := strings.Cut(part, "</li>")
		out = append(out, item)
	}
	return out
}

// The editor form is an htmx form that swaps its answer in place, so Cancel is
// a button of a separate form that posts and leaves.
func TestCancelIsAButtonOfItsOwnForm(t *testing.T) {
	f := newFixture(t)
	token := f.ingestText(t)
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	if !strings.Contains(body, `<form id="review-cancel" method="post" action="/ingest/`+token+`/cancel">`) ||
		!strings.Contains(body, `<button type="submit" form="review-cancel"`) {
		t.Error("Cancel is not tied to a form that posts to the cancel route")
	}
}

// A draft put back after a failed save is what the user left: their edits and
// the flags the rules and the LLM raised, which the next GET shows again
// (ADR-00006 obligation 4).
func TestDraftPutBackAfterAFailedSaveKeepsEditsAndFlags(t *testing.T) {
	for name, saver := range map[string]Saver{"unknown unit": unknownUnitSaver{}, "save failure": failingSaver{}} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.h.saver = saver
			token := f.ingestText(t)
			form := reviewForm()
			form.Set("author", "Grace")
			form.Set("c.0.l.1.mark", "low llm")
			form.Set("c.0.l.1.issue", "ingredient not matched")
			form.Set("c.0.s.1.mark", "low")
			form.Set("e.2.mark", "llm")
			form.Set("mark.name", "low")
			if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code < 400 {
				t.Fatalf("status = %d, want the save to fail", rec.Code)
			}
			rec := f.do(http.MethodGet, "/"+token, nil)
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, `value="Grace"`) {
				t.Fatalf("the user's edit did not survive: status %d", rec.Code)
			}
			if got := strings.Count(body, "data-low"); got != 2 {
				t.Errorf("low items after the failed save = %d, want the line and the step", got)
			}
			line := items(body, "line")[1]
			if !strings.Contains(line, "Check:") || !strings.Contains(line, "LLM") || !strings.Contains(line, "ingredient not matched") {
				t.Errorf("the line lost its flags or issue: %s", line)
			}
			if !strings.Contains(items(body, "equipment")[2], "LLM") {
				t.Error("the equipment lost its LLM mark")
			}
			if !strings.Contains(between(t, body, `<label for="f-name">`, "</div>"), "Check:") {
				t.Error("the name lost its flag")
			}
		})
	}
}

// Flags follow their rows past the blank rows the editor drops: a flagged row
// after a blank middle row keeps its flag in the draft put back.
func TestFlagsFollowTheirRowsPastBlankRows(t *testing.T) {
	f := newFixture(t)
	f.h.saver = failingSaver{}
	token := f.ingestText(t)
	form := reviewForm()
	// Blank middle rows, then flagged rows behind them.
	form.Set("c.0.l.2.rule", "linear") // line 1 (the proposed one) moves to form row 2
	for k, v := range map[string]string{
		"c.0.l.2.ing": "dragonfruit zest", "c.0.l.2.propose": "1", "c.0.l.2.raw": "1 dragonfruit zest",
		"c.0.l.2.mark": "low llm", "c.0.l.2.issue": "ingredient not matched",
		"c.0.s.2.text": "Bake until goldn", "c.0.s.2.to": "0", "c.0.s.2.mark": "low",
		"e.3.name": "whisk", "e.3.mark": "llm",
	} {
		form.Set(k, v)
	}
	for _, k := range []string{"c.0.l.1.ing", "c.0.l.1.raw", "c.0.l.1.propose", "c.0.s.1.text", "e.2.name"} {
		form.Del(k)
	}
	form.Set("c.0.l.1.rule", "linear") // a row present but blank
	form.Set("c.0.s.1.to", "0")
	form.Set("e.2.qty", "")
	if rec := f.do(http.MethodPost, "/"+token+"/confirm", form); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want the save to fail", rec.Code)
	}
	body := f.do(http.MethodGet, "/"+token, nil).Body.String()
	lines := items(body, "line")
	if len(lines) != 2 || !strings.Contains(lines[1], "Check:") || !strings.Contains(lines[1], "LLM") || !strings.Contains(lines[1], "ingredient not matched") || strings.Contains(lines[0], "Check:") {
		t.Errorf("lines after the blank row lost their flags or took another's: %q", lines)
	}
	steps := items(body, "step")
	if len(steps) != 2 || !strings.Contains(steps[1], "Check:") || strings.Contains(steps[0], "Check:") {
		t.Errorf("steps after the blank row: %q", steps)
	}
	equip := items(body, "equipment")
	if len(equip) != 3 || !strings.Contains(equip[2], "LLM") || strings.Contains(equip[1], "LLM") {
		t.Errorf("equipment after the blank row: %q", equip)
	}
}
