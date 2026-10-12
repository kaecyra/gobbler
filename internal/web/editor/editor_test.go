package editor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// env is a real SQLite file with every migration applied (so the seeded
// catalog is there), the real stores over it, the editor routes mounted behind
// the auth middleware, and a recipe to edit.
type env struct {
	t     *testing.T
	ctx   context.Context
	store *store.Store
	cat   *catalog.Store
	h     http.Handler
	id    int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	var authed context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { authed = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if authed == nil {
		t.Fatal("auth middleware did not call the handler")
	}
	s, c := store.New(d), catalog.New(d)
	e := &env{t: t, ctx: authed, store: s, cat: c}
	e.id, err = s.CreateWithSnapshots(authed, e.fixture(), store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("the original source")})
	if err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	r := chi.NewRouter()
	r.Use(a.Middleware())
	r.Mount(Pattern, Routes(Deps{Recipes: s, Catalog: c}))
	e.h = r
	return e
}

// ingredientName is the catalog name of the ingredient of a stored line.
func (e *env) ingredientName(ci, li int) string {
	e.t.Helper()
	r, _ := e.stored()
	i, err := e.cat.GetIngredient(e.ctx, int64(r.Components[ci].Lines[li].Ingredient))
	if err != nil {
		e.t.Fatalf("GetIngredient: %v", err)
	}
	return i.Name
}

// hasIngredient, hasUnit and hasEquipment report whether the catalog matches a
// name, failing the test on a lookup error so an absence check cannot pass on
// a failed read.
func (e *env) hasIngredient(name string) bool {
	e.t.Helper()
	_, ok, err := e.cat.MatchIngredient(e.ctx, name)
	if err != nil {
		e.t.Fatalf("MatchIngredient(%q): %v", name, err)
	}
	return ok
}

func (e *env) hasUnit(name string) bool {
	e.t.Helper()
	_, ok, err := e.cat.MatchUnit(e.ctx, name)
	if err != nil {
		e.t.Fatalf("MatchUnit(%q): %v", name, err)
	}
	return ok
}

func (e *env) hasEquipment(name string) bool {
	e.t.Helper()
	_, ok, err := e.cat.MatchEquipment(e.ctx, name)
	if err != nil {
		e.t.Fatalf("MatchEquipment(%q): %v", name, err)
	}
	return ok
}

// fixture is a two-component recipe with a qualifier, a step dependency across
// components and a piece of equipment.
func (e *env) fixture() recipe.Recipe {
	flour, _, _ := e.cat.MatchIngredient(context.Background(), "flour")
	sugar, _, _ := e.cat.MatchIngredient(context.Background(), "sugar")
	whisk, _, _ := e.cat.MatchEquipment(context.Background(), "whisk")
	return recipe.Recipe{
		Name: "Test Loaf", Description: "Plain.", Tags: []string{"bread"},
		Components: []recipe.Component{
			{
				Name: "Dough",
				Lines: []recipe.IngredientLine{{
					Amount:     quantity.Amount{Qty: quantity.Exact(quantity.Int(2)), Unit: quantity.MustUnit("cup")},
					Ingredient: recipe.IngredientID(flour.ID), Rule: quantity.RuleLinear, Raw: "2 cups sifted flour",
					Qualifiers: []recipe.Qualifier{{Kind: recipe.QualState, Text: "sifted"}},
				}},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
					{ID: "rest", Text: "Let it rest.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				},
			},
			{
				Name:  "Glaze",
				Lines: []recipe.IngredientLine{{Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(1)), Unit: quantity.MustUnit("cup")}, Ingredient: recipe.IngredientID(sugar.ID), Rule: quantity.RuleLinear}},
				Steps: []recipe.Step{{ID: "glaze", Text: "Glaze the loaf.", DependsOn: []recipe.StepID{"rest"}}},
			},
		},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(whisk.ID), Steps: []recipe.StepID{"mix"}}},
	}
}

// names reads the catalog names the way the handler does, to build forms.
func (e *env) names() lookups {
	e.t.Helper()
	h := &handler{Deps: Deps{Recipes: e.store, Catalog: e.cat}}
	n, err := h.names(e.ctx)
	if err != nil {
		e.t.Fatalf("names: %v", err)
	}
	return n
}

