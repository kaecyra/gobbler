package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
)

// post sends a form as email, the way the pages' forms do.
func post(h http.Handler, path, email string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(auth.DevIdentityHeader, email)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var invitationPath = regexp.MustCompile(`/invitations/([0-9A-Za-z]{8})\b`)

// lastInvitationCode reads the newest invitation code off a page that lists
// them.
func lastInvitationCode(t *testing.T, body string) string {
	t.Helper()
	all := invitationPath.FindAllStringSubmatch(body, -1)
	if len(all) == 0 {
		t.Fatalf("no invitation link in:\n%s", body)
	}
	return all[len(all)-1][1]
}

func wantStatus(t *testing.T, what string, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s = %d, want %d:\n%s", what, rec.Code, want, rec.Body.String())
	}
}

// The operator starts a household for a stranger, who accepts it, invites a
// second person into it, and each step is refused to everyone it is not for
// (ADR-00018, Invitations).
func TestInvitationFlowFromOperatorToSecondMember(t *testing.T) {
	h := tenantRouter(t)
	const newcomer, guest = "new@example.com", "guest@example.com"

	wantStatus(t, "operator invites", post(h, "/operator/invitations", operatorEmail, url.Values{"email": {newcomer}}), http.StatusSeeOther)
	page := do(h, http.MethodGet, "/operator/invitations", operatorEmail)
	wantStatus(t, "operator page", page, http.StatusOK)
	code := lastInvitationCode(t, page.Body.String())
	link := "/invitations/" + code
	if !strings.Contains(page.Body.String(), newcomer) {
		t.Errorf("operator page does not list the pending invitation for %s", newcomer)
	}

	// Only the addressed email can open or accept it.
	for _, who := range []string{strangerEmail, auth.DevEmail, operatorEmail} {
		wantStatus(t, who+" opens it", do(h, http.MethodGet, link, who), http.StatusNotFound)
		wantStatus(t, who+" accepts it", post(h, link+"/accept", who, url.Values{"name": {"Hijack"}}), http.StatusNotFound)
	}
	open := do(h, http.MethodGet, link, newcomer)
	wantStatus(t, "addressee opens it", open, http.StatusOK)
	if !strings.Contains(open.Body.String(), "start a household") {
		t.Errorf("household invitation page does not offer to start one:\n%s", open.Body.String())
	}
	wantStatus(t, "blank household name", post(h, link+"/accept", newcomer, url.Values{"name": {" "}}), http.StatusBadRequest)
	wantStatus(t, "addressee accepts", post(h, link+"/accept", newcomer, url.Values{"name": {"Newcomers"}}), http.StatusSeeOther)
	wantStatus(t, "single use", post(h, link+"/accept", newcomer, url.Values{"name": {"Again"}}), http.StatusGone)

	members := do(h, http.MethodGet, "/household/", newcomer)
	wantStatus(t, "members page", members, http.StatusOK)
	if !strings.Contains(members.Body.String(), "Newcomers") {
		t.Errorf("members page does not name the household:\n%s", members.Body.String())
	}

	// A member invites a guest; a member of another household cannot see or
	// revoke the invitation.
	wantStatus(t, "member invites", post(h, "/household/invitations", newcomer, url.Values{"email": {guest}}), http.StatusSeeOther)
	joinCode := lastInvitationCode(t, do(h, http.MethodGet, "/household/", newcomer).Body.String())
	if body := do(h, http.MethodGet, "/household/", auth.DevEmail).Body.String(); strings.Contains(body, guest) {
		t.Errorf("another household's members page lists the guest invitation:\n%s", body)
	}
	revoke := "/household/invitations/" + joinCode + "/revoke"
	wantStatus(t, "other household revokes", post(h, revoke, auth.DevEmail, nil), http.StatusNotFound)

	joinLink := "/invitations/" + joinCode
	wantStatus(t, "a member of another household opens a join invitation", do(h, http.MethodGet, joinLink, auth.DevEmail), http.StatusNotFound)
	wantStatus(t, "guest accepts", post(h, joinLink+"/accept", guest, nil), http.StatusSeeOther)
	if body := do(h, http.MethodGet, "/household/", guest).Body.String(); !strings.Contains(body, newcomer) || !strings.Contains(body, "Newcomers") {
		t.Errorf("guest's members page:\n%s", body)
	}
}

