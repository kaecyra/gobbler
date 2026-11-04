package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"slices"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// keyEntropy is where keys come from; a test swaps it to force a collision.
var keyEntropy io.Reader = rand.Reader

// mintKey returns prefix and KeyRandomLen random characters of the key
// alphabet that is not in taken, and adds it to taken. The alphabet has 32
// characters, so each is one byte reduced by a mask without bias.
func mintKey(prefix string, taken map[string]bool) (string, error) {
	for {
		var raw [recipe.KeyRandomLen]byte
		if _, err := io.ReadFull(keyEntropy, raw[:]); err != nil {
			return "", fmt.Errorf("generate key: %w", err)
		}
		b := make([]byte, 0, len(prefix)+len(raw))
		b = append(b, prefix...)
		for _, r := range raw {
			b = append(b, recipe.KeyAlphabet[r&31])
		}
		if k := string(b); !taken[k] {
			taken[k] = true
			return k, nil
		}
	}
}

// assignKeys returns r with a key on every step and ingredient line, ready to
// be written as recipe id (ADR-00035). It does not change r.
//
// A line with no key gets one. A step whose ID is not shaped like a key (empty,
// or a document-local label) gets one, and every dependency and equipment link
// that named the label is rewritten to it. A key already on an element stays.
// A new key is never one the recipe has held (recipe_keys), nor one the
// document holds, so a removed element's key goes to no other element.
//
// id is 0 for a recipe being created, which has held no keys yet.
func assignKeys(ctx context.Context, q *storedb.Queries, hid, id int64, r recipe.Recipe) (recipe.Recipe, error) {
	taken := map[string]bool{}
	if id != 0 {
		held, err := q.ListRecipeKeys(ctx, storedb.ListRecipeKeysParams{RecipeID: id, HouseholdID: hid})
		if err != nil {
			return recipe.Recipe{}, readErr(fmt.Sprintf("keys of recipe %d", id), err)
		}
		for _, k := range held {
			taken[k] = true
		}
	}
	// Keys the document already holds are checked for shape and uniqueness
	// before any is minted, so a minted key cannot be one of them.
	seen := map[string]bool{}
	claim := func(k, what string) error {
		if seen[k] {
			return fmt.Errorf("%w: %s %q is on two elements", ErrInvalid, what, k)
		}
		seen[k] = true
		taken[k] = true
		return nil
	}
	comps := make([]recipe.Component, len(r.Components))
	labels := map[recipe.StepID]recipe.StepID{}
	for ci, c := range r.Components {
		for _, l := range c.Lines {
			if l.Key == "" {
				continue
			}
			if !recipe.IsLineKey(l.Key) {
				return recipe.Recipe{}, fmt.Errorf("%w: %q is not an ingredient line key", ErrInvalid, l.Key)
			}
			if err := claim(l.Key, "line key"); err != nil {
				return recipe.Recipe{}, err
			}
		}
		for _, s := range c.Steps {
			if recipe.IsStepKey(s.ID) {
				if err := claim(string(s.ID), "step key"); err != nil {
					return recipe.Recipe{}, err
				}
			}
		}
		comps[ci] = recipe.Component{Name: c.Name, Lines: slices.Clone(c.Lines), Steps: slices.Clone(c.Steps)}
	}
	for ci := range comps {
		for li := range comps[ci].Lines {
			l := &comps[ci].Lines[li]
			if l.Key != "" {
				continue
			}
			k, err := mintKey(recipe.LineKeyPrefix, taken)
			if err != nil {
				return recipe.Recipe{}, err
			}
			l.Key = k
		}
		for si := range comps[ci].Steps {
			s := &comps[ci].Steps[si]
			if recipe.IsStepKey(s.ID) {
				continue
			}
			if s.ID != "" {
				if _, dup := labels[s.ID]; dup {
					return recipe.Recipe{}, fmt.Errorf("%w: %w: step label %q", recipe.ErrBadStepID, ErrInvalid, s.ID)
				}
			}
			k, err := mintKey(recipe.StepKeyPrefix, taken)
			if err != nil {
				return recipe.Recipe{}, err
			}
			if s.ID != "" {
				labels[s.ID] = recipe.StepID(k)
			}
			s.ID = recipe.StepID(k)
		}
	}
	relabel := func(ids []recipe.StepID) []recipe.StepID {
		if len(ids) == 0 {
			return ids
		}
		out := make([]recipe.StepID, len(ids))
		for i, id := range ids {
			if k, ok := labels[id]; ok {
				id = k
			}
			out[i] = id
		}
		return out
	}
	for ci := range comps {
		for si := range comps[ci].Steps {
			s := &comps[ci].Steps[si]
			s.DependsOn = relabel(s.DependsOn)
		}
	}
	out := r
	out.Components = comps
	out.Equipment = slices.Clone(r.Equipment)
	for i := range out.Equipment {
		out.Equipment[i].Steps = relabel(out.Equipment[i].Steps)
	}
	return out, nil
}

// recordKeys adds every key r holds to the keys recipe id has held.
func recordKeys(ctx context.Context, q *storedb.Queries, hid, id int64, r recipe.Recipe) error {
	add := func(k string) error {
		if err := q.InsertRecipeKey(ctx, storedb.InsertRecipeKeyParams{HouseholdID: hid, RecipeID: id, Key: k}); err != nil {
			return wrapWrite(fmt.Sprintf("record key %s of recipe %d", k, id), err)
		}
		return nil
	}
	for _, c := range r.Components {
		for _, l := range c.Lines {
			if err := add(l.Key); err != nil {
				return err
			}
		}
		for _, s := range c.Steps {
			if err := add(string(s.ID)); err != nil {
				return err
			}
		}
	}
	return nil
}
