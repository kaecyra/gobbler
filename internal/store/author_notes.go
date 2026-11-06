package store

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// The recipe author's notes (ADR-00041) have statements of their own
// (queries/author_notes.sql). The model has already checked the one-paragraph
// rule; the column refuses an empty text that got past it.

func insertAuthorNotes(ctx context.Context, q *storedb.Queries, hid, id int64, notes []recipe.AuthorNote) error {
	for i, n := range notes {
		err := q.InsertAuthorNote(ctx, storedb.InsertAuthorNoteParams{
			HouseholdID: hid, RecipeID: id, Position: int64(i), Title: n.Title, Text: n.Text,
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write author note %d", i+1), err)
		}
	}
	return nil
}

func loadAuthorNotes(ctx context.Context, q *storedb.Queries, hid, id int64) ([]recipe.AuthorNote, error) {
	rows, err := q.ListAuthorNotes(ctx, storedb.ListAuthorNotesParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("author notes", err)
	}
	var out []recipe.AuthorNote
	for _, r := range rows {
		out = append(out, recipe.AuthorNote{Title: r.Title, Text: r.Text})
	}
	return out, nil
}

// docAuthorNote is an author note in the revision document.
type docAuthorNote struct {
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

func encodeAuthorNotes(notes []recipe.AuthorNote) []docAuthorNote {
	var out []docAuthorNote
	for _, n := range notes {
		out = append(out, docAuthorNote{Title: n.Title, Text: n.Text})
	}
	return out
}

func decodeAuthorNotes(notes []docAuthorNote) []recipe.AuthorNote {
	var out []recipe.AuthorNote
	for _, n := range notes {
		out = append(out, recipe.AuthorNote{Title: n.Title, Text: n.Text})
	}
	return out
}