// stored returns the current document and version.
func (e *env) stored() (recipe.Recipe, int64) {
	e.t.Helper()
	r, err := e.store.Get(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("Get: %v", err)
	}
	in, err := e.store.Info(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("Info: %v", err)
	}
	return r, in.Version
}

// form is what the editor page would post for the stored recipe right now.
func (e *env) form() url.Values {
	e.t.Helper()
	r, v := e.stored()
	d := fromRecipe(r, e.names())
	d.Version = v
	return encode(d)
}

type result struct {
	*httptest.ResponseRecorder
}

func (r result) body() string { return r.Body.String() }

func (e *env) do(method, target string, form url.Values, hdr map[string]string) result {
	e.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return result{rec}
}

func (e *env) get() result { return e.do(http.MethodGet, e.path(), nil, nil) }

func (e *env) path() string { return "/recipes/" + strconv.FormatInt(e.id, 10) + "/edit" }

func (e *env) post(form url.Values) result { return e.do(http.MethodPost, e.path(), form, nil) }

func (e *env) postHTMX(form url.Values) result {
	return e.do(http.MethodPost, e.path(), form, map[string]string{"HX-Request": "true"})
}

func (e *env) revisions() []store.Revision {
	e.t.Helper()
	revs, err := e.store.ListRevisions(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("ListRevisions: %v", err)
	}
	return revs
}

// encode is the inverse of parseForm: the fields a browser would post for d.
func encode(d draft) url.Values {
	f := url.Values{}
	f.Set(fieldVersion, strconv.FormatInt(d.Version, 10))
	for k, v := range map[string]string{
		"name": d.Name, "description": d.Description, "source_url": d.SourceURL, "author": d.Author,
		"yield_qty": d.YieldQty, "yield_unit": d.YieldUnit, "servings": d.Servings, "calories": d.Calories,
		"tags": d.Tags, "prep": d.Prep, "cook": d.Cook, "total": d.Total,
	} {
		f.Set(k, v)
	}
	for ci, c := range d.Components {
		f.Set(cField(ci, "name"), c.Name)
		for li, l := range c.Lines {
			f.Set(lField(ci, li, "qty"), l.Qty)
			f.Set(lField(ci, li, "unit"), l.Unit)
			f.Set(lField(ci, li, "pkgqty"), l.PkgQty)
			f.Set(lField(ci, li, "pkgunit"), l.PkgUnit)
			f.Set(lField(ci, li, "ing"), l.Ingredient)
			f.Set(lField(ci, li, "rule"), l.Rule)
			f.Set(lField(ci, li, "raw"), l.Raw)
			setFlag(f, lField(ci, li, "optional"), l.Optional)
			setFlag(f, lField(ci, li, "propose"), l.Propose)
			for qi, q := range l.Quals {
				f.Set(qField(ci, li, qi, "kind"), q.Kind)
				f.Set(qField(ci, li, qi, "text"), q.Text)
			}
		}
		for si, s := range c.Steps {
			f.Set(sField(ci, si, "id"), s.ID)
			f.Set(sField(ci, si, "text"), s.Text)
			f.Set(sField(ci, si, "dur"), s.Duration)
			f.Set(sField(ci, si, "to"), strconv.Itoa(s.To))
			setFlag(f, sField(ci, si, "passive"), s.Passive)
			f[sField(ci, si, "dep")] = append([]string(nil), s.Deps...)
		}
	}
	for ei, q := range d.Equipment {
		f.Set(eField(ei, "name"), q.Name)
		f.Set(eField(ei, "qty"), q.Qty)
		setFlag(f, eField(ei, "propose"), q.Propose)
		f[eField(ei, "step")] = append([]string(nil), q.Steps...)
	}
	return f
}

func setFlag(f url.Values, key string, on bool) {
	if on {
		f.Set(key, "1")
	}
}

func (r result) wantStatus(t *testing.T, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("status = %d, want %d; body:\n%s", r.Code, want, r.body())
	}
}

func (r result) wantBody(t *testing.T, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.body(), s) {
			t.Errorf("body lacks %q", s)
		}
	}
}

