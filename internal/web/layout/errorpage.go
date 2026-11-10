package layout

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
)

// ErrorView is what an error page shows (docs/ui/design.md, Error pages): a
// mascot standing on a baseline, a small label, a heading, a line of text, an
// optional detail panel and the ways out. A page that knows more about its
// failure than the status table does builds its own and renders ErrorPageView;
// everything else gets one from ErrorViewFor.
type ErrorView struct {
	Status int
	// Mascot is one of the Mascot paths.
	Mascot string
	// Label is the small line above the heading; empty means "Error <status>".
	Label   string
	Heading string
	Body    string
	// Detail, when set, is shown in a tinted panel under Body.
	Detail  templ.Component
	Actions []ErrorAction
}

// ErrorAction is a way out of an error page. A SignOut action is a form that
// posts to SignOutPath; any other is a link to Href.
type ErrorAction struct {
	Label   string
	Href    string
	Primary bool
	SignOut bool
}

// LabelText is the small line above the heading.
func (v ErrorView) LabelText() string {
	if v.Label != "" {
		return v.Label
	}
	return "Error " + strconv.Itoa(v.Status)
}

var (
	toRecipes = ErrorAction{Label: "Go to your recipes", Href: "/recipes", Primary: true}
	toHome    = ErrorAction{Label: "Home", Href: "/"}
)

// serverTrouble is the text of every 5xx page that has no message of its own.
const serverTrouble = "It's been logged. Reload the page in a moment, and if it keeps happening, tell whoever runs this site what you were doing."

// ErrorViewFor is the page for status with message as its text. An empty
// message takes the status's own text; a 404 always does, because the reason
// a page is missing is never more use to the person than where to go next.
func ErrorViewFor(status int, message string) ErrorView {
	v := ErrorView{Status: status, Mascot: MascotWink, Body: message, Actions: []ErrorAction{primary(toHome)}}
	switch {
	case status == http.StatusNotFound:
		v.Mascot, v.Heading = MascotTasting, "We couldn't find that page"
		v.Body = "The address may be mistyped, or the page may have moved."
		v.Actions = []ErrorAction{toRecipes, toHome}
	case status == http.StatusForbidden:
		v.Heading = "This page isn't open to you"
	case status == http.StatusGone:
		v.Mascot, v.Heading = MascotHerbs, "That link no longer works"
	case status == http.StatusBadGateway:
		v.Heading = "We couldn't reach that site"
	case status >= 500:
		v.Heading = "Something went wrong on our side"
		if v.Body == "" {
			v.Body = serverTrouble
		}
	default:
		v.Heading = "We couldn't do that"
	}
	return v
}

func primary(a ErrorAction) ErrorAction {
	a.Primary = true
	return a
}

// goneInvitation names the reasons an invitation no longer opens. Each is a
// 410 with the same way forward.
var goneInvitation = []struct {
	err     error
	heading string
}{
	{apihouseholds.ErrExpired, "This invitation has expired"},
	{apihouseholds.ErrUsed, "This invitation has already been used"},
	{apihouseholds.ErrRevoked, "This invitation was withdrawn"},
}

// errorView is the page WriteError shows for err, which the status table
// mapped to status with text as the sentinel's own text.
func errorView(r *http.Request, err error, status int, text string) ErrorView {
	for _, g := range goneInvitation {
		if errors.Is(err, g.err) {
			return ErrorView{
				Status: status, Mascot: MascotHerbs, Label: "Invitation", Heading: g.heading,
				Body: "Ask the person who invited you to send a new link.", Actions: []ErrorAction{primary(toHome)},
			}
		}
	}
	if errors.Is(err, apirecipes.ErrNotFound) && strings.HasPrefix(r.URL.Path, "/recipes/") {
		return ErrorView{
			Status: status, Mascot: MascotMixingBowl, Heading: "That recipe isn't in your household",
			Body:    "It may have been deleted. A recipe from another household opens only once it's shared with you.",
			Actions: []ErrorAction{toRecipes},
		}
	}
	return ErrorViewFor(status, sentence(text))
}

// sentence turns a sentinel's text ("invitation has expired") into a sentence
// a page can show: a capital first letter and a closing full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(s)
	s = string(unicode.ToUpper(first)) + s[size:]
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "!") {
		s += "."
	}
	return s
}
