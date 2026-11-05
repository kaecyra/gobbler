package picture

import (
	"context"
	"errors"
	"testing"
)

type fakeKeys struct {
	key string
	ok  bool
	err error
}

func (f fakeKeys) Key(context.Context, int64) (string, bool, error) { return f.key, f.ok, f.err }

func TestSrcsAddressesAPictureByVersion(t *testing.T) {
	got, err := Srcs{Pictures: fakeKeys{key: "recipes/7/abc_D-9.webp", ok: true}}.Src(context.Background(), 7)
	if err != nil || got != "/recipes/7/picture?v=abc_D-9" {
		t.Errorf("Src = %q, %v; want /recipes/7/picture?v=abc_D-9", got, err)
	}
}

func TestSrcsIsEmptyForARecipeWithNoPicture(t *testing.T) {
	got, err := Srcs{Pictures: fakeKeys{}}.Src(context.Background(), 7)
	if err != nil || got != "" {
		t.Errorf("Src = %q, %v; want empty and no error", got, err)
	}
}

func TestSrcsPassesAFailureOn(t *testing.T) {
	boom := errors.New("boom")
	if _, err := (Srcs{Pictures: fakeKeys{err: boom}}).Src(context.Background(), 7); !errors.Is(err, boom) {
		t.Errorf("Src error = %v, want %v", err, boom)
	}
}