func TestShowLoadsTheStoredRecipeIntoTheForm(t *testing.T) {
	e := newEnv(t)
	res := e.get()
	res.wantStatus(t, http.StatusOK)
	res.wantBody(t,
		`value="Test Loaf"`, `value="Dough"`, `value="Glaze"`,
		`name="c.0.l.0.qty" value="2"`, `name="c.0.l.0.unit" value="cup"`, `name="c.0.l.0.ing" value="`+e.ingredientName(0, 0)+`"`,
		`name="c.0.l.0.q.0.text" value="sifted"`, `Let it rest.`, `value="60 min"`,
		`name="e.0.name" value="whisk"`, `name="version"`)
}

func TestShowOfAMissingRecipeIs404(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/recipes/9999/edit", "/recipes/abc/edit", "/recipes/0/edit"} {
		if res := e.do(http.MethodGet, p, nil, nil); res.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, res.Code)
		}
	}
}

func TestSavingTwiceWritesTwoRevisionsAndTheFirstHoldsThePreEditDocument(t *testing.T) {
	e := newEnv(t)
	before, _ := e.stored()

	f := e.form()
	f.Set("name", "First Edit")
	res := e.post(f)
	res.wantStatus(t, http.StatusSeeOther)
	if loc := res.Header().Get("Location"); loc != "/recipes/"+strconv.FormatInt(e.id, 10) {
		t.Errorf("redirect to %q, want the recipe page", loc)
	}

	f = e.form()
	f.Set("name", "Second Edit")
	e.post(f).wantStatus(t, http.StatusSeeOther)

	revs := e.revisions()
	if len(revs) != 2 {
		t.Fatalf("revisions = %d, want 2", len(revs))
	}
	if revs[0].Author != auth.DevEmail || revs[0].CreatedAt.IsZero() {
		t.Errorf("revision author/time = %q / %v, want the verified identity and a time", revs[0].Author, revs[0].CreatedAt)
	}
	first, err := e.store.GetVersion(e.ctx, e.id, revs[0].Version)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if first.Name != before.Name {
		t.Errorf("first revision name = %q, want the pre-edit %q", first.Name, before.Name)
	}
	second, err := e.store.GetVersion(e.ctx, e.id, revs[1].Version)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if second.Name != "First Edit" {
		t.Errorf("second revision name = %q, want %q", second.Name, "First Edit")
	}
	if got, _ := e.stored(); got.Name != "Second Edit" {
		t.Errorf("stored name = %q, want %q", got.Name, "Second Edit")
	}
}

func TestSaveWithoutChangesKeepsTheDocument(t *testing.T) {
	e := newEnv(t)
	before, _ := e.stored()
	e.post(e.form()).wantStatus(t, http.StatusSeeOther)
	after, _ := e.stored()
	if err := sameDocument(before, after); err != nil {
		t.Errorf("a save with no edits changed the document: %v", err)
	}
}

func sameDocument(a, b recipe.Recipe) error {
	if a.Name != b.Name || a.Description != b.Description || len(a.Components) != len(b.Components) ||
		len(a.Equipment) != len(b.Equipment) || len(a.Tags) != len(b.Tags) {
		return errors.New("shape differs")
	}
	for ci := range a.Components {
		ca, cb := a.Components[ci], b.Components[ci]
		if ca.Name != cb.Name || len(ca.Lines) != len(cb.Lines) || len(ca.Steps) != len(cb.Steps) {
			return errors.New("component differs: " + ca.Name)
		}
		for li := range ca.Lines {
			la, lb := ca.Lines[li], cb.Lines[li]
			if !la.Amount.Qty.Equal(lb.Amount.Qty) || la.Amount.Unit.Name != lb.Amount.Unit.Name ||
				la.Ingredient != lb.Ingredient || la.Rule != lb.Rule || la.Raw != lb.Raw || len(la.Qualifiers) != len(lb.Qualifiers) {
				return errors.New("line differs: " + la.Raw)
			}
		}
		for si := range ca.Steps {
			sa, sb := ca.Steps[si], cb.Steps[si]
			if sa.ID != sb.ID || sa.Text != sb.Text || sa.Duration != sb.Duration || sa.Passive != sb.Passive ||
				strings.Join(stepIDs(sa.DependsOn), ",") != strings.Join(stepIDs(sb.DependsOn), ",") {
				return errors.New("step differs: " + string(sa.ID))
			}
		}
	}
	return nil
}

