package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestUpdateIfVersionRejectsStaleSecondSave(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("original"))
	if err != nil {
		t.Fatal(err)
	}
	// Two loads of the same version, as two open editors would hold.
	a, err := s.Info(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Info(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateIfVersion(ctx, id, a.Version, simpleRecipe("first")); err != nil {
		t.Fatalf("first save: %v", err)
	}
	revisions := count(t, d, `SELECT COUNT(*) FROM revisions`)

	err = s.UpdateIfVersion(ctx, id, b.Version, simpleRecipe("second"))
	if !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("second save = %v, want ErrStaleVersion", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "first" {
		t.Errorf("name = %q, the rejected save changed the recipe", got.Name)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revisions`); n != revisions {
		t.Errorf("revisions = %d, want %d: a rejected save wrote a revision", n, revisions)
	}
	if in, _ := s.Info(ctx, id); in.Version != a.Version+1 {
		t.Errorf("version = %d, want %d", in.Version, a.Version+1)
	}
}

func cyclicRecipe() recipe.Recipe {
	r := simpleRecipe("Loop")
	r.Components[0].Steps = []recipe.Step{
		{ID: "a", Text: "A", DependsOn: []recipe.StepID{"b"}},
		{ID: "b", Text: "B", DependsOn: []recipe.StepID{"a"}},
	}
	return r
}

func TestUpdateIfVersionMissingRecipeAndInvalidDocument(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	if err := s.UpdateIfVersion(ctx, 99, 1, simpleRecipe("x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing recipe = %v, want ErrNotFound", err)
	}
	id, _ := s.Create(ctx, simpleRecipe("x"))
	if err := s.UpdateIfVersion(ctx, id, 1, cyclicRecipe()); err == nil || errors.Is(err, ErrStaleVersion) {
		t.Errorf("invalid document = %v, want a validation error", err)
	}
	if err := s.UpdateIfVersion(context.Background(), id, 1, simpleRecipe("y")); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("no identity = %v, want ErrNoIdentity", err)
	}
}

// Of racing saves from the same version exactly one wins.
func TestUpdateIfVersionConcurrentSavesOneWins(t *testing.T) {
	s, _ := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("shared"))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.UpdateIfVersion(ctx, id, 1, simpleRecipe(fmt.Sprintf("w%d", w)))
		}()
	}
	wg.Wait()
	close(results)
	var won, stale int
	for err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrStaleVersion):
			stale++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 || stale != workers-1 {
		t.Errorf("won = %d, stale = %d; want 1 and %d", won, stale, workers-1)
	}
}

func TestCreateWithSnapshotsStoresBoth(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	src := []byte("<html>exact bytes \x00\xff</html>")
	id, err := s.CreateWithSnapshots(ctx, simpleRecipe("x"),
		SnapshotInput{Kind: SnapshotHTML, Content: src},
		SnapshotInput{Kind: SnapshotLLMResponse, Content: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSnapshot(ctx, id, SnapshotHTML)
	if err != nil || !bytes.Equal(got.Content, src) {
		t.Errorf("html snapshot = %v, %v", got.Content, err)
	}
	if kinds, err := s.ListSnapshotKinds(ctx, id); err != nil || len(kinds) != 2 {
		t.Errorf("kinds = %v, %v", kinds, err)
	}
	if in, _ := s.Info(ctx, id); in.Version != 1 {
		t.Errorf("version = %d, want 1", in.Version)
	}
}

// A snapshot that cannot be written takes the recipe down with it: nothing is
// left behind to compensate for.
func TestCreateWithSnapshotsWritesNothingOnFailure(t *testing.T) {
	cases := map[string][]SnapshotInput{
		"duplicate kind": {{Kind: SnapshotText, Content: []byte("a")}, {Kind: SnapshotText, Content: []byte("b")}},
		"bogus kind":     {{Kind: SnapshotText, Content: []byte("a")}, {Kind: "bogus"}},
	}
	wantErr := map[string]error{"duplicate kind": ErrSnapshotExists, "bogus kind": ErrInvalid}
	for name, snaps := range cases {
		t.Run(name, func(t *testing.T) {
			s, d := newTestStore(t)
			ctx := authedCtx(t)
			_, err := s.CreateWithSnapshots(ctx, simpleRecipe("x"), snaps...)
			if !errors.Is(err, wantErr[name]) {
				t.Fatalf("err = %v, want %v", err, wantErr[name])
			}
			for _, table := range []string{"recipes", "source_snapshots"} {
				if n := count(t, d, `SELECT COUNT(*) FROM `+table); n != 0 {
					t.Errorf("%s rows = %d, want 0", table, n)
				}
			}
		})
	}
}

func TestCreateWithSnapshotsRejectsInvalidRecipeAndNoIdentity(t *testing.T) {
	s, d := newTestStore(t)
	if _, err := s.CreateWithSnapshots(authedCtx(t), cyclicRecipe(), SnapshotInput{Kind: SnapshotText}); err == nil {
		t.Error("invalid recipe accepted")
	}
	if _, err := s.CreateWithSnapshots(context.Background(), simpleRecipe("x"), SnapshotInput{Kind: SnapshotText}); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("no identity = %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("recipes = %d, want 0", n)
	}
}

// A delete outside the writer queue contends with the queued writers and can
// starve past busy_timeout (#104).
func TestConcurrentDeletesAndEditsDoNotFailBusy(t *testing.T) {
	s, _ := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	ctx := authedCtx(t)
	shared, err := s.Create(ctx, simpleRecipe("shared"))
	if err != nil {
		t.Fatal(err)
	}
	const victims = 150
	ids := make(chan int64, victims)
	for i := 0; i < victims; i++ {
		id, err := s.Create(ctx, simpleRecipe(fmt.Sprintf("victim%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		ids <- id
	}
	close(ids)

	errs := make(chan error, 4096)
	var editing, deleting sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 12; w++ {
		editing.Add(1)
		go func() {
			defer editing.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := s.Update(ctx, shared, simpleRecipe(fmt.Sprintf("w%d-%d", w, i))); err != nil {
					errs <- fmt.Errorf("Update: %w", err)
					return
				}
			}
		}()
	}
	for w := 0; w < 6; w++ {
		deleting.Add(1)
		go func() {
			defer deleting.Done()
			for id := range ids {
				if err := s.Delete(ctx, id); err != nil {
					errs <- fmt.Errorf("Delete: %w", err)
				}
			}
		}()
	}
	deleting.Wait()
	close(stop)
	editing.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
