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

	domain "github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

func newEquipmentFixture(t *testing.T) *domain.Store {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	return domain.New(d)
}

func doEquipment(cat *domain.Store, operator bool, method, target string, form url.Values) *httptest.ResponseRecorder {
	root := chi.NewRouter()
	root.Mount(EquipmentPattern, EquipmentRoutes(EquipmentDeps{Catalog: cat}))
	req := httptest.NewRequest(method, EquipmentPattern+target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if operator {
		req = req.WithContext(layout.WithOperator(req.Context()))
	}
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req)
	return w
}

func TestEquipmentPageListsCatalogEquipmentWithItsSizedState(t *testing.T) {
	cat := newEquipmentFixture(t)
	ctx := context.Background()
	for _, e := range []domain.Equipment{{Name: "zz big tin", Sized: true}, {Name: "zz whisk", Aliases: []string{"zz balloon whisk"}}} {
		if _, err := cat.CreateEquipment(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	w := doEquipment(cat, true, http.MethodGet, "/", nil)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", w.Code, body)
	}
	for _, want := range []string{"zz big tin", "zz whisk", "zz balloon whisk", "Clear sized on zz big tin", "Mark zz whisk sized"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestOperatorSetsAndClearsSized(t *testing.T) {
	cat := newEquipmentFixture(t)
	ctx := context.Background()
	tin, err := cat.CreateEquipment(ctx, domain.Equipment{Name: "zz loaf tin", Aliases: []string{"zz bread tin"}, Reviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	target := "/" + strconv.FormatInt(tin.ID, 10) + "/sized"

	if w := doEquipment(cat, true, http.MethodPost, target, url.Values{"sized": {"1"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("set = %d\n%s", w.Code, w.Body)
	}
	got, err := cat.GetEquipment(ctx, tin.ID)
	if err != nil || !got.Sized {
		t.Fatalf("after set = %+v err %v, want sized", got, err)
	}
	// Setting the flag changes nothing else about the row.
	if got.Name != tin.Name || !got.Reviewed || len(got.Aliases) != 1 {
		t.Errorf("set altered the row: %+v", got)
	}

	if w := doEquipment(cat, true, http.MethodPost, target, url.Values{"sized": {"0"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("clear = %d\n%s", w.Code, w.Body)
	}
	if got, err = cat.GetEquipment(ctx, tin.ID); err != nil || got.Sized {
		t.Fatalf("after clear = %+v err %v, want unsized", got, err)
	}
}

func TestOnlyAnOperatorMaySetSized(t *testing.T) {
	cat := newEquipmentFixture(t)
	ctx := context.Background()
	tin, err := cat.CreateEquipment(ctx, domain.Equipment{Name: "zz member tin"})
	if err != nil {
		t.Fatal(err)
	}
	target := "/" + strconv.FormatInt(tin.ID, 10) + "/sized"
	if w := doEquipment(cat, false, http.MethodPost, target, url.Values{"sized": {"1"}}); w.Code != http.StatusForbidden {
		t.Errorf("member set = %d, want 403", w.Code)
	}
	if w := doEquipment(cat, false, http.MethodGet, "/", nil); w.Code != http.StatusForbidden {
		t.Errorf("member list = %d, want 403", w.Code)
	}
	if got, err := cat.GetEquipment(ctx, tin.ID); err != nil || got.Sized {
		t.Errorf("a member changed the flag: %+v err %v", got, err)
	}
}

func TestSizedRequestsAreValidated(t *testing.T) {
	cat := newEquipmentFixture(t)
	ctx := context.Background()
	tin, err := cat.CreateEquipment(ctx, domain.Equipment{Name: "zz valid tin"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(tin.ID, 10)
	for _, c := range []struct {
		name, target string
		form         url.Values
		want         int
	}{
		{"no value", "/" + id + "/sized", url.Values{}, http.StatusBadRequest},
		{"junk value", "/" + id + "/sized", url.Values{"sized": {"yes"}}, http.StatusBadRequest},
		{"bad id", "/abc/sized", url.Values{"sized": {"1"}}, http.StatusBadRequest},
		{"missing equipment", "/999999/sized", url.Values{"sized": {"1"}}, http.StatusNotFound},
	} {
		if w := doEquipment(cat, true, http.MethodPost, c.target, c.form); w.Code != c.want {
			t.Errorf("%s: status = %d, want %d", c.name, w.Code, c.want)
		}
	}
	if got, err := cat.GetEquipment(ctx, tin.ID); err != nil || got.Sized {
		t.Errorf("a rejected request changed the flag: %+v err %v", got, err)
	}
}
