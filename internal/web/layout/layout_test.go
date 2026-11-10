package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
)

func TestPageRendersTitleNavAndContentSlot(t *testing.T) {
	body := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<p>slot content</p>"))
		return err
	})
	ctx := templ.WithChildren(context.Background(), body)
	var b bytes.Buffer
	if err := Page("Lasagna <b>").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		"<title>Lasagna &lt;b&gt; - gobbler</title>",
		`aria-label="Primary"`,
		`<main id="content">`,
		`src="` + ThemePath + `"`,
		`data-theme-toggle`,
		`Made by <a href="` + MakerURL + `" target="_blank" rel="noopener">kaecyra</a> in <a href="` + CanadaPath + `">Canada</a>`,
		"<p>slot content</p>",
		`href="` + StylesheetPath + `"`,
		`src="` + HTMXPath + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page missing %q in:\n%s", want, got)
		}
	}
}

// ADR-00022 obligation 4: each mascot is its own cached SVG file and is shown
// with an img tag, so no page carries a drawing inline.
func TestPageInlinesNoMascotDrawing(t *testing.T) {
	var b bytes.Buffer
	if err := Page("x").Render(templ.WithChildren(context.Background(), templ.NopComponent), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, `src="`+MascotHeader+`"`) {
		t.Errorf("header does not show the mascot from %s:\n%s", MascotHeader, got)
	}
	for _, inline := range []string{"<symbol", `id="gob`, "<clipPath", "<use", "M695 1486", "#acafaf"} {
		if strings.Contains(got, inline) {
			t.Errorf("page inlines mascot drawing markup %q", inline)
		}
	}
}

func TestWriteErrorMapsStatusAndHidesInternals(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
		hideBody   string
	}{
		{"not found", ErrNotFound, http.StatusNotFound, "couldn't find that page", ""},
		{"invalid", ErrInvalid, http.StatusBadRequest, "invalid request", ""},
		{"conflict", ErrConflict, http.StatusConflict, "conflict", ""},
		{"catalog not found", catalog.ErrNotFound, http.StatusNotFound, "couldn't find that page", ""},
		{"catalog invalid", catalog.ErrInvalid, http.StatusBadRequest, "invalid catalog entry", ""},
		{"catalog conflict", catalog.ErrConflict, http.StatusConflict, "already in use", ""},
		{"wrapped keeps sentinel text only", fmt.Errorf("recipe 7: %w", ErrNotFound), http.StatusNotFound, "couldn't find that page", "recipe 7"},
		{"driver text under a conflict", fmt.Errorf("insert ingredient: %w: constraint failed: UNIQUE constraint failed: ingredient.name (2067)", catalog.ErrConflict), http.StatusConflict, "catalog name or alias already in use", "UNIQUE constraint"},
		{"household not found", fmt.Errorf("invitation aZ09bY18: %w", household.ErrNotFound), http.StatusNotFound, "couldn't find that page", "aZ09bY18"},
		{"household invalid", household.ErrInvalid, http.StatusBadRequest, "invalid input", ""},
		{"wrong confirmation", household.ErrConfirmation, http.StatusBadRequest, "does not match", ""},
		{"already a member", household.ErrAlreadyMember, http.StatusConflict, "leave the current household first", ""},
		{"last member", household.ErrLastMember, http.StatusConflict, "delete the household", ""},
		{"not the last member", household.ErrNotLastMember, http.StatusConflict, "only the last member", ""},
		{"expired invitation", household.ErrExpired, http.StatusGone, "expired", ""},
		{"used invitation", household.ErrUsed, http.StatusGone, "already been used", ""},
		{"revoked invitation", household.ErrRevoked, http.StatusGone, "withdrawn", ""},
		{"unknown", errors.New("db password is hunter2"), http.StatusInternalServerError, "Something went wrong", "hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			// The page sentence-cases a sentinel's text (#613), so compare
			// without case.
			body := strings.ToLower(html.UnescapeString(rec.Body.String()))
			if !strings.Contains(body, strings.ToLower(tc.wantBody)) {
				t.Errorf("body missing %q:\n%s", tc.wantBody, body)
			}
			if tc.hideBody != "" && strings.Contains(body, strings.ToLower(tc.hideBody)) {
				t.Errorf("body leaks %q:\n%s", tc.hideBody, body)
			}
		})
	}
}

