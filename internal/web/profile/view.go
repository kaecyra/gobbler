package profile

import (
	domain "github.com/kaecyra/gobbler/internal/profile"
)

// pageView is everything the profile page shows. Fields of the person come only
// through Shown, which the visibility rule has already filtered for this viewer
// (ADR-00032). A section reads what it needs from here and adds a field for
// anything of its own.
type pageView struct {
	// Address is the profile's opaque address, the root of its section routes.
	Address string
	// Self is whether the viewer is the profile's person. The self-only
	// sections render only then.
	Self  bool
	Shown domain.Shown
	// OwnName is the display name the person set, empty if none, and
	// DefaultName what the page shows then. Both are filled for the person
	// themself only, for the form that sets the name.
	OwnName     string
	DefaultName string
}

func newView(s subject) pageView {
	v := pageView{Address: s.Profile.Address, Self: s.Self(), Shown: s.Profile.As(s.Relation)}
	if v.Self {
		v.OwnName = s.Profile.DisplayName
		v.DefaultName = domain.Profile{Email: s.Profile.Email}.Name()
	}
	return v
}

// title is the page's title: the person's name where the viewer may see it.
func (v pageView) title() string {
	if v.Shown.Name == "" {
		return "Profile"
	}
	return v.Shown.Name
}

// sectionPath is the path of a section's route on this profile, for example
// sectionPath("name") for the display name form.
func (v pageView) sectionPath(section string) string {
	return Pattern + "/" + v.Address + "/" + section
}
