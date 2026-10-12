package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// Summary is a recipe as the list and search pages show it: enough to pick a
// recipe, none of the document. Total is the stored total time, or prep plus
// cook when the source stated none; the full derivation (ADR-00004) needs the
// step graph and belongs to the recipe page.
type Summary struct {
	ID          int64
	Name        string
	Description string
	Rating      *int
	Tags        []string
	Prep        time.Duration
	Cook        time.Duration
	Total       time.Duration
}

// SearchQuery selects recipes. Text is what the user typed: every word must
// appear (as a word prefix) in the name, description, ingredients, tags or
// notes. Tags are exact tag names; a recipe must carry all of them. Both empty
// selects every recipe.
type SearchQuery struct {
	Text string
	Tags []string
}

// TagCount is a tag in use and the number of recipes carrying it.
type TagCount struct {
	Tag     string
	Recipes int
}

// Search returns the recipes matching q: best text match first, or by name when
// q has no text. No match is an empty slice, not an error. Characters that mean
// something to FTS5 are ordinary punctuation here, so no input is a syntax error.
func (s *Store) Search(ctx context.Context, q SearchQuery) ([]Summary, error) {
	text := strings.TrimSpace(q.Text)
	expr, hasWords := matchExpression(text)
	if text != "" && !hasWords {
		return []Summary{}, nil
	}
	tags := cleanTags(q.Tags)

	var out []Summary
	err := s.readTx(ctx, func(tx *sql.Tx, qs *storedb.Queries) error {
		rows, err := s.summaryRows(ctx, tx, qs, expr)
		if err != nil {
			return err
		}
		if len(tags) > 0 {
			ids, err := qs.ListRecipeIDsWithAllTags(ctx, storedb.ListRecipeIDsWithAllTagsParams{Tags: tags, Column2: int64(len(tags))})
			if err != nil {
				return readErr("recipes by tag", err)
			}
			keep := make(map[int64]bool, len(ids))
			for _, id := range ids {
				keep[id] = true
			}
			rows = filterRows(rows, keep)
		}
		tagRows, err := qs.ListAllRecipeTags(ctx)
		if err != nil {
			return readErr("tags", err)
		}
		byRecipe := make(map[int64][]string)
		for _, r := range tagRows {
			byRecipe[r.RecipeID] = append(byRecipe[r.RecipeID], r.Tag)
		}
		out = make([]Summary, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.summary(byRecipe[r.id]))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Tags returns every tag in use with its recipe count, alphabetically.
func (s *Store) Tags(ctx context.Context) ([]TagCount, error) {
	out := []TagCount{}
	err := s.read(ctx, func(q *storedb.Queries) error {
		rows, err := q.ListTagCounts(ctx)
		if err != nil {
			return readErr("tag counts", err)
		}
		for _, r := range rows {
			out = append(out, TagCount{Tag: r.Tag, Recipes: int(r.Recipes)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// summaryRow is one recipes row before its tags are attached.
type summaryRow struct {
	id                      int64
	name, description       string
	rating                  sql.NullInt64
	prepNS, cookNS, totalNS int64
}

func (r summaryRow) summary(tags []string) Summary {
	total := time.Duration(r.totalNS)
	if total == 0 {
		total = time.Duration(r.prepNS + r.cookNS)
	}
	return Summary{
		ID: r.id, Name: r.name, Description: r.description, Rating: intPtr(r.rating), Tags: tags,
		Prep: time.Duration(r.prepNS), Cook: time.Duration(r.cookNS), Total: total,
	}
}

func filterRows(rows []summaryRow, keep map[int64]bool) []summaryRow {
	out := rows[:0]
	for _, r := range rows {
		if keep[r.id] {
			out = append(out, r)
		}
	}
	return out
}

// searchSQL is hand-written because sqlc cannot resolve the `recipe_search
// MATCH ?` form FTS5 requires (queries/search.sql names the rest). The bm25
// weights follow the index columns (name, description, ingredients, tags,
// notes): a hit in the name counts most, then tags, ingredients, description
// and notes.
const searchSQL = `
SELECT r.id, r.name, r.description, r.rating, r.prep_ns, r.cook_ns, r.total_ns
FROM recipe_search s JOIN recipes r ON r.id = s.rowid
WHERE recipe_search MATCH ?
ORDER BY bm25(recipe_search, 10.0, 2.0, 3.0, 5.0, 1.0), lower(r.name), r.id`

// summaryRows returns every recipe (expr empty, by name) or the recipes
// matching expr (best first).
func (s *Store) summaryRows(ctx context.Context, tx *sql.Tx, qs *storedb.Queries, expr string) ([]summaryRow, error) {
	if expr == "" {
		rows, err := qs.ListRecipeSummaries(ctx)
		if err != nil {
			return nil, readErr("recipes", err)
		}
		out := make([]summaryRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, summaryRow{r.ID, r.Name, r.Description, r.Rating, r.PrepNs, r.CookNs, r.TotalNs})
		}
		return out, nil
	}
	rs, err := tx.QueryContext(ctx, searchSQL, expr)
	if err != nil {
		return nil, readErr("search", err)
	}
	var out []summaryRow
	for rs.Next() {
		var r summaryRow
		if err := rs.Scan(&r.id, &r.name, &r.description, &r.rating, &r.prepNS, &r.cookNS, &r.totalNS); err != nil {
			return nil, errors.Join(readErr("search", err), rs.Close())
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, errors.Join(readErr("search", err), rs.Close())
	}
	if err := rs.Close(); err != nil {
		return nil, readErr("search", err)
	}
	return out, nil
}

// readTx is read with the transaction itself, for the one hand-written query.
func (s *Store) readTx(ctx context.Context, fn func(tx *sql.Tx, q *storedb.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	err = fn(tx, s.q.WithTx(tx))
	if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
		err = errors.Join(err, fmt.Errorf("end read transaction: %w", rerr))
	}
	return err
}

// matchExpression turns what a user typed into an FTS5 query: each word is a
// quoted string (so operators, NEAR, column filters and stray quotes are plain
// text) and the last gets a prefix star so results appear while typing. Words
// are joined by AND. ok is false when the input holds no word at all.
func matchExpression(text string) (expr string, ok bool) {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	if len(words) == 0 {
		return "", false
	}
	for i, w := range words {
		words[i] = `"` + w + `"`
	}
	return strings.Join(words, " ") + "*", true
}

// cleanTags drops blanks and repeats, so the tag count the query compares
// against is the number of distinct tags asked for.
func cleanTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	var out []string
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
