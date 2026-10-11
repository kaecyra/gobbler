package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/source.html")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSnapshotStoredUnalteredAndOncePerKind(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	src := readFixture(t)

	if err := s.AddSnapshot(ctx, id, SnapshotHTML, src); err != nil {
		t.Fatalf("AddSnapshot: %v", err)
	}
	got, err := s.GetSnapshot(ctx, id, SnapshotHTML)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Content, src) {
		t.Errorf("content altered:\nwant %q\n got %q", src, got.Content)
	}
	sum := sha256.Sum256(src)
	if got.SHA256 != hex.EncodeToString(sum[:]) || got.CreatedBy == "" || got.CreatedAt.IsZero() || got.Kind != SnapshotHTML {
		t.Errorf("snapshot metadata = %+v", got)
	}

	err = s.AddSnapshot(ctx, id, SnapshotHTML, []byte("replacement"))
	if !errors.Is(err, ErrSnapshotExists) {
		t.Fatalf("second AddSnapshot = %v, want ErrSnapshotExists", err)
	}
	again, _ := s.GetSnapshot(ctx, id, SnapshotHTML)
	if !bytes.Equal(again.Content, src) {
		t.Error("a second add replaced the stored snapshot")
	}

	// A different kind of the same ingest is its own snapshot.
	if err := s.AddSnapshot(ctx, id, SnapshotLLMResponse, []byte(`{"name":"x"}`)); err != nil {
		t.Fatal(err)
	}
	kinds, err := s.ListSnapshotKinds(ctx, id)
	if err != nil || len(kinds) != 2 {
		t.Errorf("kinds = %v, %v", kinds, err)
	}
	if err := s.AddSnapshot(ctx, id, "bogus", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("bogus kind = %v, want ErrInvalid", err)
	}
	if err := s.AddSnapshot(ctx, id+50, SnapshotText, []byte("t")); !errors.Is(err, ErrNotFound) {
		t.Errorf("snapshot of missing recipe = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSnapshot(ctx, id, SnapshotOCR); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSnapshot(missing kind) = %v, want ErrNotFound", err)
	}

	// Editing and restoring the recipe never touches the snapshot.
	if err := s.Update(ctx, id, simpleRecipe("edited")); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	final, _ := s.GetSnapshot(ctx, id, SnapshotHTML)
	if !bytes.Equal(final.Content, src) {
		t.Error("snapshot changed across edit and restore")
	}
}

// The database refuses to change or remove a snapshot even for code that
// bypasses the Go API; deleting the recipe still removes its snapshots.
func TestSnapshotsAreImmutableInTheDatabase(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddSnapshot(ctx, id, SnapshotText, []byte("original")); err != nil {
		t.Fatal(err)
	}

	for name, stmt := range map[string]string{
		"update content": `UPDATE source_snapshots SET content = 'tampered'`,
		"update kind":    `UPDATE source_snapshots SET kind = 'html'`,
		"delete":         `DELETE FROM source_snapshots`,
	} {
		_, err := d.Exec(stmt)
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Errorf("%s: err = %v, want an immutability error", name, err)
		}
	}
	got, err := s.GetSnapshot(ctx, id, SnapshotText)
	if err != nil || string(got.Content) != "original" {
		t.Fatalf("snapshot after tampering = %q, %v", got.Content, err)
	}

	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete recipe with snapshot: %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots`); n != 0 {
		t.Errorf("snapshots left after recipe delete: %d", n)
	}
}

// TestSnapshotAPIHasNoMutator fails when a method that could change or remove
// a snapshot is added to the store: the exported snapshot surface is add and
// read only (ADR-00004 obligation 4).
func TestSnapshotAPIHasNoMutator(t *testing.T) {
	allowed := map[string]bool{"AddSnapshot": true, "GetSnapshot": true, "ListSnapshotKinds": true}
	typ := reflect.TypeOf(&Store{})
	var found int
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if !strings.Contains(strings.ToLower(name), "snapshot") {
			continue
		}
		found++
		if !allowed[name] {
			t.Errorf("exported snapshot method %s is not on the add/read allow-list", name)
		}
	}
	if found != len(allowed) {
		t.Errorf("found %d snapshot methods, want the %d allowed ones", found, len(allowed))
	}
}

// INSERT OR REPLACE and REPLACE INTO delete the conflicting row without firing
// delete triggers, so they must be refused on insert.
func TestSnapshotCannotBeReplacedThroughTheDatabase(t *testing.T) {
	s, d := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddSnapshot(ctx, id, SnapshotText, []byte("original")); err != nil {
		t.Fatal(err)
	}

	for name, stmt := range map[string]string{
		"insert or replace": `INSERT OR REPLACE INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (?, 'text', 'TAMPERED', 'x', 'now', 'me')`,
		"replace into":      `REPLACE INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (?, 'text', 'TAMPERED', 'x', 'now', 'me')`,
	} {
		if _, err := d.Exec(stmt, id); err == nil {
			t.Errorf("%s: succeeded, want it refused", name)
		}
		got, err := s.GetSnapshot(ctx, id, SnapshotText)
		if err != nil || string(got.Content) != "original" {
			t.Fatalf("%s: snapshot = %q, %v; want the original", name, got.Content, err)
		}
	}
}

func TestListSnapshotKindsOfMissingRecipe(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	if _, err := s.ListSnapshotKinds(ctx, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListSnapshotKinds(missing) = %v, want ErrNotFound", err)
	}
	id, err := s.Create(ctx, simpleRecipe("x"))
	if err != nil {
		t.Fatal(err)
	}
	if kinds, err := s.ListSnapshotKinds(ctx, id); err != nil || len(kinds) != 0 {
		t.Errorf("ListSnapshotKinds(no snapshots) = %v, %v; want empty", kinds, err)
	}
}
