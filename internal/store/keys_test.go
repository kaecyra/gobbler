package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/db/dbtest"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func lineOf(raw string) recipe.IngredientLine {
	return recipe.IngredientLine{Rule: quantity.RuleLinear, Raw: raw}
}

// keysOf lists every step and line key of r.
func keysOf(r recipe.Recipe) (steps, lines []string) {
	for _, c := range r.Components {
		for _, l := range c.Lines {
			lines = append(lines, l.Key)
		}
		for _, s := range c.Steps {
			steps = append(steps, string(s.ID))
		}
	}
	return steps, lines
}

// entropyFor returns a key source whose first keys are the given ones, then
// real randomness: what a store that has no memory of removed keys would mint.
func entropyFor(keys ...string) io.Reader {
	var b []byte
	for _, key := range keys {
		for _, c := range key[strings.IndexByte(key, '-')+1:] {
			b = append(b, byte(strings.IndexRune(recipe.KeyAlphabet, c)))
		}
	}
	return io.MultiReader(bytes.NewReader(b), rand.Reader)
}

// ADR-00035 obligation 2: the store gives every step and line a key in the
// format, a key survives an edit, and a key a removed element held is never
// given to a new one, even when the source of randomness offers it.
func TestKeysAreAssignedKeptAndNeverReused(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	r := recipe.Recipe{Name: "Keys", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{lineOf("a"), lineOf("b")},
		Steps: []recipe.Step{{ID: "one", Text: "First."}, {ID: "two", Text: "Second.", DependsOn: []recipe.StepID{"one"}}},
	}}}
	id, err := s.Create(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	steps, lines := keysOf(got)
	for _, k := range steps {
		if !recipe.IsStepKey(recipe.StepID(k)) {
			t.Errorf("step key %q is not in the stp- format", k)
		}
	}
	for _, k := range lines {
		if !recipe.IsLineKey(k) {
			t.Errorf("line key %q is not in the lin- format", k)
		}
	}
	if dep := got.Components[0].Steps[1].DependsOn; len(dep) != 1 || dep[0] != got.Components[0].Steps[0].ID {
		t.Fatalf("dependency %v does not name the first step's new key %s", dep, got.Components[0].Steps[0].ID)
	}

	// Remove the first step and the first line, add one of each, and make the
	// randomness offer exactly the removed keys.
	removedStep, removedLine := steps[0], lines[0]
	edit := got
	edit.Components = []recipe.Component{{
		Lines: []recipe.IngredientLine{got.Components[0].Lines[1], lineOf("new line")},
		Steps: []recipe.Step{{ID: got.Components[0].Steps[1].ID, Text: "Second."}, {ID: "fresh", Text: "New."}},
	}}
	old := keyEntropy
	t.Cleanup(func() { keyEntropy = old })
	// Lines are minted before steps.
	keyEntropy = entropyFor(removedLine, removedStep)
	if err := s.Update(ctx, id, edit); err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	newSteps, newLines := keysOf(after)
	if newSteps[0] != steps[1] || newLines[0] != lines[1] {
		t.Errorf("kept elements changed keys: steps %v -> %v, lines %v -> %v", steps, newSteps, lines, newLines)
	}
	if newSteps[1] == removedStep || newSteps[1] == steps[1] || !recipe.IsStepKey(recipe.StepID(newSteps[1])) {
		t.Errorf("the new step's key %q reuses a key the recipe held (removed %q)", newSteps[1], removedStep)
	}
	if newLines[1] == removedLine || newLines[1] == lines[1] || !recipe.IsLineKey(newLines[1]) {
		t.Errorf("the new line's key %q reuses a key the recipe held (removed %q)", newLines[1], removedLine)
	}
}

