package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	"github.com/kaecyra/gobbler/internal/auth"
	domain "github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// doCentral sends a request to the central ingredient routes as the dev
// identity, which is the configured operator. operator says whether the
// request is marked as the operator's, as the membership middleware does. The
// context also carries a household, as it does for an operator who is a
// member, so every test shows the page edits the central row whatever the
// context names.
func doCentral(cat *domain.Store, operator bool, method, target string, form url.Values) *httptest.ResponseRecorder {
	root := chi.NewRouter()
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: cat, Members: household.New(nil), Operators: config.Operator{Emails: []string{auth.DevEmail}}})
	if err != nil {
		panic(err)
	}
	root.Mount(CentralPattern, CentralRoutes(CentralDeps{Catalog: svc.Bind(caller.FromContext)}))
	req := httptest.NewRequest(method, CentralPattern+target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(auth.DevIdentityHeader, auth.DevEmail)
	req = req.WithContext(household.WithContext(verified(req), household.Household{ID: 7, Name: "Smiths"}))
	if operator {
		req = req.WithContext(layout.WithOperator(req.Context()))
	}
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req)
	return w
}

func newCentralIngredient(t *testing.T, cat *domain.Store, in domain.Ingredient) domain.Ingredient {
	t.Helper()
	got, err := cat.CreateIngredient(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCentralPagesRefuseANonOperator(t *testing.T) {
	cat := newEquipmentFixture(t)
	sage := newCentralIngredient(t, cat, domain.Ingredient{Name: "zz sage", Aisle: "produce", Reviewed: true})
	target := "/" + strconv.FormatInt(sage.ID, 10)
	for _, c := range []struct {
		method, target string
		form           url.Values
	}{
		{http.MethodGet, "/", nil},
		{http.MethodGet, target, nil},
		{http.MethodPost, target, url.Values{"name": {"zz sage"}, "aisle": {"herbs"}}},
	} {
		w := doCentral(cat, false, c.method, c.target, c.form)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", c.method, c.target, w.Code)
		}
		if strings.Contains(w.Body.String(), "zz sage") {
			t.Errorf("%s %s showed the ingredient to a non-operator", c.method, c.target)
		}
	}
	if got, err := cat.GetIngredient(context.Background(), sage.ID); err != nil || got.Aisle != "produce" {
		t.Errorf("a refused save changed the ingredient: %+v, %v", got, err)
	}
}

func TestCentralListFilters(t *testing.T) {
	cat := newEquipmentFixture(t)
	newCentralIngredient(t, cat, domain.Ingredient{Name: "zz sage", Aisle: "produce", Aliases: []string{"zz garden sage"}, Reviewed: true})
	newCentralIngredient(t, cat, domain.Ingredient{Name: "zz cumin", Aisle: "spices", Reviewed: true})

	w := doCentral(cat, true, http.MethodGet, "/", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d\n%s", w.Code, w.Body)
	}
	for _, want := range []string{"zz sage", "zz garden sage", "zz cumin"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("list lacks %q", want)
		}
	}
	w = doCentral(cat, true, http.MethodGet, "/?q=garden", nil)
	if body := w.Body.String(); !strings.Contains(body, "zz sage") || strings.Contains(body, "zz cumin") {
		t.Errorf("filter by alias: want only zz sage\n%s", body)
	}
	w = doCentral(cat, true, http.MethodGet, "/?q=nothing-like-it", nil)
	if !strings.Contains(w.Body.String(), "No ingredient matches the filter.") {
		t.Errorf("empty filter result lacks its message\n%s", w.Body)
	}
}

func TestCentralEditFormLetsTheOperatorChangeTheNameAndDensity(t *testing.T) {
	cat := newEquipmentFixture(t)
	usda := int64(170931)
	sage := newCentralIngredient(t, cat, domain.Ingredient{Name: "zz sage", Aisle: "produce", USDAID: &usda, Reviewed: true})
	w := doCentral(cat, true, http.MethodGet, "/"+strconv.FormatInt(sage.ID, 10), nil)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", w.Code, body)
	}
	if !strings.Contains(body, `value="zz sage" required>`) {
		t.Errorf("the name is not editable\n%s", body)
	}
	if !strings.Contains(body, `inputmode="decimal">`) || strings.Contains(body, `inputmode="decimal" readonly`) {
		t.Errorf("the density is not editable\n%s", body)
	}
	if !strings.Contains(body, `value="170931" inputmode="numeric" readonly>`) {
		t.Errorf("the USDA id is not shown read-only\n%s", body)
	}
	if strings.Contains(body, `name="reviewed"`) {
		t.Error("the form offers the reviewed flag")
	}
}

