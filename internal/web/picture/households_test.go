package picture_test

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	domain "github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/web/picture"
)

func TestHouseholdDeleteRemovesThePictureObjects(t *testing.T) {
	e := newEnv(t)
	_, key := e.recipeWithPicture(t)
	proposal := e.draftWithCandidate(t)
	cs, err := e.svc.Candidates(e.own, proposal)
	if err != nil || len(cs) != 1 {
		t.Fatal(cs, err)
	}

	hs := picture.Households{Store: e.hs, Reaper: domain.NewReaper(e.m, nil)}
	h, _ := household.FromContext(e.own)
	if err := hs.Delete(e.own, h.ID, auth.DevEmail, "One"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	for _, k := range []string{key, cs[0].Key} {
		if _, err := e.m.Get(e.own, k); !errors.Is(err, media.ErrNotFound) {
			t.Errorf("object %q after the household was deleted: err = %v, want it gone", k, err)
		}
	}
}
