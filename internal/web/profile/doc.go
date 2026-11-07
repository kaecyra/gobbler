// Package profile is the profile page (ADR-00032): who a person is to the
// people they share with, and a place for what only they manage. It is mounted
// at layout.ProfilePath, which the admission rule opens to a signed-in
// non-member as well, so the page works for anyone who may sign in.
//
// A page's address is the opaque random one the store gave the profile, never
// the email. Every field of another person's profile that a page shows comes
// from Profile.As, which asks the one visibility rule. Every read and write
// goes through the profile service, internal/api/profile (ADR-00043), which
// checks the Caller; the handlers do not import internal/profile.
//
// # Adding a section
//
// The page is a composition (page.templ, routes.go). A later node, one of
// sessions, avatar, keys and grants, adds a section by keeping all of its own
// code in files named for it and adding exactly one line to each of two files:
//
//   - page.templ: in selfSections, a line "@<name>Section(v)" that renders the
//     section from the pageView. selfSections renders only for the person
//     whose profile it is, so a section there can never reach another viewer
//     (ADR-00032 obligation 2).
//   - routes.go: in selfRoutes, a line "h.<name>Routes(r)" that registers the
//     section's routes on r. They are mounted under /{address}/ behind a check
//     that the viewer is the profile's owner, and read the profile from
//     subjectFrom. The section calls h.Profile, the service.
//
// A section that other viewers see too is not self-only: it asks
// Visible for each field it shows, and is added to the page outside
// selfSections.
package profile