// A writer may not choose a key's shape or hand two elements one key.
func TestKeysRefusedWhenMalformedOrShared(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	bad := recipe.Recipe{Name: "x", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Key: "stp-abcdefgh", Rule: quantity.RuleLinear, Raw: "a"}},
		Steps: []recipe.Step{{ID: "s1"}},
	}}}
	if _, err := s.Create(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("a step key on a line = %v, want ErrInvalid", err)
	}
	dup := recipe.Recipe{Name: "x", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{{Key: "lin-abcdefgh", Rule: quantity.RuleLinear, Raw: "a"}, {Key: "lin-abcdefgh", Rule: quantity.RuleLinear, Raw: "b"}},
		Steps: []recipe.Step{{ID: "s1"}},
	}}}
	if _, err := s.Create(ctx, dup); !errors.Is(err, ErrInvalid) {
		t.Errorf("one line key on two lines = %v, want ErrInvalid", err)
	}
	twice := recipe.Recipe{Name: "x", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{lineOf("a")},
		Steps: []recipe.Step{{ID: "s1"}, {ID: "s1"}},
	}}}
	if _, err := s.Create(ctx, twice); err == nil {
		t.Error("two steps with one label were stored")
	}
}

// ADR-00035 obligation 4: a revision document carries every step and line key,
// and a restore brings them back.
func TestKeysRoundTripThroughRevisionAndRestore(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, recipe.Recipe{Name: "v1", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{lineOf("a"), lineOf("b")},
		Steps: []recipe.Step{{ID: "x", Text: "One."}, {ID: "y", Text: "Two.", DependsOn: []recipe.StepID{"x"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	v2 := recipe.Recipe{Name: "v2", Components: []recipe.Component{{Lines: []recipe.IngredientLine{lineOf("c")}, Steps: []recipe.Step{{ID: "z", Text: "Only."}}}}}
	if err := s.Update(ctx, id, v2); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	wantSteps, wantLines := keysOf(v1)
	gotSteps, gotLines := keysOf(old)
	if strings.Join(gotSteps, ",") != strings.Join(wantSteps, ",") || strings.Join(gotLines, ",") != strings.Join(wantLines, ",") {
		t.Errorf("revision keys steps %v lines %v, want %v and %v", gotSteps, gotLines, wantSteps, wantLines)
	}
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	rs, rl := keysOf(restored)
	if strings.Join(rs, ",") != strings.Join(wantSteps, ",") || strings.Join(rl, ",") != strings.Join(wantLines, ",") {
		t.Errorf("restored keys steps %v lines %v, want %v and %v", rs, rl, wantSteps, wantLines)
	}
	if dep := restored.Components[0].Steps[1].DependsOn; len(dep) != 1 || string(dep[0]) != wantSteps[0] {
		t.Errorf("restored dependency = %v, want the first step's key %s", dep, wantSteps[0])
	}
}

// ADR-00035 obligation 2 (migration): rows that exist before the migration get
// keys in the new format, dependencies still resolve, and the keys are in the
// record of keys the recipe has held.
func TestMigrationGivesExistingRowsKeys(t *testing.T) {
	ctx := context.Background()
	d := openRaw(t)
	if err := db.Migrate(ctx, d, dbtest.MigrationsBelow("99999")); err != nil {
		t.Fatal(err)
	}
	h, err := household.New(d).Create(ctx, "First", auth.DevEmail)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO recipes (id, household_id, name, created_at, created_by, updated_at, updated_by) VALUES (7, 1, 'Stew', 't', 'a', 't', 'a')`,
		`INSERT INTO components (id, household_id, recipe_id, position, name) VALUES (3, 1, 7, 0, '')`,
		`INSERT INTO ingredient_lines (id, household_id, component_id, position, rule, raw) VALUES (4, 1, 3, 0, 'linear', 'water')`,
		`INSERT INTO ingredient_lines (id, household_id, component_id, position, rule, raw) VALUES (5, 1, 3, 1, 'linear', 'salt')`,
		`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text) VALUES (5, 1, 7, 3, 0, 's1', 'Boil.')`,
		`INSERT INTO steps (id, household_id, recipe_id, component_id, position, step_key, text) VALUES (6, 1, 7, 3, 1, 's2', 'Serve.')`,
		`INSERT INTO step_dependencies (household_id, step_id, position, depends_on_step_id) VALUES (1, 6, 0, 5)`,
		`INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by) VALUES (1, 7, 'text', x'6162', 'h', 't', 'a')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("migrate over recipe rows: %v", err)
	}
	s := New(d)
	got, err := s.Get(household.WithContext(authedCtx(t), h), 7)
	if err != nil {
		t.Fatal(err)
	}
	steps, lines := keysOf(got)
	if len(steps) != 2 || len(lines) != 2 {
		t.Fatalf("steps %v lines %v, want 2 each", steps, lines)
	}
	for _, k := range steps {
		if !recipe.IsStepKey(recipe.StepID(k)) {
			t.Errorf("migrated step key %q is not in the format", k)
		}
	}
	for _, k := range lines {
		if !recipe.IsLineKey(k) {
			t.Errorf("migrated line key %q is not in the format", k)
		}
	}
	if steps[0] == steps[1] || lines[0] == lines[1] {
		t.Errorf("migrated keys repeat: %v %v", steps, lines)
	}
	if dep := got.Components[0].Steps[1].DependsOn; len(dep) != 1 || string(dep[0]) != steps[0] {
		t.Errorf("dependency after migration = %v, want %s", dep, steps[0])
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipe_keys WHERE recipe_id = 7`); n != 4 {
		t.Errorf("recipe_keys holds %d keys for the recipe, want 4", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = 7`); n != 1 {
		t.Errorf("the migrated snapshot kept %d rows, want 1", n)
	}
	if rows, err := d.Query(`PRAGMA foreign_key_check`); err != nil {
		t.Fatal(err)
	} else {
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			t.Error("foreign_key_check reports violations after the migration")
		}
	}
}

