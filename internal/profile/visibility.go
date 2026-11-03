package profile

import "strings"

// Relation is how a viewer stands to the person whose profile they look at
// (ADR-00032, Visibility).
type Relation string

// The relations the visibility rule is keyed on.
const (
	// Self is the person looking at their own profile.
	Self Relation = "self"
	// SameHousehold is another member of the person's household.
	SameHousehold Relation = "same-household"
	// Friend is reserved for friends, which do not exist yet. Nothing returns
	// it today; it is here so that the rule's callers already handle it.
	Friend Relation = "friend"
	// Other is any other signed-in person who was given the address.
	Other Relation = "other"
)

// Field is one thing a profile shows.
type Field string

// The profile's fields today.
const (
	FieldEmail       Field = "email"
	FieldDisplayName Field = "display-name"
	FieldAvatar      Field = "avatar"
)

// Visible is the one rule: whether a viewer in relation rel may see field f of
// another person's profile. Every page or fragment that shows a person's
// fields asks it, so a limit added later changes this function and no page.
//
// Today every known relation sees every known field. An unknown relation or
// field sees nothing, so a new one added without a decision here is hidden
// rather than leaked.
func Visible(rel Relation, f Field) bool {
	switch rel {
	case Self, SameHousehold, Friend, Other:
	default:
		return false
	}
	switch f {
	case FieldEmail, FieldDisplayName, FieldAvatar:
		return true
	default:
		return false
	}
}

// RelationBetween names how viewer stands to subject, both emails. sameHousehold
// is whether the two belong to one household, which the caller reads from the
// household store; this package does not import it. Friend is never returned
// until friends exist.
func RelationBetween(viewer, subject string, sameHousehold bool) Relation {
	switch {
	case strings.EqualFold(strings.TrimSpace(viewer), strings.TrimSpace(subject)):
		return Self
	case sameHousehold:
		return SameHousehold
	default:
		return Other
	}
}

// Shown is a profile as one viewer may see it: each field is filled only when
// Visible allowed it, and is empty otherwise.
type Shown struct {
	Email string
	// Name is the display name, or the email's local part when none is set.
	Name string
	// Avatar is the reference to an uploaded picture, empty until one is.
	Avatar string
	// Seed is what the default avatar is drawn from; empty when the avatar is
	// not visible.
	Seed string
}

// As returns p as a viewer in relation rel may see it. A page shows only what
// this returns, never the Profile itself.
func (p Profile) As(rel Relation) Shown {
	var s Shown
	if Visible(rel, FieldEmail) {
		s.Email = p.Email
	}
	if Visible(rel, FieldDisplayName) {
		s.Name = p.Name()
	}
	if Visible(rel, FieldAvatar) {
		s.Avatar = p.Avatar
		s.Seed = p.Email
	}
	return s
}
