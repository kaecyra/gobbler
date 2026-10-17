package ingest

import (
	"reflect"
	"testing"
)

// savedEquipmentNames reads the equipment of a saved recipe back as names.
func savedEquipmentNames(t *testing.T, e *env, id int64) []string {
	t.Helper()
	got, err := e.store.Get(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range got.Equipment {
		eq, err := e.catalog.GetEquipment(e.ctx, int64(l.Equipment))
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, eq.Name)
	}
	return names
}

func TestSaveLinksEquipmentAndCreatesProposalsUnreviewedOnlyOnSave(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatal(err)
	}

	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := []string{"dutch oven", "mandoline slicer"}
	if got := savedEquipmentNames(t, e, saved.RecipeID); !reflect.DeepEqual(got, want) {
		t.Errorf("saved equipment = %q, want %q", got, want)
	}
	if len(saved.NewEquipment) != 1 || saved.NewEquipment[0].Name != "mandoline slicer" || saved.NewEquipment[0].Reviewed {
		t.Errorf("NewEquipment = %+v, want the mandoline, unreviewed", saved.NewEquipment)
	}
	unrev, err := e.catalog.ListUnreviewedEquipment(e.ctx)
	if err != nil || len(unrev) != 1 || unrev[0].Name != "mandoline slicer" {
		t.Fatalf("unreviewed equipment = %+v, err %v", unrev, err)
	}
	if unrev[0].Sized {
		t.Errorf("ingestion must never set sized (ADR-00019)")
	}
}

func TestSaveReusesEquipmentCreatedSinceTheDraft(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.saver().Save(e.ctx, confirm(t, d)); err != nil {
		t.Fatal(err)
	}
	second, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if len(second.NewEquipment) != 0 {
		t.Errorf("second save created %+v; the first save's equipment exists now", second.NewEquipment)
	}
	if got := savedEquipmentNames(t, e, second.RecipeID); len(got) != 2 {
		t.Errorf("saved equipment = %q", got)
	}
}

func TestSaveWritesEachEquipmentOnce(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Toast\n\nIngredients\n2 slices bread\n\nDirections\n1. Heat the oven.\n")
	if err != nil {
		t.Fatal(err)
	}
	// An edited draft can repeat a piece of equipment under two spellings.
	oven := d.Equipment[0]
	d.Equipment = append(d.Equipment, Equipment{Name: "ovens"}, oven)
	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if got := savedEquipmentNames(t, e, saved.RecipeID); !reflect.DeepEqual(got, []string{"oven"}) {
		t.Errorf("saved equipment = %q, want one oven", got)
	}
}

func TestSaveWithoutEquipmentSavesNone(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Toast\n\nIngredients\n2 slices bread\n\nDirections\n1. Toast it.\n")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil || len(got.Equipment) != 0 {
		t.Errorf("equipment = %+v, err %v", got.Equipment, err)
	}
}