// A key the recipe held, whose element has been removed, cannot come back on an
// element of an ordinary edit (a crafted form could try); a restore of the
// version that held it may bring it back, on the element that held it.
func TestARemovedKeyCannotComeBackOnAnEdit(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, recipe.Recipe{Name: "v1", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{lineOf("a"), lineOf("b")},
		Steps: []recipe.Step{{ID: "x", Text: "One."}, {ID: "y", Text: "Two."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := s.Get(ctx, id)
	steps, lines := keysOf(v1)
	// Remove the second step and second line.
	v2 := v1
	v2.Components = []recipe.Component{{Lines: v1.Components[0].Lines[:1], Steps: v1.Components[0].Steps[:1]}}
	if err := s.Update(ctx, id, v2); err != nil {
		t.Fatal(err)
	}

	stepBack := v2
	stepBack.Components = []recipe.Component{{Lines: v2.Components[0].Lines, Steps: []recipe.Step{v2.Components[0].Steps[0], {ID: recipe.StepID(steps[1]), Text: "Imposter."}}}}
	if err := s.Update(ctx, id, stepBack); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), steps[1]) {
		t.Errorf("a removed step's key on a new step = %v, want ErrInvalid naming the key", err)
	}
	lineBack := v2
	lineBack.Components = []recipe.Component{{Lines: []recipe.IngredientLine{v2.Components[0].Lines[0], {Key: lines[1], Rule: quantity.RuleLinear, Raw: "imposter"}}, Steps: v2.Components[0].Steps}}
	if err := s.Update(ctx, id, lineBack); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), lines[1]) {
		t.Errorf("a removed line's key on a new line = %v, want ErrInvalid naming the key", err)
	}
	if cur, _ := s.Info(ctx, id); cur.Version != 2 {
		t.Errorf("a refused edit left version %d, want 2", cur.Version)
	}
	// A restore of version 1 puts the elements back with their keys.
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatalf("restore: %v", err)
	}
	back, _ := s.Get(ctx, id)
	if gs, gl := keysOf(back); strings.Join(gs, ",") != strings.Join(steps, ",") || strings.Join(gl, ",") != strings.Join(lines, ",") {
		t.Errorf("restored keys %v %v, want %v %v", gs, gl, steps, lines)
	}
}