func TestCentralSaveEditsTheCentralRowAndRedirects(t *testing.T) {
	cat := newEquipmentFixture(t)
	usda := int64(170931)
	sage := newCentralIngredient(t, cat, domain.Ingredient{Name: "zz sage", Aisle: "produce", Aliases: []string{"zz sage leaf"}, USDAID: &usda, Reviewed: true})
	w := doCentral(cat, true, http.MethodPost, "/"+strconv.FormatInt(sage.ID, 10), url.Values{
		"name":    {"zz garden sage"},
		"aliases": {"zz sage leaf\nzz common sage"},
		"aisle":   {"herbs"},
		"density": {"0.1"},
		// The USDA id is not the form's to change: a value sent anyway is ignored.
		"usda_id": {"1"},
	})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != CentralPattern {
		t.Fatalf("save = %d to %q, want 303 to %s\n%s", w.Code, w.Header().Get("Location"), CentralPattern, w.Body)
	}
	// Read with no household: the central row itself changed. In the
	// household's scope a rename of a central entry is refused, so the save
	// could not have gone there.
	got, err := cat.GetIngredient(context.Background(), sage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "zz garden sage" || got.Aisle != "herbs" || got.DensityGPerML == nil || *got.DensityGPerML != 0.1 ||
		len(got.Aliases) != 2 || got.USDAID == nil || *got.USDAID != usda || !got.Reviewed || !got.Central {
		t.Errorf("after save = %+v", got)
	}
}

func TestCentralSaveShowsProblemsInline(t *testing.T) {
	cat := newEquipmentFixture(t)
	sage := newCentralIngredient(t, cat, domain.Ingredient{Name: "zz sage", Aisle: "produce", Reviewed: true})
	newCentralIngredient(t, cat, domain.Ingredient{Name: "zz thyme", Aisle: "produce", Reviewed: true})
	target := "/" + strconv.FormatInt(sage.ID, 10)

	w := doCentral(cat, true, http.MethodPost, target, url.Values{"name": {"zz thyme"}, "aisle": {"produce"}})
	if w.Code != http.StatusConflict {
		t.Errorf("rename onto another entry = %d, want 409", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `data-testid="form-error"`) || !strings.Contains(body, "is already used by") {
		t.Errorf("the conflict is not shown on the form\n%s", body)
	}

	w = doCentral(cat, true, http.MethodPost, target, url.Values{"name": {"zz sage"}, "aliases": {"zz thyme"}, "aisle": {"produce"}})
	if w.Code != http.StatusConflict {
		t.Errorf("alias naming another entry = %d, want 409", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `data-testid="form-error"`) || !strings.Contains(body, "is already used by") {
		t.Errorf("the alias conflict is not shown on the form\n%s", body)
	}

	w = doCentral(cat, true, http.MethodPost, target, url.Values{"name": {"zz sage"}, "aisle": {"produce"}, "density": {"heavy"}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), densityMessage) {
		t.Errorf("bad density = %d, want 400 with the density message\n%s", w.Code, w.Body)
	}
	if got, err := cat.GetIngredient(context.Background(), sage.ID); err != nil || got.Name != "zz sage" || got.DensityGPerML != nil || len(got.Aliases) != 0 {
		t.Errorf("a refused save changed the ingredient: %+v, %v", got, err)
	}
}

func TestCentralPagesDoNotOpenAMissingEntry(t *testing.T) {
	cat := newEquipmentFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		w := doCentral(cat, true, method, "/999", url.Values{"name": {"zz x"}, "aisle": {"x"}})
		if w.Code != http.StatusNotFound {
			t.Errorf("%s of a missing entry = %d, want 404", method, w.Code)
		}
	}
	if w := doCentral(cat, true, http.MethodGet, "/abc", nil); w.Code != http.StatusBadRequest {
		t.Errorf("unreadable id = %d, want 400", w.Code)
	}
}