func TestRevokedExpiredAndRefusedInvitations(t *testing.T) {
	h := tenantRouter(t)
	wantStatus(t, "invite", post(h, "/household/invitations", auth.DevEmail, url.Values{"email": {"rev@example.com"}}), http.StatusSeeOther)
	code := lastInvitationCode(t, do(h, http.MethodGet, "/household/", auth.DevEmail).Body.String())
	link := "/invitations/" + code
	revoke := "/household/invitations/" + code + "/revoke"

	wantStatus(t, "revoke", post(h, revoke, auth.DevEmail, nil), http.StatusSeeOther)
	wantStatus(t, "open revoked", do(h, http.MethodGet, link, "rev@example.com"), http.StatusGone)
	wantStatus(t, "accept revoked", post(h, link+"/accept", "rev@example.com", nil), http.StatusGone)
	for _, bad := range []string{"abc", "1", "abcdefg-", "abcdefghi"} {
		wantStatus(t, "malformed code "+bad, do(h, http.MethodGet, "/invitations/"+bad, "rev@example.com"), http.StatusNotFound)
	}
	bad := post(h, "/household/invitations", auth.DevEmail, url.Values{"email": {"nope"}})
	wantStatus(t, "bad email", bad, http.StatusBadRequest)
	if !strings.Contains(bad.Body.String(), `&#34;nope&#34; is not an email address`) {
		t.Errorf("a mistyped email gets no reason:\n%s", bad.Body.String())
	}
	self := post(h, "/household/members/remove", auth.DevEmail, url.Values{"email": {auth.DevEmail}})
	wantStatus(t, "remove oneself", self, http.StatusBadRequest)
	if !strings.Contains(self.Body.String(), "use leave to remove yourself") {
		t.Errorf("removing oneself gets no reason:\n%s", self.Body.String())
	}

	// A person who already has a household is told to leave it first.
	wantStatus(t, "invite a member", post(h, "/household/invitations", auth.DevEmail, url.Values{"email": {auth.DevOtherEmail}}), http.StatusSeeOther)
	code = lastInvitationCode(t, do(h, http.MethodGet, "/household/", auth.DevEmail).Body.String())
	link = "/invitations/" + code
	page := do(h, http.MethodGet, link, auth.DevOtherEmail)
	wantStatus(t, "member opens an invitation", page, http.StatusOK)
	if !strings.Contains(page.Body.String(), "Leave it first") || strings.Contains(page.Body.String(), "<button") && strings.Contains(page.Body.String(), "Join ") {
		t.Errorf("a member is offered to join:\n%s", page.Body.String())
	}
	refused := post(h, link+"/accept", auth.DevOtherEmail, nil)
	wantStatus(t, "member accepts", refused, http.StatusConflict)
	if !strings.Contains(refused.Body.String(), "leave the current household first") {
		t.Errorf("refusal does not say to leave first:\n%s", refused.Body.String())
	}
}

