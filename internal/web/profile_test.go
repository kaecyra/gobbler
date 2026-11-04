package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/layout/layouttest"
)

// The profile pages through the real router (ADR-00032).

var profileAddress = regexp.MustCompile(`^/profile/([A-Za-z0-9_-]{22})$`)

// as makes a request from the dev identity email; form, when set, is the body.
func as(h http.Handler, method, target, email string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// profileOf opens email's own profile through /profile and returns its path.
func profileOf(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	rec := as(h, http.MethodGet, "/profile", email, nil)
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !profileAddress.MatchString(loc) {
		t.Fatalf("GET /profile as %s = %d to %q, want a redirect to /profile/<address>", email, rec.Code, loc)
	}
	return loc
}

func TestProfileAddressIsRandomStableAndHoldsNoEmail(t *testing.T) {
	h := tenantRouter(t)
	mine := profileOf(t, h, auth.DevEmail)
	if again := profileOf(t, h, auth.DevEmail); again != mine {
		t.Errorf("the address changed between visits: %q then %q", mine, again)
	}
	theirs := profileOf(t, h, auth.DevOtherEmail)
	if theirs == mine {
		t.Error("two people share an address")
	}
	for _, part := range []string{"dev", "localhost", "other", "@"} {
		if strings.Contains(mine, part) {
			t.Errorf("address %q contains %q", mine, part)
		}
	}
}

func TestOwnProfileShowsThePersonAndTheNameForm(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, auth.DevEmail)
	rec := as(h, http.MethodGet, path, auth.DevEmail, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, rec.Code)
	}
	for _, want := range []string{
		"Your profile", `data-testid="profile-email">` + auth.DevEmail, `>dev</h1>`,
		`action="` + path + `/name"`, `class="avatar av-bg-`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("own profile missing %q:\n%s", want, body)
		}
	}
}

// ADR-00032 obligation 5: the default avatar is drawn in the app, and the page
// loads nothing from another origin.
func TestProfilePageLoadsNothingFromAnotherOrigin(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, auth.DevEmail)
	body := as(h, http.MethodGet, path, auth.DevEmail, nil).Body.String()
	layouttest.AssertNoRemoteAssets(t, "profile page", body)
	if !strings.Contains(body, "<svg class=\"avatar ") {
		t.Errorf("the avatar is not drawn inline:\n%s", body)
	}
}

// ADR-00032 obligation 2 and the issue: another person sees the profile and
// none of the things only its owner sees, and cannot change it.
func TestAnotherPersonSeesTheProfileButCannotEditIt(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, auth.DevEmail)
	rec := as(h, http.MethodGet, path, auth.DevOtherEmail, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, auth.DevEmail) {
		t.Fatalf("another household's member opening %s = %d, want the profile", path, rec.Code)
	}
	for _, hidden := range []string{"Your profile", `action="` + path + `/name"`, `name="name"`, "profile-name-form", "profile-avatar-form", "Change your picture"} {
		if strings.Contains(body, hidden) {
			t.Errorf("another person's view shows %q", hidden)
		}
	}

	for _, who := range []string{auth.DevOtherEmail, strangerEmail} {
		rec := as(h, http.MethodPost, path+"/name", who, url.Values{"name": {"Hijacked"}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s renaming another person = %d, want 404", who, rec.Code)
		}
	}
	if body := as(h, http.MethodGet, path, auth.DevEmail, nil).Body.String(); strings.Contains(body, "Hijacked") {
		t.Error("another person's rename took effect")
	}
}

// ADR-00032 obligation 3: an unknown or guessed address is 404, for a read and
// for a change, whoever asks.
func TestUnknownProfileAddressIs404(t *testing.T) {
	h := tenantRouter(t)
	mine := profileOf(t, h, auth.DevEmail)
	for _, guess := range []string{
		"/profile/" + strings.Repeat("A", 22),
		"/profile/" + auth.DevEmail,
		"/profile/dev",
		"/profile/" + mine[len("/profile/"):len("/profile/")+21],
		"/profile/" + mine[len("/profile/"):] + "A",
	} {
		for _, who := range []string{auth.DevEmail, strangerEmail} {
			if rec := as(h, http.MethodGet, guess, who, nil); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s as %s = %d, want 404", guess, who, rec.Code)
			}
			if rec := as(h, http.MethodPost, guess+"/name", who, url.Values{"name": {"x"}}); rec.Code != http.StatusNotFound {
				t.Errorf("POST %s/name as %s = %d, want 404", guess, who, rec.Code)
			}
		}
	}
}

// ADR-00032: every signed-in person has a profile, so admission opens it to a
// non-member, and only it.
func TestNonMemberOpensOwnProfileAndNothingElse(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, strangerEmail)
	rec := as(h, http.MethodGet, path, strangerEmail, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), strangerEmail) {
		t.Fatalf("a non-member's own profile = %d", rec.Code)
	}
	if rec := as(h, http.MethodGet, "/recipes", strangerEmail, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a non-member's /recipes = %d, want 403", rec.Code)
	}
}