func TestRenderWritesNothingWhenComponentFailsMidway(t *testing.T) {
	c := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("<p>partial"))
		return errors.New("render broke")
	})
	rec := httptest.NewRecorder()
	Render(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusOK, c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "partial") || strings.Contains(rec.Body.String(), "render broke") {
		t.Errorf("body carries the partial render or the error text:\n%s", rec.Body.String())
	}
}

func TestRenderWritesStatusAndBody(t *testing.T) {
	c := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<p>ok</p>"))
		return err
	})
	rec := httptest.NewRecorder()
	Render(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusCreated, c)
	if rec.Code != http.StatusCreated || rec.Body.String() != "<p>ok</p>" {
		t.Errorf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
}

// renderAt serves path through a router that mounts a page at each pattern the
// way package web mounts features, so the layout sees real chi route state.
func renderAt(t *testing.T, path string) string {
	t.Helper()
	return renderAtAs(t, path, memberOperator)
}

// identity is who a rendered request comes from: a household member or not,
// an operator or not.
type identity struct{ member, operator bool }

var (
	memberOperator    = identity{member: true, operator: true}
	memberOnly        = identity{member: true}
	nonMember         = identity{}
	nonMemberOperator = identity{operator: true}
)

// renderAtAs is renderAt for a request from who.
func renderAtAs(t *testing.T, path string, who identity) string {
	t.Helper()
	page := func(w http.ResponseWriter, r *http.Request) {
		if who.operator {
			r = r.WithContext(WithOperator(r.Context()))
		}
		if who.member {
			r = r.WithContext(household.WithContext(r.Context(), household.Household{ID: 1, Name: "Probe"}))
		}
		Render(w, r, http.StatusOK, Page("Probe"))
	}
	r := chi.NewRouter()
	r.Get("/", page)
	for _, pattern := range []string{"/recipes", "/recipes/{id}/cook", "/ingest", "/catalog", "/shopping", "/shares", "/household", "/operator"} {
		sub := chi.NewRouter()
		sub.Get("/", page)
		sub.Get("/*", page)
		r.Mount(pattern, sub)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d:\n%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestNavLinksEveryTopLevelPage(t *testing.T) {
	got := renderAtAs(t, "/", memberOperator)
	for _, href := range []string{"/recipes", "/ingest", "/catalog", "/shopping", "/shares", "/household", "/operator"} {
		if !strings.Contains(got, `href="`+href+`"`) {
			t.Errorf("nav missing link to %s in:\n%s", href, got)
		}
	}
	if strings.Contains(got, "aria-current") {
		t.Errorf("home page marks a nav link current:\n%s", got)
	}
}

func TestNavMarksOnlyTheCurrentSection(t *testing.T) {
	cases := []struct{ path, current string }{
		{"/recipes", "/recipes"},
		{"/recipes/7/cook", "/recipes"},
		{"/ingest", "/ingest"},
		{"/ingest/photo", "/ingest"},
		{"/catalog", "/catalog"},
		{"/shopping/3", "/shopping"},
		{"/shares", "/shares"},
		{"/shares/new/7", "/shares"},
		{"/household", "/household"},
		{"/operator", "/operator"},
		{"/operator/settings", "/operator"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := renderAt(t, tc.path)
			if n := strings.Count(got, `aria-current="page"`); n != 1 {
				t.Fatalf("aria-current marks = %d, want exactly one:\n%s", n, got)
			}
			if !regexp.MustCompile(`<a [^>]*href="` + tc.current + `"[^>]*aria-current="page"`).MatchString(got) {
				t.Errorf("link %s not marked current:\n%s", tc.current, got)
			}
		})
	}
}

func TestNavWithoutRouterMarksNothing(t *testing.T) {
	var b bytes.Buffer
	if err := Page("x").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "aria-current") {
		t.Errorf("no request context, yet a link is current:\n%s", b.String())
	}
}

