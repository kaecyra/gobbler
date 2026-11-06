package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidAuthorNote is returned for an author note with no text or more than
// one paragraph (ADR-00041). Length never makes a note invalid (ADR-00044).
var ErrInvalidAuthorNote = errors.New("invalid author note")

// AuthorNoteGuidelineChars is the length a note is meant to stay within,
// counted in characters. It is a guideline, not a limit: Validate accepts a
// longer note, and ingest flags one over it at low confidence so the review
// screen shows it (ADR-00044).
const AuthorNoteGuidelineChars = 400

// AuthorNote is one of the recipe author's own notes: a tip, a substitution, a
// storage hint. It belongs to the recipe as a whole, not to a step, and is kept
// apart from the household's journal notes, which are timestamped and theirs
// (ADR-00041). Title is empty when the note has none; Text is one paragraph.
type AuthorNote struct {
	Title string
	Text  string
}

// Lead is the title as the bold opening of the note's text: the title and a
// colon, unless the title already ends in punctuation; empty for a note with no
// title. The recipe page and cook mode show it the same way.
func (n AuthorNote) Lead() string {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		return ""
	}
	if strings.ContainsRune(".:!?", rune(title[len(title)-1])) {
		return title
	}
	return title + ":"
}

// Validate checks that the note has text, that the text is one paragraph (it
// holds no blank line). Length is not checked (ADR-00044). The rule holds for every writer: ingest, the editor and MCP.
func (n AuthorNote) Validate() error {
	text := strings.TrimSpace(n.Text)
	switch {
	case text == "":
		return fmt.Errorf("%w: the note has no text", ErrInvalidAuthorNote)
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