func TestEditsReachTheStoredDocument(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set("tags", "bread, easy, bread")
	f.Set("servings", "6")
	f.Set("yield_qty", "1")
	f.Set("yield_unit", "loaf")
	f.Set("prep", "20 min")
	f.Set("calories", "250")
	f.Set(lField(0, 0, "qty"), "1½")
	f.Set(lField(0, 0, "unit"), "cups") // an alias
	f.Set(lField(0, 0, "rule"), "fixed")
	f.Set(lField(0, 0, "optional"), "1")
	f.Set(qField(0, 0, 0, "kind"), "preparation")
	f.Set(qField(0, 0, 0, "text"), "finely sifted")
	f.Set(sField(0, 0, "text"), "Mix well.")
	f.Set(sField(0, 0, "dur"), "1 h 30 min")
	f.Set(sField(0, 0, "passive"), "1")
	f.Set(cField(1, "name"), "Icing")
	e.post(f).wantStatus(t, http.StatusSeeOther)

	got, _ := e.stored()
	l := got.Components[0].Lines[0]
	switch {
	case strings.Join(got.Tags, ",") != "bread,easy":
		t.Errorf("tags = %v, want bread, easy", got.Tags)
	case got.Servings.String() != "6" || got.Yield.Unit != "loaf" || got.Times.Prep != 20*time.Minute:
		t.Errorf("servings/yield/prep = %v / %v / %v", got.Servings, got.Yield, got.Times.Prep)
	case got.CaloriesPerServing == nil || *got.CaloriesPerServing != 250:
		t.Errorf("calories = %v", got.CaloriesPerServing)
	case l.Amount.Qty.String() != "1 1/2" || l.Amount.Unit.Name != "cup" || l.Rule != quantity.RuleFixed || !l.Optional:
		t.Errorf("line = %+v", l)
	case len(l.Qualifiers) != 1 || l.Qualifiers[0] != (recipe.Qualifier{Kind: recipe.QualPreparation, Text: "finely sifted"}):
		t.Errorf("qualifiers = %+v", l.Qualifiers)
	case got.Components[0].Steps[0].Text != "Mix well." || got.Components[0].Steps[0].Duration != 90*time.Minute || !got.Components[0].Steps[0].Passive:
		t.Errorf("step = %+v", got.Components[0].Steps[0])
	case got.Components[1].Name != "Icing":
		t.Errorf("component name = %q", got.Components[1].Name)
	}
}

func TestQuantitiesAcceptFractionsMixedNumbersDecimalsAndRanges(t *testing.T) {
	for in, want := range map[string]string{"½": "1/2", "1 1/2": "1 1/2", "0.75": "3/4", "2-3": "2-3", "1½": "1 1/2"} {
		e := newEnv(t)
		f := e.form()
		f.Set(lField(0, 0, "qty"), in)
		e.post(f).wantStatus(t, http.StatusSeeOther)
		got, _ := e.stored()
		if q := got.Components[0].Lines[0].Amount.Qty.String(); q != want {
			t.Errorf("quantity %q stored as %q, want %q", in, q, want)
		}
	}
}

func TestInvalidQuantityShowsAnInlineErrorKeepsInputAndSavesNothing(t *testing.T) {
	e := newEnv(t)
	before, v := e.stored()
	f := e.form()
	f.Set("name", "Should Not Save")
	f.Set(lField(0, 0, "qty"), "lots")
	res := e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, `&#34;lots&#34; is not a quantity`, `name="c.0.l.0.qty" value="lots"`, `value="Should Not Save"`)
	after, v2 := e.stored()
	if v2 != v || len(e.revisions()) != 0 {
		t.Errorf("version %d -> %d, revisions %d: a rejected save must write nothing", v, v2, len(e.revisions()))
	}
	if err := sameDocument(before, after); err != nil {
		t.Errorf("stored recipe changed: %v", err)
	}
}

