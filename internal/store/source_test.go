package store

import (
	"reflect"
	"testing"
)

func TestSourceAddressDropsOnlyTheFragment(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.test/y":            "https://x.test/y",
		"https://x.test/y#recipe":     "https://x.test/y",
		"https://x.test/y?a=1#r#s":    "https://x.test/y?a=1",
		"HTTPS://X.test/y/":           "HTTPS://X.test/y/",
		"#only":                       "",
		"":                            "",
		"https://x.test/100%25_off#a": "https://x.test/100%25_off",
	} {
		if got := SourceAddress(in); got != want {
			t.Errorf("SourceAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

// RecipesFromSource compares addresses by SourceAddress: a stored fragment is
// dropped, '%' and '_' are plain characters, and only the household's own
// recipes are read.
func TestRecipesFromSourceMatchesTheAddressWithoutItsFragment(t *testing.T) {
	ctx := authedCtx(t)
	s, _ := newTestStore(t)
	create := func(name, url string) int64 {
		t.Helper()
		r := simpleRecipe(name)
		r.SourceURL = url
		id, err := s.Create(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	plain := create("Plain", "https://x.test/100%_off")
	withFragment := create("Fragment", "https://x.test/100%_off#recipe")
	create("Longer", "https://x.test/100%_offer")
	create("Wildcard", "https://x.test/100X_off")
	create("Typed", "")
	other := simpleRecipe("Other household")
	other.SourceURL = "https://x.test/100%_off"
	if _, err := s.Create(otherCtx(t), other); err != nil {
		t.Fatal(err)
	}

	got, err := s.RecipesFromSource(ctx, "https://x.test/100%_off#step-2")
	if err != nil {
		t.Fatal(err)
	}
	want := []SourcedRecipe{{ID: plain, Name: "Plain"}, {ID: withFragment, Name: "Fragment"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RecipesFromSource = %+v, want %+v", got, want)
	}
	for _, addr := range []string{"", "#recipe", "https://x.test/elsewhere"} {
		if got, err := s.RecipesFromSource(ctx, addr); err != nil || len(got) != 0 {
			t.Errorf("RecipesFromSource(%q) = %+v, %v, want nothing", addr, got, err)
		}
	}
	if _, err := s.RecipesFromSource(noHouseholdCtx(t), "https://x.test/100%_off"); err == nil {
		t.Error("RecipesFromSource with no household succeeded")
	}
}