func TestLeaveRemoveAndDelete(t *testing.T) {
	h := tenantRouter(t)
	const owner, mate = "owner@example.com", "mate@example.com"
	join := func(email string, form url.Values, path, issuer string) {
		t.Helper()
		wantStatus(t, "invite "+email, post(h, path, issuer, form), http.StatusSeeOther)
	}
	join(owner, url.Values{"email": {owner}}, "/operator/invitations", operatorEmail)
	oid := lastInvitationCode(t, do(h, http.MethodGet, "/operator/invitations", operatorEmail).Body.String())
	wantStatus(t, "start", post(h, "/invitations/"+oid+"/accept", owner, url.Values{"name": {"Kitchen"}}), http.StatusSeeOther)
	join(mate, url.Values{"email": {mate}}, "/household/invitations", owner)
	mid := lastInvitationCode(t, do(h, http.MethodGet, "/household/", owner).Body.String())
	wantStatus(t, "join", post(h, "/invitations/"+mid+"/accept", mate, nil), http.StatusSeeOther)

	// Neither the household's name nor a member of another household lets a
	// stranger remove or delete.
	wantStatus(t, "other household removes", post(h, "/household/members/remove", auth.DevEmail, url.Values{"email": {mate}}), http.StatusNotFound)
	wantStatus(t, "delete with two members", post(h, "/household/delete", owner, url.Values{"confirm": {"Kitchen"}}), http.StatusConflict)
	wantStatus(t, "remove oneself", post(h, "/household/members/remove", owner, url.Values{"email": {owner}}), http.StatusBadRequest)

	wantStatus(t, "remove a member", post(h, "/household/members/remove", owner, url.Values{"email": {mate}}), http.StatusSeeOther)
	wantStatus(t, "removed member is a non-member", do(h, http.MethodGet, "/household/", mate), http.StatusForbidden)

	last := post(h, "/household/leave", owner, nil)
	wantStatus(t, "last member leaves", last, http.StatusConflict)
	if !strings.Contains(last.Body.String(), "delete the household") {
		t.Errorf("refusal does not say to delete instead:\n%s", last.Body.String())
	}
	wantStatus(t, "wrong confirmation", post(h, "/household/delete", owner, url.Values{"confirm": {"Nope"}}), http.StatusBadRequest)
	wantStatus(t, "still a member", do(h, http.MethodGet, "/household/", owner), http.StatusOK)
	wantStatus(t, "delete", post(h, "/household/delete", owner, url.Values{"confirm": {"Kitchen"}}), http.StatusSeeOther)
	wantStatus(t, "deleted household's member", do(h, http.MethodGet, "/household/", owner), http.StatusForbidden)
}

func TestHouseholdRoutesRefuseNonMembersAndOtherHouseholds(t *testing.T) {
	h := tenantRouter(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/household/"},
		{http.MethodPost, "/household/invitations"},
		{http.MethodPost, "/household/invitations/aZ09bY18/revoke"},
		{http.MethodPost, "/household/members/remove"},
		{http.MethodPost, "/household/leave"},
		{http.MethodPost, "/household/delete"},
	} {
		rec := do(h, tc.method, tc.path, strangerEmail)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), notAMemberText) {
			t.Errorf("non-member %s %s = %d, want the not-a-member page", tc.method, tc.path, rec.Code)
		}
	}
	// A household's page names only itself.
	for email, other := range map[string]string{auth.DevEmail: "Other household", auth.DevOtherEmail: "Dev household"} {
		rec := do(h, http.MethodGet, "/household/", email)
		wantStatus(t, email+" members page", rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), other) || strings.Contains(rec.Body.String(), strings.TrimSuffix(other, " household")+"@") {
			t.Errorf("%s sees %q:\n%s", email, other, rec.Body.String())
		}
	}
}

func TestOperatorInvitationPageIsOperatorOnly(t *testing.T) {
	h := tenantRouter(t)
	cases := []struct {
		name, email, method, path string
		want                      int
	}{
		{"non-operator member", auth.DevEmail, http.MethodGet, "/operator", http.StatusForbidden},
		{"non-operator member, issue", auth.DevEmail, http.MethodPost, "/operator/invitations", http.StatusForbidden},
		{"non-operator member, revoke", auth.DevEmail, http.MethodPost, "/operator/invitations/aZ09bY18/revoke", http.StatusForbidden},
		{"non-member non-operator", strangerEmail, http.MethodGet, "/operator/", http.StatusForbidden},
		{"non-member non-operator, issue", strangerEmail, http.MethodPost, "/operator/invitations", http.StatusForbidden},
		{"operator with no household", operatorEmail, http.MethodGet, "/operator", http.StatusOK},
		{"operator who is a member", auth.DevOtherEmail, http.MethodGet, "/operator/", http.StatusOK},
	}
	for _, c := range cases {
		if rec := do(h, c.method, c.path, c.email); rec.Code != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, rec.Code, c.want)
		}
	}
	body := do(h, http.MethodGet, "/operator/invitations", operatorEmail).Body.String()
	for _, leak := range []string{"Dev household", "Other household", auth.DevEmail, auth.DevOtherEmail} {
		if strings.Contains(body, leak) {
			t.Errorf("operator invitation page shows household data %q", leak)
		}
	}
}