func TestRenameSetsAndClearsTheDisplayName(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, auth.DevEmail)

	rec := as(h, http.MethodPost, path+"/name", auth.DevEmail, url.Values{"name": {"Chef <Dev>"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path {
		t.Fatalf("rename = %d to %q, want a redirect back to the profile", rec.Code, rec.Header().Get("Location"))
	}
	body := as(h, http.MethodGet, path, auth.DevEmail, nil).Body.String()
	if !strings.Contains(body, `>Chef &lt;Dev&gt;</h1>`) {
		t.Errorf("the new name is not shown, escaped:\n%s", body)
	}
	// Another person sees the name too (the rule shows every field today).
	if body := as(h, http.MethodGet, path, auth.DevOtherEmail, nil).Body.String(); !strings.Contains(body, `>Chef &lt;Dev&gt;</h1>`) {
		t.Error("another person does not see the display name")
	}

	as(h, http.MethodPost, path+"/name", auth.DevEmail, url.Values{"name": {"  "}})
	if body := as(h, http.MethodGet, path, auth.DevEmail, nil).Body.String(); !strings.Contains(body, `>dev</h1>`) {
		t.Errorf("a blank name did not bring back the default:\n%s", body)
	}
}

func TestRenameRefusesATooLongNameWithItsReason(t *testing.T) {
	h := tenantRouter(t)
	path := profileOf(t, h, auth.DevEmail)
	rec := as(h, http.MethodPost, path+"/name", auth.DevEmail, url.Values{"name": {strings.Repeat("a", 61)}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at most 60 characters") {
		t.Errorf("a 61-character name = %d, body %s", rec.Code, rec.Body)
	}
}

func TestProfileNeedsASession(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	for _, path := range []string{"/profile", "/profile/" + strings.Repeat("A", 22)} {
		if rec := s.do(http.MethodGet, path, nil); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), layout.SignInPath) {
			t.Errorf("GET %s without a session = %d to %q, want sign-in", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// ADR-00032 obligation 6, as the server renders it: the header shows the
// avatar menu to a signed-in person, whether or not they belong to a
// household, and the signed-out figure to a visitor on a public page.
func TestHeaderShowsAvatarMenuSignedInAndFigureSignedOut(t *testing.T) {
	s := newSignInSite(t, false, time.Hour)
	seedMember(t, s.db, "Alpha", "ann@example.com")
	_, member := s.signIn(t, "ann@example.com", "/")
	_, stranger := s.signIn(t, "stranger@example.com", "/")

	for name, c := range map[string]*http.Cookie{"member": member, "non-member": stranger} {
		loc := s.do(http.MethodGet, "/profile", c).Header().Get("Location")
		body := s.do(http.MethodGet, loc, c).Body.String()
		for _, want := range []string{
			`data-avatar="account"`, `data-theme-toggle`, `href="/profile" aria-label="Profile"`, `action="/logout"`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: signed-in header missing %q", name, want)
			}
		}
		for _, absent := range []string{`data-avatar="signed-out"`, `href="/login"`, `class="modebtn"`} {
			if strings.Contains(body, absent) {
				t.Errorf("%s: signed-in header has %q", name, absent)
			}
		}
	}

	body := s.do(http.MethodGet, "/canada", nil).Body.String()
	for _, want := range []string{`data-avatar="signed-out"`, `data-theme-toggle`, `href="/login"`} {
		if !strings.Contains(body, want) {
			t.Errorf("signed-out header missing %q", want)
		}
	}
	for _, absent := range []string{`data-avatar="account"`, `href="/profile"`, `action="/logout"`} {
		if strings.Contains(body, absent) {
			t.Errorf("signed-out header has %q", absent)
		}
	}
}

// The account menu's Profile link is never marked current: on /profile/{address}
// the page may be someone else's, and the layout cannot tell cheaply.
func TestAccountMenuProfileLinkIsNotCurrentOnAnyProfile(t *testing.T) {
	h := tenantRouter(t)
	mine := profileOf(t, h, auth.DevEmail)
	for who, path := range map[string]string{"own": mine, "someone else's": mine} {
		viewer := auth.DevEmail
		if who != "own" {
			viewer = auth.DevOtherEmail
		}
		body := as(h, http.MethodGet, path, viewer, nil).Body.String()
		if !strings.Contains(body, `href="/profile" aria-label="Profile"`) {
			t.Fatalf("%s profile: no Profile link:\n%s", who, body)
		}
		if regexp.MustCompile(`href="/profile"[^>]*aria-current`).MatchString(body) {
			t.Errorf("%s profile: the Profile link is marked current", who)
		}
	}
}