func TestNavHidesOperatorLinksFromEveryoneElse(t *testing.T) {
	for _, op := range []string{OperatorPath} {
		if got := renderAtAs(t, "/", memberOnly); strings.Contains(got, `href="`+op+`"`) {
			t.Errorf("non-operator nav links operator page %s:\n%s", op, got)
		}
		if got := renderAtAs(t, "/", memberOperator); !strings.Contains(got, `href="`+op+`"`) {
			t.Errorf("operator nav missing operator page %s:\n%s", op, got)
		}
	}
	got := renderAtAs(t, "/", memberOnly)
	for _, href := range []string{"/recipes", "/ingest", "/catalog", "/shopping", "/shares", "/household"} {
		if !strings.Contains(got, `href="`+href+`"`) {
			t.Errorf("non-operator nav lost %s:\n%s", href, got)
		}
	}
}

// The nav and the membership middleware read one rule, Admits: every identity
// sees a link exactly when it may open the page.
func TestNavShowsExactlyTheLinksAdmitsAdmits(t *testing.T) {
	for name, who := range map[string]identity{
		"member operator":     memberOperator,
		"member":              memberOnly,
		"non-member":          nonMember,
		"non-member operator": nonMemberOperator,
	} {
		t.Run(name, func(t *testing.T) {
			got := renderAtAs(t, "/", who)
			for _, item := range navItems {
				want := Admits(item.Path, who.member, who.operator)
				if has := strings.Contains(got, `href="`+item.Path+`"`); has != want {
					t.Errorf("link to %s shown = %v, want %v:\n%s", item.Path, has, want, got)
				}
			}
		})
	}
}

func TestNavHidesMemberLinksFromANonMember(t *testing.T) {
	got := renderAtAs(t, "/", nonMember)
	for _, item := range navItems {
		if strings.Contains(got, `href="`+item.Path+`"`) {
			t.Errorf("non-member nav links %s:\n%s", item.Path, got)
		}
	}
	got = renderAtAs(t, "/", nonMemberOperator)
	for _, item := range navItems {
		want := isOperatorPath(item.Path)
		if has := strings.Contains(got, `href="`+item.Path+`"`); has != want {
			t.Errorf("non-member operator: link to %s shown = %v, want %v", item.Path, has, want)
		}
	}
}

