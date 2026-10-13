package journal

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/household"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// env is a real SQLite file with every migration applied, the real recipe
// store over it, the journal routes mounted, and a request context carrying
// the verified dev identity (only the auth middleware can mint one).
type env struct {
	t     *testing.T
	ctx   context.Context
	store *store.Store
	h     http.Handler
	id    int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	var authed context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { authed = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if authed == nil {
		t.Fatal("auth middleware did not call the handler")
	}
	hh := devHousehold(t, d)
	authed = household.WithContext(authed, hh)
	s := store.New(d)
	id, err := s.Create(authed, recipe.Recipe{
		Name:       "Journal <Cake>",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "mix", Text: "Mix."}}}},
	})
	if err != nil {
		t.Fatalf("create recipe: %v", err)
	}
	r := chi.NewRouter()
	// The real router authenticates before the features; do the same.
	r.Use(a.Middleware(), asMember(hh))
	r.Mount(Pattern, Routes(Deps{Journal: s}))
	return &env{t: t, ctx: authed, store: s, h: r, id: id}
}

// notes, made, rating and who read back through the store and fail the test on
// any error, so an assertion that nothing was saved cannot pass on a failed
// read.
func (e *env) notes() []store.Note {
	e.t.Helper()
	n, err := e.store.ListNotes(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("ListNotes: %v", err)
	}
	return n
}

func (e *env) made() []store.MadeIt {
	e.t.Helper()
	m, err := e.store.ListMadeIt(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("ListMadeIt: %v", err)
	}
	return m
}

func (e *env) rating() *int {
	e.t.Helper()
	r, err := e.store.Get(e.ctx, e.id)
	if err != nil {
		e.t.Fatalf("Get: %v", err)
	}
	return r.Rating
}

func (e *env) who() string {
	e.t.Helper()
	w, ok := auth.EmailFromContext(e.ctx)
	if !ok {
		e.t.Fatal("no identity on the test context")
	}
	return w
}

func (e *env) path(suffix string) string { return fmt.Sprintf("/recipes/%d/journal%s", e.id, suffix) }

func (e *env) do(method, path string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prev)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

var htmx = []string{"HX-Request", "true"}

func mustContain(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\n%s", w, body)
		}
	}
}

func mustNotContain(t *testing.T, body string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(body, w) {
			t.Errorf("body contains %q\n%s", w, body)
		}
	}
}

func TestEmptyJournalFragment(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodGet, e.path(""), nil, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, `id="journal"`, "No notes yet.", "Not made yet.")
	mustNotContain(t, body, "<html", "<title>")
}

func TestJournalPageWithoutHTMXIsAFullPage(t *testing.T) {
	e := newEnv(t)
	body := e.do(http.MethodGet, e.path(""), nil).Body.String()
	mustContain(t, body, "<html", "Journal &lt;Cake&gt;", `id="journal"`)
}

func TestNotesPersistWithVerifiedAuthorNewestFirst(t *testing.T) {
	e := newEnv(t)
	for _, b := range []string{"first <b>note</b>", "second note"} {
		// created_by in the form must be ignored: the author is the identity.
		rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {b}, "created_by": {"mallory@example.com"}}, htmx...)
		if rec.Code != http.StatusOK {
			t.Fatalf("post note = %d\n%s", rec.Code, rec.Body)
		}
	}
	notes := e.notes()
	if len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}
	who := e.who()
	for _, n := range notes {
		if n.CreatedBy != who || strings.Contains(n.CreatedBy, "mallory") {
			t.Errorf("author = %q, want %q", n.CreatedBy, who)
		}
	}
	body := e.do(http.MethodGet, e.path(""), nil, htmx...).Body.String()
	mustContain(t, body, "first &lt;b&gt;note&lt;/b&gt;", "second note", who)
	mustNotContain(t, body, "mallory", "<b>note</b>")
	if strings.Index(body, "second note") > strings.Index(body, "first &lt;b&gt;") {
		t.Errorf("notes are not newest first\n%s", body)
	}
}

func TestEmptyNoteIsRejectedInline(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {"   "}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so htmx swaps the message", rec.Code)
	}
	mustContain(t, rec.Body.String(), `role="alert"`, "Write something")
	if notes := e.notes(); len(notes) != 0 {
		t.Errorf("saved %+v", notes)
	}
}