func TestRejectedSaveWithHTMXIsAFragmentThatCanBeSwapped(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(lField(0, 0, "qty"), "lots")
	res := e.postHTMX(f)
	res.wantStatus(t, http.StatusOK)
	if strings.Contains(res.body(), "<html") {
		t.Error("an htmx response must be the form fragment, not a page")
	}
	res.wantBody(t, `id="editor"`, "is not a quantity")
}

func TestAcceptedSaveWithHTMXRedirectsTheBrowser(t *testing.T) {
	e := newEnv(t)
	res := e.postHTMX(e.form())
	res.wantStatus(t, http.StatusOK)
	if got := res.Header().Get("HX-Redirect"); got != "/recipes/"+strconv.FormatInt(e.id, 10) {
		t.Errorf("HX-Redirect = %q", got)
	}
}

func TestStepCycleIsRejectedNamingTheStepsAndSavesNothing(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	// mix now depends on glaze, which depends on rest, which depends on mix.
	f[sField(0, 0, "dep")] = []string{"glaze"}
	res := e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, "depend on each other in a circle", "step 1 (mix)", "step 1 (glaze)", "step 2 (rest)", `name="c.0.s.0.dep" value="glaze" checked`)
	if len(e.revisions()) != 0 {
		t.Error("a cycle must not write a revision")
	}
	if got, _ := e.stored(); len(got.Components[0].Steps[0].DependsOn) != 0 {
		t.Errorf("stored mix depends on %v, want none", got.Components[0].Steps[0].DependsOn)
	}
}

func TestStaleSaveIsRejectedAndDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	loaded := e.form()

	other := e.form()
	other.Set("name", "Edited elsewhere")
	e.post(other).wantStatus(t, http.StatusSeeOther)

	loaded.Set("name", "Edited from the old form")
	res := e.post(loaded)
	res.wantStatus(t, http.StatusConflict)
	res.wantBody(t, "was changed after you opened the editor", `value="Edited from the old form"`)
	if got, _ := e.stored(); got.Name != "Edited elsewhere" {
		t.Errorf("stored name = %q: the stale save overwrote the newer edit", got.Name)
	}
	if n := len(e.revisions()); n != 1 {
		t.Errorf("revisions = %d, want only the one from the first edit", n)
	}
}

func TestUnknownIngredientIsRefusedUnlessProposed(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(lField(0, 0, "ing"), "zzyzx root")
	res := e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, "No ingredient in the catalog is called", "unreviewed ingredient", `name="c.0.l.0.propose"`)
	if e.hasIngredient("zzyzx root") {
		t.Error("a refused form must not add to the catalog")
	}

	f.Set(lField(0, 0, "propose"), "1")
	e.post(f).wantStatus(t, http.StatusSeeOther)
	ing, ok, err := e.cat.MatchIngredient(e.ctx, "zzyzx root")
	if err != nil || !ok {
		t.Fatalf("proposed ingredient: found %v, err %v", ok, err)
	}
	if ing.Reviewed {
		t.Error("a proposed ingredient must be unreviewed")
	}
	got, _ := e.stored()
	if got.Components[0].Lines[0].Ingredient != recipe.IngredientID(ing.ID) {
		t.Errorf("line ingredient = %d, want the proposed %d", got.Components[0].Lines[0].Ingredient, ing.ID)
	}
}

func TestFormRejectedByValidationDoesNotCreateProposedEntries(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(lField(0, 0, "ing"), "zzyzx root")
	f.Set(lField(0, 0, "propose"), "1")
	f.Set(lField(0, 0, "qty"), "lots")
	e.post(f).wantStatus(t, http.StatusBadRequest)
	if e.hasIngredient("zzyzx root") {
		t.Error("a form rejected for another reason must not create the proposed ingredient")
	}
}

func TestUnknownUnitIsFlaggedNotCreated(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(lField(0, 0, "unit"), "smidgeon-of-doom")
	res := e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, "There is no unit called")
	if e.hasUnit("smidgeon-of-doom") {
		t.Error("the editor must not create units")
	}
}

