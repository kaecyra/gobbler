package layout

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
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
		`<nav class="site-nav"`,
		`<main id="content">`,
		"<p>slot content</p>",
		`href="` + StylesheetPath + `"`,
		`src="` + HTMXPath + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page missing %q in:\n%s", want, got)
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
		{"not found", ErrNotFound, http.StatusNotFound, "not found", ""},
		{"invalid", ErrInvalid, http.StatusBadRequest, "invalid request", ""},
		{"conflict", ErrConflict, http.StatusConflict, "conflict", ""},
		{"catalog not found", catalog.ErrNotFound, http.StatusNotFound, "catalog entry not found", ""},
		{"catalog invalid", catalog.ErrInvalid, http.StatusBadRequest, "invalid catalog entry", ""},
		{"catalog conflict", catalog.ErrConflict, http.StatusConflict, "already in use", ""},
		{"wrapped keeps sentinel text only", fmt.Errorf("recipe 7: %w", ErrNotFound), http.StatusNotFound, "not found", "recipe 7"},
		{"driver text under a conflict", fmt.Errorf("insert ingredient: %w: constraint failed: UNIQUE constraint failed: ingredient.name (2067)", catalog.ErrConflict), http.StatusConflict, "catalog name or alias already in use", "UNIQUE constraint"},
		{"unknown", errors.New("db password is hunter2"), http.StatusInternalServerError, "Something went wrong", "hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tc.err)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.wantBody) {
				t.Errorf("body missing %q:\n%s", tc.wantBody, body)
			}
			if tc.hideBody != "" && strings.Contains(body, tc.hideBody) {
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
	page := func(w http.ResponseWriter, r *http.Request) {
		Render(w, r, http.StatusOK, Page("Probe"))
	}
	r := chi.NewRouter()
	r.Get("/", page)
	for _, pattern := range []string{"/recipes", "/recipes/{id}/cook", "/ingest", "/catalog", "/shopping", "/settings"} {
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
	got := renderAt(t, "/")
	for _, href := range []string{"/recipes", "/ingest", "/catalog", "/shopping", "/settings"} {
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
		{"/settings", "/settings"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := renderAt(t, tc.path)
			if n := strings.Count(got, `aria-current="page"`); n != 1 {
				t.Fatalf("aria-current marks = %d, want exactly one:\n%s", n, got)
			}
			if !strings.Contains(got, `href="`+tc.current+`" aria-current="page"`) {
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
