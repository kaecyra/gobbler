package store

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

const (
	videoA = "dQw4w9WgXcQ"
	videoB = "9bZkp7q19f0"
)

func withVideo(r recipe.Recipe, id string) recipe.Recipe {
	r.VideoID = id
	return r
}

func videoOf(ctx context.Context, t *testing.T, s *Store, id int64) string {
	t.Helper()
	r, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return r.VideoID
}

// Obligation 2 of ADR-00021: the video ID is part of the recipe revision, so it
// survives an edit, a restore of an earlier revision and a shared copy.
func TestVideoIDSurvivesEditRestoreAndAShare(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)

	id, err := s.Create(a, withVideo(simpleRecipe("Pancakes"), videoA))
	if err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != videoA {
		t.Fatalf("after create: video %q, want %q", got, videoA)
	}

	// An edit that does not touch the video keeps it (the document is replaced
	// whole, so the editor sends the ID back; the store must store what it is given).
	if err := s.Update(a, id, withVideo(simpleRecipe("Pancakes v2"), videoA)); err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != videoA {
		t.Errorf("after an edit that keeps it: video %q, want %q", got, videoA)
	}

	// Changing it is an edit; the revision holds the document as it was.
	if err := s.Update(a, id, withVideo(simpleRecipe("Pancakes v3"), videoB)); err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != videoB {
		t.Errorf("after changing the video: %q, want %q", got, videoB)
	}
	old, err := s.GetVersion(a, id, 2)
	if err != nil || old.VideoID != videoA {
		t.Errorf("version 2 = video %q (%v), want %q: a revision holds the video it replaced", old.VideoID, err, videoA)
	}

	// Removing it, then restoring the revision that had it.
	if err := s.Update(a, id, simpleRecipe("Pancakes v4")); err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != "" {
		t.Fatalf("after removing the video: %q, want none", got)
	}
	if err := s.Restore(a, id, 3); err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != videoB {
		t.Errorf("after restoring version 3: video %q, want %q", got, videoB)
	}
	if err := s.Restore(a, id, 1); err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, id); got != videoA {
		t.Errorf("after restoring version 1: video %q, want %q", got, videoA)
	}

	// A clone and a shared copy carry it.
	cloneID, err := s.Clone(a, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := videoOf(a, t, s, cloneID); got != videoA {
		t.Errorf("clone: video %q, want %q", got, videoA)
	}
	shareID, err := s.SendShare(a, id, otherHouseholdID)
	if err != nil {
		t.Fatal(err)
	}
	copyID, err := s.AcceptShare(b, shareID, fixed(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got := videoOf(b, t, s, copyID); got != videoA {
		t.Errorf("shared copy: video %q, want %q", got, videoA)
	}
}

func TestVideoIDMustBeWellFormed(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	for _, bad := range []string{"short", "waytoolongforavideoid", "dQw4w9WgXc!", " dQw4w9WgXcQ"} {
		if _, err := s.Create(a, withVideo(simpleRecipe("Bad"), bad)); !errors.Is(err, recipe.ErrInvalidVideoID) {
			t.Errorf("Create with video %q = %v, want ErrInvalidVideoID", bad, err)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("a rejected video left %d recipes", n)
	}
}

// Obligation 3 of ADR-00021 (the table part): thumbnails are rows keyed by
// video ID, one per ID, read only through a recipe of the household.
func TestThumbnailIsKeptOncePerVideoAndReadThroughARecipe(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	one, err := s.Create(a, withVideo(simpleRecipe("One"), videoA))
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.Create(a, withVideo(simpleRecipe("Two"), videoA))
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.HasThumbnail(a, videoA); err != nil || ok {
		t.Fatalf("HasThumbnail before saving = %v, %v; want false", ok, err)
	}
	if _, err := s.Thumbnail(a, one); !errors.Is(err, ErrNoThumbnail) || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Thumbnail before saving = %v, want ErrNoThumbnail (an ErrNotFound)", err)
	}

	if err := s.SaveThumbnail(a, videoA, []byte("first"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThumbnail(a, videoA, []byte("second"), "image/png"); err != nil {
		t.Fatalf("saving the same video again: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM video_thumbnails`); n != 1 {
		t.Errorf("rows for one video = %d, want 1", n)
	}
	for _, id := range []int64{one, two} {
		got, err := s.Thumbnail(a, id)
		if err != nil || string(got.Content) != "first" || got.ContentType != "image/jpeg" {
			t.Errorf("Thumbnail(%d) = %q %q (%v), want the first download for both recipes", id, got.Content, got.ContentType, err)
		}
	}
	if ok, err := s.HasThumbnail(a, videoA); err != nil || !ok {
		t.Errorf("HasThumbnail after saving = %v, %v; want true", ok, err)
	}
}

// Obligation 4 at the store: another household's recipe finds nothing, even for
// a video ID it names itself and even when the thumbnail row exists.
func TestThumbnailIsNotReachableFromAnotherHousehold(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	mine, err := s.Create(a, withVideo(simpleRecipe("Mine"), videoA))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThumbnail(a, videoA, []byte("img"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thumbnail(b, mine); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household reading my recipe's thumbnail = %v, want ErrNotFound", err)
	}
	// B's own recipe naming a different video sees nothing, and B's recipe
	// naming the same video sees the shared row only through its own recipe.
	theirs, err := s.Create(b, withVideo(simpleRecipe("Theirs"), videoB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thumbnail(b, theirs); !errors.Is(err, ErrNoThumbnail) {
		t.Errorf("a recipe whose video has no thumbnail = %v, want ErrNoThumbnail", err)
	}
	plain, err := s.Create(a, simpleRecipe("No video"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Thumbnail(a, plain); !errors.Is(err, ErrNoThumbnail) {
		t.Errorf("a recipe with no video = %v, want ErrNoThumbnail", err)
	}
}

func TestSaveThumbnailRefusesWhatIsNotAThumbnail(t *testing.T) {
	s, _ := newTestStore(t)
	a := authedCtx(t)
	for name, c := range map[string]struct {
		id      string
		content []byte
		ct      string
	}{
		"bad id":        {"nope", []byte("x"), "image/jpeg"},
		"empty content": {videoA, nil, "image/jpeg"},
		"no type":       {videoA, []byte("x"), ""},
	} {
		if err := s.SaveThumbnail(a, c.id, c.content, c.ct); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: SaveThumbnail = %v, want ErrInvalid", name, err)
		}
	}
	if err := s.SaveThumbnail(context.Background(), videoA, []byte("x"), "image/jpeg"); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("without identity = %v, want ErrNoIdentity", err)
	}
}