// Sign-in is offered, in the account menu, only where people sign in to the app
// (ADR-00030); with identity from in front of it, neither it nor sign-out is.
// The signed-in case is rendered through the router in package web, which owns
// identity.
func TestHeaderOffersSignInOnlyWithSignIn(t *testing.T) {
	render := func(ctx context.Context) string {
		var b strings.Builder
		if err := Page("Probe").Render(ctx, &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got := render(context.Background()); strings.Contains(got, `href="/login"`) || strings.Contains(got, `action="/logout"`) {
		t.Errorf("without sign-in the header offers it:\n%s", got)
	}
	got := render(WithSignIn(context.Background()))
	if !strings.Contains(got, `href="/login"`) || strings.Contains(got, `action="/logout"`) {
		t.Errorf("an anonymous visitor is not offered sign-in alone:\n%s", got)
	}
	if strings.Contains(got, `aria-label="More sections"`) {
		t.Error("an anonymous visitor is offered the sections menu")
	}
}

// ADR-00032: a visitor with no identity sees the signed-out figure, not an
// avatar, and its menu holds the theme toggle. The old standalone button is
// gone. The signed-in avatar menu is rendered through the router in package
// web, which owns identity.
func TestHeaderShowsTheSignedOutFigureWithTheToggle(t *testing.T) {
	var b strings.Builder
	if err := Page("Probe").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{`data-avatar="signed-out"`, `class="avatar figure"`, `data-theme-toggle`, `data-theme-label`} {
		if !strings.Contains(got, want) {
			t.Errorf("signed-out header missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{`data-avatar="account"`, `modebtn`, `href="/profile"`} {
		if strings.Contains(got, absent) {
			t.Errorf("signed-out header has %q", absent)
		}
	}
	if n := strings.Count(got, `data-theme-toggle`); n != 1 {
		t.Errorf("the page has %d theme toggles, want exactly one (theme.js binds the first)", n)
	}
}

// Sign out left the sections menu for the account menu, so a non-member, who has
// no sections, has no sections menu either.
func TestSectionsMenuHoldsOnlySections(t *testing.T) {
	if got := renderAtAs(t, "/", nonMember); strings.Contains(got, `aria-label="More sections"`) {
		t.Errorf("a non-member is offered an empty sections menu:\n%s", got)
	}
	if got := renderAtAs(t, "/", memberOnly); !strings.Contains(got, `aria-label="More sections"`) {
		t.Error("a member is not offered the sections menu")
	}
	if got := renderAtAs(t, "/", memberOperator); strings.Contains(got, `action="/logout"`) {
		t.Error("the sections menu still offers sign-out")
	}
}

func TestAdmits(t *testing.T) {
	cases := []struct {
		path             string
		member, operator bool
		want             bool
	}{
		{"/recipes", true, false, true},
		{"/recipes", false, false, false},
		{"/recipes", false, true, false},
		{"/operator/settings", true, false, false},
		{"/operator/settings", true, true, true},
		// The deployment page's old address redirects, for an operator only.
		{"/settings", true, false, false},
		{"/settings", false, true, true},
		{"/operator/invitations", false, true, true},
		{"/operator/invitations", false, false, false},
		{"//operator", true, false, false},
		{NotAMemberPath, false, false, true},
		{InvitationsPath + "/7", false, false, true},
		{InvitationsPath, false, false, false},
		{"/static/app.css", false, false, true},
		{"/", false, false, false},
		{"/", true, false, true},
		{SignInPath, false, false, true},
		{CallbackPath, false, false, true},
		{SignOutPath, false, false, true},
		{SignOutPath + "/x", false, false, false},
		// ADR-00032: a non-member opens the profile pages, and nothing that
		// merely starts with the same letters.
		{ProfilePath, false, false, true},
		{ProfilePath + "/", false, false, true},
		{ProfilePath + "/Zm9vYmFy/name", false, false, true},
		{ProfilePath + "s", false, false, false},
		{ProfilePath, true, false, true},
	}
	for _, tc := range cases {
		if got := Admits(tc.path, tc.member, tc.operator); got != tc.want {
			t.Errorf("Admits(%q, member=%v, operator=%v) = %v, want %v", tc.path, tc.member, tc.operator, got, tc.want)
		}
	}
}

// Every icon the header names is drawn. A name with no drawing once left the
// Profile link and the signed-out figure without a picture; the icon set now
// fails the render on one instead.
func TestEveryHeaderIconIsDrawn(t *testing.T) {
	names := []IconName{IconMoon, IconSignIn, IconSignOut, IconUser, IconMenu, IconCode, profileIcon}
	for _, item := range navItems {
		names = append(names, item.Icon)
	}
	for _, name := range names {
		var b strings.Builder
		if err := Icon(name).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`<(path|circle|rect)\b`).MatchString(b.String()) {
			t.Errorf("icon %q draws nothing: %s", name, b.String())
		}
	}
}

// The footer shows the release the binary was linked with, and says plainly
// when it is a local build with none.
func TestFooterShowsTheVersion(t *testing.T) {
	saved := Version
	t.Cleanup(func() { Version = saved })
	for version, want := range map[string]string{"0.98.0": "v0.98.0", "dev": "dev build"} {
		Version = version
		var b strings.Builder
		if err := Page("x").Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), `data-testid="app-version">`+want+`<`) {
			t.Errorf("version %q: footer does not show %q", version, want)
		}
	}
}