func TestPlainPostRedirectsAndInvalidPlainPostIs400(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {"hi"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != e.path("") {
		t.Errorf("redirect = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {""}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid plain post = %d, want 400", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Write something")
}

func TestRatingSetAndClear(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/rating"), url.Values{"rating": {"4"}}, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	if got := e.rating(); got == nil || *got != 4 {
		t.Fatalf("rating = %v, want 4", got)
	}
	mustContain(t, rec.Body.String(), `<option value="4" selected>`)

	e.do(http.MethodPost, e.path("/rating"), url.Values{"rating": {""}}, htmx...)
	if got := e.rating(); got != nil {
		t.Fatalf("rating = %d, want cleared", *got)
	}
}

func TestRatingOutOfRangeIsRejectedAndNotSaved(t *testing.T) {
	e := newEnv(t)
	for _, v := range []string{"0", "6", "-1", "abc", "2.5"} {
		rec := e.do(http.MethodPost, e.path("/rating"), url.Values{"rating": {v}}, htmx...)
		if rec.Code != http.StatusOK {
			t.Errorf("%q: status = %d", v, rec.Code)
		}
		mustContain(t, rec.Body.String(), `role="alert"`, "whole number from 1 to 5")
		if got := e.rating(); got != nil {
			t.Errorf("%q saved rating %d", v, *got)
		}
	}
}

func TestRatingWritesARevisionThroughTheStore(t *testing.T) {
	e := newEnv(t)
	e.do(http.MethodPost, e.path("/rating"), url.Values{"rating": {"5"}}, htmx...)
	revs, err := e.store.ListRevisions(e.ctx, e.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("revisions = %+v, want one from the rating edit", revs)
	}
}

func TestMadeItWithAndWithoutOptionalFields(t *testing.T) {
	e := newEnv(t)
	today := time.Now().UTC().Format("2006-01-02")
	// Bare: date defaults to today.
	if rec := e.do(http.MethodPost, e.path("/made"), url.Values{"on": {""}}, htmx...); rec.Code != http.StatusOK {
		t.Fatalf("bare = %d\n%s", rec.Code, rec.Body)
	}
	// Full.
	full := url.Values{"on": {"2024-03-09"}, "rating": {"5"}, "comment": {"Great <i>crumb</i>"}}
	rec := e.do(http.MethodPost, e.path("/made"), full, htmx...)
	if rec.Code != http.StatusOK {
		t.Fatalf("full = %d\n%s", rec.Code, rec.Body)
	}
	entries := e.made()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	who := e.who()
	var bare, fullE store.MadeIt
	for _, m := range entries {
		if m.CreatedBy != who {
			t.Errorf("author = %q, want %q", m.CreatedBy, who)
		}
		if m.Comment == "" {
			bare = m
		} else {
			fullE = m
		}
	}
	if bare.On.Format("2006-01-02") != today || bare.Rating != nil {
		t.Errorf("bare = %+v, want today and no rating", bare)
	}
	if fullE.On.Format("2006-01-02") != "2024-03-09" || fullE.Rating == nil || *fullE.Rating != 5 {
		t.Errorf("full = %+v", fullE)
	}
	body := e.do(http.MethodGet, e.path(""), nil, htmx...).Body.String()
	mustContain(t, body, "2024-03-09", "Great &lt;i&gt;crumb&lt;/i&gt;", today)
	if strings.Index(body, today) > strings.Index(body, "2024-03-09") {
		t.Errorf("made-it log is not newest first\n%s", body)
	}
}

func TestMadeItInvalidInputIsRejectedInline(t *testing.T) {
	e := newEnv(t)
	for name, form := range map[string]url.Values{
		"not a date":      {"on": {"yesterday"}},
		"impossible date": {"on": {"2024-02-30"}},
		"bad rating":      {"on": {"2024-03-09"}, "rating": {"9"}},
	} {
		rec := e.do(http.MethodPost, e.path("/made"), form, htmx...)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", name, rec.Code)
		}
		mustContain(t, rec.Body.String(), `role="alert"`)
	}
	if entries := e.made(); len(entries) != 0 {
		t.Errorf("saved %+v", entries)
	}
	// The typed values are kept so the user can fix them.
	rec := e.do(http.MethodPost, e.path("/made"), url.Values{"on": {"yesterday"}, "comment": {"keep me"}}, htmx...)
	mustContain(t, rec.Body.String(), `value="yesterday"`, "keep me", "YYYY-MM-DD")
}

func TestUnknownRecipeIs404(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		method, path string
		form         url.Values
	}{
		{http.MethodGet, "/recipes/9999/journal", nil},
		{http.MethodPost, "/recipes/9999/journal/notes", url.Values{"body": {"x"}}},
		{http.MethodPost, "/recipes/9999/journal/rating", url.Values{"rating": {"3"}}},
		{http.MethodPost, "/recipes/9999/journal/made", url.Values{"on": {"2024-01-01"}}},
		{http.MethodGet, "/recipes/abc/journal", nil},
	} {
		if rec := e.do(c.method, c.path, c.form, htmx...); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", c.method, c.path, rec.Code)
		}
	}
}

func TestOversizedFormIsRefused(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, e.path("/notes"), url.Values{"body": {strings.Repeat("x", maxFormBytes+1)}}, htmx...)
	if rec.Code < 400 {
		t.Errorf("status = %d, want an error", rec.Code)
	}
	if notes := e.notes(); len(notes) != 0 {
		t.Errorf("saved %d notes", len(notes))
	}
}

// devHousehold returns the household the membership middleware resolves for
// the dev identity, creating it in d the way the dev bypass does.
func devHousehold(t *testing.T, d *sql.DB) household.Household {
	t.Helper()
	hs := household.New(d)
	if err := hs.Seed(context.Background(), "Dev household", auth.DevEmail); err != nil {
		t.Fatalf("seed household: %v", err)
	}
	h, err := hs.ForEmail(context.Background(), auth.DevEmail)
	if err != nil {
		t.Fatalf("household of %s: %v", auth.DevEmail, err)
	}
	return h
}

// asMember puts h on every request, as the membership middleware does for a
// member.
func asMember(h household.Household) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(household.WithContext(r.Context(), h)))
		})
	}
}