// An operator sees and revokes only the invitations they issued.
func TestOperatorRevokesOnlyOwnInvitations(t *testing.T) {
	h := tenantRouter(t)
	wantStatus(t, "issue", post(h, "/operator/invitations", operatorEmail, url.Values{"email": {"a@example.com"}}), http.StatusSeeOther)
	code := lastInvitationCode(t, do(h, http.MethodGet, "/operator/invitations", operatorEmail).Body.String())
	revoke := "/operator/invitations/" + code + "/revoke"

	wantStatus(t, "another operator's page", do(h, http.MethodGet, "/operator/invitations", auth.DevOtherEmail), http.StatusOK)
	if strings.Contains(do(h, http.MethodGet, "/operator/invitations", auth.DevOtherEmail).Body.String(), "a@example.com") {
		t.Error("an operator sees another operator's invitation")
	}
	wantStatus(t, "another operator revokes", post(h, revoke, auth.DevOtherEmail, nil), http.StatusNotFound)
	wantStatus(t, "revoke", post(h, revoke, operatorEmail, nil), http.StatusSeeOther)
	wantStatus(t, "revoke twice", post(h, revoke, operatorEmail, nil), http.StatusNotFound)
	wantStatus(t, "malformed code", post(h, "/operator/invitations/1/revoke", operatorEmail, nil), http.StatusNotFound)
	if strings.Contains(do(h, http.MethodGet, "/operator/invitations", operatorEmail).Body.String(), "a@example.com") {
		t.Error("a revoked invitation is still listed")
	}
}

func TestOperatorInvitationShowsWhyInputWasRefused(t *testing.T) {
	h := tenantRouter(t)
	rec := post(h, "/operator/invitations", operatorEmail, url.Values{"email": {"nope"}})
	wantStatus(t, "bad email", rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), `&#34;nope&#34; is not an email address`) {
		t.Errorf("a mistyped invitee gets no reason:\n%s", rec.Body.String())
	}
}

var integerInvitationLink = regexp.MustCompile(`/invitations/\d{1,7}\b`)

// An invitation is named by its code and nothing else (ADR-00028): the pages
// that list invitations link codes, never the integer key, and a code nobody
// was issued gets exactly the answer a code addressed to someone else gets, so
// the accept route cannot be used to learn which codes exist.
func TestInvitationsAreNamedOnlyByCode(t *testing.T) {
	h := tenantRouter(t)
	wantStatus(t, "invite", post(h, "/household/invitations", auth.DevEmail, url.Values{"email": {"cy@example.com"}}), http.StatusSeeOther)
	wantStatus(t, "operator invites", post(h, "/operator/invitations", operatorEmail, url.Values{"email": {"dee@example.com"}}), http.StatusSeeOther)
	members := do(h, http.MethodGet, "/household/", auth.DevEmail).Body.String()
	operator := do(h, http.MethodGet, "/operator/invitations", operatorEmail).Body.String()
	for page, body := range map[string]string{"members": members, "operator": operator} {
		if integerInvitationLink.MatchString(body) {
			t.Errorf("%s page names an invitation by its integer key:\n%s", page, body)
		}
	}
	code := lastInvitationCode(t, members)
	link := "/invitations/" + code

	unknown := "Zz000000"
	if unknown == code {
		unknown = "Zz000001"
	}
	for _, path := range []string{"", "/accept"} {
		method := http.MethodGet
		if path != "" {
			method = http.MethodPost
		}
		other := do(h, method, link+path, "someone@example.com")
		missing := do(h, method, "/invitations/"+unknown+path, "someone@example.com")
		if other.Code != http.StatusNotFound || missing.Code != other.Code || missing.Body.String() != other.Body.String() {
			t.Errorf("%s %s: unknown code got %d, another's code got %d; want the same 404 page",
				method, path, missing.Code, other.Code)
		}
	}
}
