package catalog

import (
	"context"
	"errors"
	"testing"
)

func TestIngredientUsageSumsEveryRegisteredCounter(t *testing.T) {
	s := newTestStore(t)
	u := NewUsage(s)
	in := mustIngredient(t, s, Ingredient{Name: "zz usage thing", Aisle: "test"})

	if n, err := u.IngredientUsage(context.Background(), in.ID); err != nil || n != 0 {
		t.Fatalf("with no counter registered: got %d, %v; want 0, nil", n, err)
	}

	var asked []int64
	u.OnIngredientUsage(func(_ context.Context, id int64) (int64, error) { asked = append(asked, id); return 3, nil })
	u.OnIngredientUsage(func(context.Context, int64) (int64, error) { return 4, nil })

	n, err := u.IngredientUsage(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("IngredientUsage: %v", err)
	}
	if n != 7 {
		t.Errorf("usage = %d, want the sum 7", n)
	}
	if len(asked) != 1 || asked[0] != in.ID {
		t.Errorf("counter asked about %v, want [%d]", asked, in.ID)
	}
}

func TestIngredientUsageOfMissingIngredientIsNotFound(t *testing.T) {
	s := newTestStore(t)
	u := NewUsage(s)
	u.OnIngredientUsage(func(context.Context, int64) (int64, error) { return 1, nil })
	if _, err := u.IngredientUsage(context.Background(), 987654); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestIngredientUsageReportsCounterFailure(t *testing.T) {
	s := newTestStore(t)
	u := NewUsage(s)
	in := mustIngredient(t, s, Ingredient{Name: "zz usage fail", Aisle: "test"})
	boom := errors.New("boom")
	u.OnIngredientUsage(func(context.Context, int64) (int64, error) { return 0, boom })
	if _, err := u.IngredientUsage(context.Background(), in.ID); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the counter's error", err)
	}
}
