package layout

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/apikey"
)

func renderTo(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestNewAPIKeyRowsSaysWhoMadeAHouseholdKeyOnly(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	keys := []apikey.Key{{ID: 7, Prefix: "gob_abcdefgh", Name: "bot", CreatedBy: "ann@example.com", CreatedAt: now.Add(-24 * time.Hour)}}
	path := func(id int64) string { return "/x/" + strconv.FormatInt(id, 10) + "/revoke" }

	personal := NewAPIKeyRows(keys, now, false, path)
	if got, want := personal[0].Detail, "gob_abcdefgh… made 2026-11-02. No expiry. Never used."; got != want || personal[0].RevokePath != "/x/7/revoke" {
		t.Errorf("personal row = %+v, want detail %q", personal[0], want)
	}
	household := NewAPIKeyRows(keys, now, true, path)
	if got, want := household[0].Detail, "gob_abcdefgh… made by ann@example.com on 2026-11-02. No expiry. Never used."; got != want {
		t.Errorf("household detail = %q, want %q", got, want)
	}
}

func TestAPIKeyComponentsListEscapeAndOfferEveryLifetime(t *testing.T) {
	list := renderTo(t, APIKeyList([]APIKeyRow{{ID: 1, Name: "<b>x</b>", Detail: "d", RevokePath: "/r/1"}}, "none"))
	for _, want := range []string{`action="/r/1"`, "&lt;b&gt;x&lt;/b&gt;", `aria-label="Revoke the key &lt;b&gt;x&lt;/b&gt;"`} {
		if !strings.Contains(list, want) {
			t.Errorf("list missing %q:\n%s", want, list)
		}
	}
	if empty := renderTo(t, APIKeyList(nil, "none yet")); !strings.Contains(empty, "none yet") || strings.Contains(empty, "<ul") {
		t.Errorf("empty list = %s", empty)
	}
	form := renderTo(t, APIKeyForm("/make", "ph"))
	for _, l := range apikey.Lifetimes {
		if !strings.Contains(form, `value="`+l.Value+`"`) {
			t.Errorf("form offers no lifetime %q", l.Value)
		}
	}
	if !strings.Contains(form, `action="/make"`) || !strings.Contains(form, `maxlength="60"`) || !strings.Contains(form, `value="90d" selected`) {
		t.Errorf("form = %s", form)
	}
}

func TestAPIKeyCreatedPageShowsTheSecretAndNamesWhoseKeyItIs(t *testing.T) {
	page := renderTo(t, APIKeyCreatedPage(APIKeyCreated{Name: "n", Secret: "gob_s3cret", For: "for the household H", BackHref: "/back", BackLabel: "Go back"}))
	for _, want := range []string{`value="gob_s3cret"`, "<strong>n</strong> for the household H.", `href="/back"`, "Go back"} {
		if !strings.Contains(page, want) {
			t.Errorf("created page missing %q", want)
		}
	}
	if own := renderTo(t, APIKeyCreatedPage(APIKeyCreated{Name: "n", Secret: "s", BackHref: "/b"})); !strings.Contains(own, "<strong>n</strong>.") {
		t.Errorf("a personal key's page has stray text after the name")
	}
}