func TestProposedEquipmentIsAddedUnreviewed(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(eField(0, "name"), "zzyzx grinder")
	e.post(f).wantStatus(t, http.StatusBadRequest)
	f.Set(eField(0, "propose"), "1")
	e.post(f).wantStatus(t, http.StatusSeeOther)
	eq, ok, err := e.cat.MatchEquipment(e.ctx, "zzyzx grinder")
	if err != nil || !ok || eq.Reviewed {
		t.Fatalf("proposed equipment: found %v, reviewed %v, err %v", ok, eq.Reviewed, err)
	}
}

func TestActionsReshapeTheFormWithoutSaving(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set(fieldAction, "add-component")
	f.Set("name", "Typed but unsaved")
	res := e.post(f)
	res.wantStatus(t, http.StatusOK)
	res.wantBody(t, `name="c.2.name"`, `value="Typed but unsaved"`)
	if len(e.revisions()) != 0 {
		t.Error("an action must not save")
	}
	if got, _ := e.stored(); len(got.Components) != 2 || got.Name != "Test Loaf" {
		t.Errorf("an action changed the stored recipe: %d components, name %q", len(got.Components), got.Name)
	}
}

func TestBadActionIs400(t *testing.T) {
	e := newEnv(t)
	for _, a := range []string{"rm-line:9.9", "fly:0", "rm-step:x", "up-qual:0.0"} {
		f := e.form()
		f.Set(fieldAction, a)
		if res := e.post(f); res.Code != http.StatusBadRequest {
			t.Errorf("action %q = %d, want 400", a, res.Code)
		}
	}
}

func TestPostWithoutAVersionIs400(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Del(fieldVersion)
	e.post(f).wantStatus(t, http.StatusBadRequest)
}

func TestSaveNeverTouchesTheSourceSnapshot(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set("name", "Edited")
	e.post(f).wantStatus(t, http.StatusSeeOther)
	snap, err := e.store.GetSnapshot(e.ctx, e.id, store.SnapshotText)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if string(snap.Content) != "the original source" {
		t.Errorf("snapshot = %q, want it unchanged", snap.Content)
	}
}

func TestSaveKeepsWhatTheFormDoesNotEdit(t *testing.T) {
	e := newEnv(t)
	r, _ := e.stored()
	rating := 4
	r.Rating = &rating
	r.Times.Special = []recipe.SpecialTime{{Label: recipe.LabelRise, Duration: time.Hour}}
	if err := e.store.Update(e.ctx, e.id, r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	f := e.form()
	f.Set("name", "Renamed")
	e.post(f).wantStatus(t, http.StatusSeeOther)
	got, _ := e.stored()
	if got.Rating == nil || *got.Rating != 4 || len(got.Times.Special) != 1 {
		t.Errorf("rating %v, special times %v: the editor dropped what it does not show", got.Rating, got.Times.Special)
	}
}

func TestBlankNameAndNoComponentsAreRefused(t *testing.T) {
	e := newEnv(t)
	f := e.form()
	f.Set("name", "  ")
	res := e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, "A recipe needs a name.")

	f = e.form()
	for k := range f {
		if strings.HasPrefix(k, "c.") || strings.HasPrefix(k, "e.") {
			f.Del(k)
		}
	}
	res = e.post(f)
	res.wantStatus(t, http.StatusBadRequest)
	res.wantBody(t, "at least one component")
}

func TestStaleSaveWithAProposalCreatesNoCatalogEntry(t *testing.T) {
	e := newEnv(t)
	loaded := e.form()

	other := e.form()
	other.Set("name", "Edited elsewhere")
	e.post(other).wantStatus(t, http.StatusSeeOther)

	loaded.Set(lField(0, 0, "ing"), "zzyzx root")
	loaded.Set(lField(0, 0, "propose"), "1")
	loaded.Set(eField(0, "name"), "zzyzx grinder")
	loaded.Set(eField(0, "propose"), "1")
	e.post(loaded).wantStatus(t, http.StatusConflict)
	if e.hasIngredient("zzyzx root") {
		t.Error("a stale save created the proposed ingredient")
	}
	if e.hasEquipment("zzyzx grinder") {
		t.Error("a stale save created the proposed equipment")
	}
}
