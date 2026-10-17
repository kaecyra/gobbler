package shopping

import (
	"reflect"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// baseTotals sums amounts per comparable base: volume in ml, mass in g, anything
// else by its own unit name. Only exact amounts are used.
func baseTotals(t *testing.T, as []quantity.Amount) map[string]string {
	t.Helper()
	sums := map[string]quantity.Rat{}
	for _, a := range as {
		key := "other:" + a.Unit.Name
		if a.Unit.Converts() {
			switch a.Unit.Dimension {
			case quantity.DimVolume:
				key, a = convertTo(t, a, "ml")
			case quantity.DimMass:
				key, a = convertTo(t, a, "g")
			}
		}
		if cur, ok := sums[key]; ok {
			sums[key] = cur.Add(a.Qty.Min())
		} else {
			sums[key] = a.Qty.Min()
		}
	}
	out := map[string]string{}
	for k, v := range sums {
		out[k] = v.String()
	}
	return out
}

func convertTo(t *testing.T, a quantity.Amount, unit string) (string, quantity.Amount) {
	t.Helper()
	c, err := quantity.Convert(a, quantity.MustUnit(unit))
	if err != nil {
		t.Fatal(err)
	}
	return unit, c
}

// shareTotals sums every share of it.
func shareTotals(t *testing.T, it Item) map[string]string {
	t.Helper()
	var all []quantity.Amount
	for _, s := range it.Shares {
		all = append(all, s.Amounts...)
	}
	return baseTotals(t, all)
}

func TestSharesSumToMergedAmountAcrossUnitConversion(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idButter, amt(t, 2, 1, "tbsp"))),
		entry(t, 2, "B", 1, line(idButter, amt(t, 1, 2, "cup"))),
	}})
	b := items(l)["butter"]
	wantAmounts(t, b, "10 tbsp")
	if got, want := shareTotals(t, b), baseTotals(t, b.Amounts); !reflect.DeepEqual(got, want) {
		t.Errorf("shares sum to %v, merged is %v", got, want)
	}
	if len(b.Shares) != 2 || b.Shares[0].Source.Name != "A" || b.Shares[1].Source.Name != "B" {
		t.Fatalf("shares = %+v, want one per recipe in entry order", b.Shares)
	}
	if got := b.Shares[0].Amounts[0].String(); got != "2 tbsp" {
		t.Errorf("A's share = %q, want its own 2 tbsp", got)
	}
	if got := b.Shares[1].Amounts[0].String(); got != "1/2 cup" {
		t.Errorf("B's share = %q, want its own 1/2 cup", got)
	}
}

func TestSharesFollowAVolumeToMassConversionOfTheMergedLine(t *testing.T) {
	// 0.5 g/ml: 1 cup of flour is mass-converted because the other recipe gives
	// grams, so the merged line is one mass amount. The cup share must be in
	// grams too, or the shares would not add up to the line.
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idFlour, amt(t, 1, 1, "cup"))),
		entry(t, 2, "B", 1, line(idFlour, amt(t, 100, 1, "g"))),
	}})
	f := items(l)["flour"]
	if len(f.Amounts) != 1 || f.Amounts[0].Unit.Dimension != quantity.DimMass {
		t.Fatalf("merged = %v, want a single mass amount", f.Amounts)
	}
	if got, want := shareTotals(t, f), baseTotals(t, f.Amounts); !reflect.DeepEqual(got, want) {
		t.Errorf("shares sum to %v, merged is %v", got, want)
	}
	for _, s := range f.Shares {
		if len(s.Amounts) != 1 || s.Amounts[0].Unit.Dimension != quantity.DimMass {
			t.Errorf("%s share = %v, want a mass amount", s.Source.Name, s.Amounts)
		}
	}
}

func TestSharesKeepVolumeWhenTheMergedLineStaysVolume(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idFlour, amt(t, 1, 1, "cup"))),
		entry(t, 2, "B", 1, line(idFlour, amt(t, 2, 1, "cup"))),
	}})
	f := items(l)["flour"]
	wantAmounts(t, f, "3 cup")
	if got := f.Shares[0].Amounts[0].String(); got != "1 cup" {
		t.Errorf("A's share = %q, want 1 cup", got)
	}
}

func TestSharesCoverManualAmountsAndRepeatedLines(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{
			entry(t, 1, "A", 2, line(idButter, amt(t, 1, 1, "tbsp")), line(idButter, amt(t, 1, 1, "tbsp"))),
		},
		Manual: []ManualItem{{Ingredient: idButter, Amount: amt(t, 3, 1, "tbsp")}},
	})
	b := items(l)["butter"]
	wantAmounts(t, b, "7 tbsp")
	if len(b.Shares) != 2 {
		t.Fatalf("shares = %+v, want the recipe's and the manual one", b.Shares)
	}
	if got := b.Shares[0].Amounts[0].String(); got != "4 tbsp" || b.Shares[0].Manual {
		t.Errorf("recipe share = %+v, want its two lines merged into 4 tbsp", b.Shares[0])
	}
	if !b.Shares[1].Manual || b.Shares[1].Amounts[0].String() != "3 tbsp" || b.Shares[1].Source != (Source{}) {
		t.Errorf("manual share = %+v, want 3 tbsp with no source", b.Shares[1])
	}
	if got, want := shareTotals(t, b), baseTotals(t, b.Amounts); !reflect.DeepEqual(got, want) {
		t.Errorf("shares sum to %v, merged is %v", got, want)
	}
}

func TestSharesOfAnAmountlessLineCarryNoAmount(t *testing.T) {
	taste := line(idSalt, quantity.Amount{})
	taste.Rule = quantity.RuleToTaste
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, taste)}})
	s := items(l)["salt"].Shares
	if len(s) != 1 || s[0].Source.Name != "A" || len(s[0].Amounts) != 0 {
		t.Errorf("shares = %+v, want A with no amount", s)
	}
}
