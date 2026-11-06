package recipe

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrInvalidAuthorNote is returned for an author note with no text, more than
// MaxAuthorNoteChars characters, or more than one paragraph (ADR-00041).
var ErrInvalidAuthorNote = errors.New("invalid author note")

// MaxAuthorNoteChars is the longest text an author note may hold, counted in
// characters (ADR-00041).
const MaxAuthorNoteChars = 400

// AuthorNote is one of the recipe author's own notes: a tip, a substitution, a
// storage hint. It belongs to the recipe as a whole, not to a step, and is kept
// apart from the household's journal notes, which are timestamped and theirs
// (ADR-00041). Title is empty when the note has none; Text is one paragraph.
type AuthorNote struct {
	Title string
	Text  string
}

// Validate checks that the note has text, that the text is one paragraph (it
// holds no blank line) and that it is at most MaxAuthorNoteChars characters.
// The rule holds for every writer: ingest, the editor and MCP.
func (n AuthorNote) Validate() error {
	text := strings.TrimSpace(n.Text)
	switch {
	case text == "":
		return fmt.Errorf("%w: the note has no text", ErrInvalidAuthorNote)
	case utf8.RuneCountInString(text) > MaxAuthorNoteChars:
		return fmt.Errorf("%w: %d characters, the most is %d", ErrInvalidAuthorNote, utf8.RuneCountInString(text), MaxAuthorNoteChars)
	case MoreThanOneParagraph(text):
		return fmt.Errorf("%w: the text holds more than one paragraph", ErrInvalidAuthorNote)
	}
	return nil
}

// MoreThanOneParagraph reports whether text holds a blank line between two
// lines of words.
func MoreThanOneParagraph(text string) bool {
	blank := false
	seen := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			blank = blank || seen
			continue
		}
		if blank {
			return true
		}
		seen = true
	}
	return false
}

func (r Recipe) validateAuthorNotes() error {
	for i, n := range r.AuthorNotes {
		if err := n.Validate(); err != nil {
			return fmt.Errorf("author note %d: %w", i+1, err)
		}
	}
	return nil
}
