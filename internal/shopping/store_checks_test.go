package shopping

import (
	"errors"
	"sync"
	"testing"
)

func TestRegenerateKeepsChecksOfItemsThatStillExist(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"), f.ing(f.butter, 2, 1, "tbsp"), f.ing(f.egg, 2, 1, ""))
	f.addEntry(l, r, 1)
	f.check(l, ingKey(f.flour), true)
	f.check(l, ingKey(f.butter), true)

	v := f.regenerate(l)
	if items := viewItems(v); !items["zz test flour"].Checked || !items["zz test butter"].Checked || items["zz test egg"].Checked {
		t.Fatalf("before the edit: checked = flour %v butter %v egg %v", items["zz test flour"].Checked, items["zz test butter"].Checked, items["zz test egg"].Checked)
	}

	// The recipe changes: flour doubles, butter goes, salt arrives.
	f.updateRecipe(r, "Cake", f.ing(f.flour, 2, 1, "cup"), f.ing(f.egg, 2, 1, ""), f.ing(f.salt, 1, 1, "tsp"))
	v = f.regenerate(l)
	items := viewItems(v)
	if flour := items["zz test flour"]; !flour.Checked {
		t.Error("flour lost its check although it is still on the list")
	} else {
		wantAmounts(t, flour.Item, "2 cup")
	}
	if _, ok := items["zz test butter"]; ok {
		t.Error("butter is still listed")
	}
	if items["zz test salt"].Checked {
		t.Error("new item arrived checked")
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE item_key = ?`, string(ingKey(f.butter))); n != 0 {
		t.Errorf("check of the vanished item was kept (%d rows)", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE item_key = ?`, string(ingKey(f.flour))); n != 1 {
		t.Errorf("check of the surviving item rows = %d, want 1", n)
	}

	// Butter coming back starts unchecked: its state was dropped, not hidden.
	f.updateRecipe(r, "Cake", f.ing(f.flour, 2, 1, "cup"), f.ing(f.butter, 2, 1, "tbsp"))
	if viewItems(f.regenerate(l))["zz test butter"].Checked {
		t.Error("butter came back checked")
	}
}

// Load alone never discards state: it only ignores what has no item.
func TestLoadDoesNotDropChecks(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"), f.ing(f.butter, 1, 1, "tbsp"))
	f.addEntry(l, r, 1)
	f.check(l, ingKey(f.butter), true)
	f.updateRecipe(r, "Cake", f.ing(f.flour, 1, 1, "cup"))
	f.load(l)
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks`); n != 1 {
		t.Errorf("check rows after Load = %d, want 1", n)
	}
}

func TestCheckSurvivesAmountChange(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	e := f.addEntry(l, f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup")), 1)
	f.check(l, ingKey(f.flour), true)
	if err := f.s.SetEntryMultiplier(f.ctx, e, quantity3(t)); err != nil {
		t.Fatal(err)
	}
	flour := viewItems(f.regenerate(l))["zz test flour"]
	wantAmounts(t, flour.Item, "3 cup")
	if !flour.Checked {
		t.Error("flour lost its check when its amount changed")
	}
}

func TestManualItemCheckFollowsTheItem(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	id, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "Paper Towels"})
	if err != nil {
		t.Fatal(err)
	}
	f.check(l, "t:paper towels", true)
	if !viewItems(f.regenerate(l))["Paper Towels"].Checked {
		t.Fatal("manual item is not checked")
	}
	if err := f.s.RemoveManualItem(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	f.regenerate(l)
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks`); n != 0 {
		t.Errorf("check rows after the item was removed = %d, want 0", n)
	}
}

func TestSetCheckedErrors(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	if err := f.s.SetChecked(f.ctx, l.ID, "bogus", true); !errors.Is(err, ErrInvalid) {
		t.Errorf("malformed key: err = %v, want ErrInvalid", err)
	}
	if err := f.s.SetChecked(f.ctx, 999, "i:1", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing list: err = %v, want ErrNotFound", err)
	}
	if err := f.s.SetChecked(f.ctx, l.ID, "i:1", false); err != nil {
		t.Errorf("clearing what is not set: %v", err)
	}
}

// Two devices act on one item at once: the result is an answer one of them
// asked for, with no errors and no duplicate row.
func TestConcurrentSetCheckedIsConsistent(t *testing.T) {
	f := newFixture(t)
	f.fixedClock() // the test clock is not goroutine-safe
	l := f.newList("L")
	key := ingKey(f.flour)

	run := func(want func(i int) bool) {
		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 8; i++ {
					if err := f.s.SetChecked(f.ctx, l.ID, key, want(g*8+i)); err != nil {
						errs <- err
					}
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("SetChecked: %v", err)
		}
	}

	run(func(int) bool { return true })
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE list_id = ?`, l.ID); n != 1 {
		t.Errorf("rows after concurrent checks = %d, want 1", n)
	}
	run(func(i int) bool { return i%2 == 0 })
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE list_id = ?`, l.ID); n > 1 {
		t.Errorf("rows after mixed set and clear = %d, want at most 1", n)
	}
	run(func(int) bool { return false })
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE list_id = ?`, l.ID); n != 0 {
		t.Errorf("rows after concurrent clears = %d, want 0", n)
	}
}

func TestOnChangeFiresAfterCommitOncePerChange(t *testing.T) {
	f := newFixture(t)
	a, b := f.newList("A"), f.newList("B")
	var got []int64
	f.s.OnChange(func(c Change) { got = append(got, c.ListID) })
	assertGot := func(what string, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: notified %v, want %v", what, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: notified %v, want %v", what, got, want)
			}
		}
		got = nil
	}

	f.check(a, "i:1", true)
	assertGot("check", a.ID)
	f.check(a, "i:1", true)
	assertGot("check again")
	f.check(a, "i:1", false)
	assertGot("uncheck", a.ID)
	f.check(a, "i:1", false)
	assertGot("uncheck again")

	if err := f.s.AddToPantry(f.ctx, 99999); err == nil {
		t.Fatal("expected unknown ingredient")
	}
	assertGot("failed write")
	if err := f.s.AddToPantry(f.ctx, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}
	assertGot("pantry add", a.ID, b.ID)
	if err := f.s.AddToPantry(f.ctx, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}
	assertGot("pantry add again")

	if _, err := f.s.AddManualItem(f.ctx, b.ID, ManualItem{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	assertGot("manual item", b.ID)
}
