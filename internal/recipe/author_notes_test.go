package recipe

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAuthorNotes(t *testing.T) {
	step := Component{Steps: []Step{{ID: "a", Title: "Mix"}}}
	with := func(n ...AuthorNote) Recipe { return Recipe{Components: []Component{step}, AuthorNotes: n} }
	tests := []struct {
		name    string
		r       Recipe
		wantErr error
	}{
		{"no notes", with(), nil},
		{"title and text", with(AuthorNote{Title: "Honey", Text: "Do not give to infants."}), nil},
		{"text only", with(AuthorNote{Text: "Keeps three days."}), nil},
		{"400 characters", with(AuthorNote{Text: strings.Repeat("a", MaxAuthorNoteChars)}), nil},
		{"401 characters", with(AuthorNote{Text: strings.Repeat("a", MaxAuthorNoteChars+1)}), ErrInvalidAuthorNote},
		{"400 multi-byte characters", with(AuthorNote{Text: strings.Repeat("é", MaxAuthorNoteChars)}), nil},
		{"401 multi-byte characters", with(AuthorNote{Text: strings.Repeat("é", MaxAuthorNoteChars+1)}), ErrInvalidAuthorNote},
		{"two paragraphs", with(AuthorNote{Text: "One.\n\nTwo."}), ErrInvalidAuthorNote},
		{"two paragraphs, CRLF", with(AuthorNote{Text: "One.\r\n\r\nTwo."}), ErrInvalidAuthorNote},
		{"line break is not a paragraph", with(AuthorNote{Text: "One.\nTwo."}), nil},
		{"empty text", with(AuthorNote{Title: "Honey"}), ErrInvalidAuthorNote},
		{"blank text", with(AuthorNote{Text: " \n "}), ErrInvalidAuthorNote},
		{"second note bad", with(AuthorNote{Text: "ok"}, AuthorNote{Text: "a\n\nb"}), ErrInvalidAuthorNote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.r.Validate(); !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthorNoteValidateNamesTheNote(t *testing.T) {
	r := Recipe{Components: []Component{{Steps: []Step{{ID: "a"}}}}, AuthorNotes: []AuthorNote{{Text: "ok"}, {Text: "a\n\nb"}}}
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "author note 2") {
		t.Errorf("Validate() = %v, want it to name author note 2", err)
	}
}
