package profile

import "testing"

// ADR-00032: today the rule shows every field to every relation that can reach
// a profile; an unknown relation or field sees nothing.
func TestVisible(t *testing.T) {
	relations := []Relation{Self, SameHousehold, Friend, Other}
	fields := []Field{FieldEmail, FieldDisplayName, FieldAvatar}
	for _, rel := range relations {
		for _, f := range fields {
			if !Visible(rel, f) {
				t.Errorf("Visible(%q, %q) = false, want true", rel, f)
			}
		}
	}
	if Visible("stranger", FieldEmail) {
		t.Error("an unknown relation sees a field")
	}
	if Visible(Self, "phone") {
		t.Error("an unknown field is visible")
	}
}

func TestRelationBetween(t *testing.T) {
	cases := []struct {
		name            string
		viewer, subject string
		sameHousehold   bool
		want            Relation
	}{
		{"same email", "ann@example.com", "ann@example.com", false, Self},
		{"same email in another case", " Ann@Example.com", "ann@example.com", true, Self},
		{"same household", "ann@example.com", "bob@example.com", true, SameHousehold},
		{"different households", "ann@example.com", "bob@example.com", false, Other},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelationBetween(tc.viewer, tc.subject, tc.sameHousehold); got != tc.want {
				t.Errorf("RelationBetween = %q, want %q", got, tc.want)
			}
		})
	}
}

// As is how a page gets fields: a field the rule hides comes back empty.
func TestAsShowsOnlyWhatVisibleAllows(t *testing.T) {
	p := Profile{Email: "ann@example.com", DisplayName: "Ann B", Avatar: "media/1"}
	got := p.As(Other)
	want := Shown{Email: "ann@example.com", Name: "Ann B", Avatar: "media/1", Seed: "ann@example.com"}
	if got != want {
		t.Errorf("As(Other) = %+v, want %+v", got, want)
	}
	if got := (Profile{Email: "ann@example.com"}).As(Self).Name; got != "ann" {
		t.Errorf("a profile with no display name shows %q, want the local part", got)
	}
	if got := p.As("stranger"); got != (Shown{}) {
		t.Errorf("As(unknown relation) = %+v, want nothing", got)
	}
}
