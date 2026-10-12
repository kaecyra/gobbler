package ingest

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/store"
)

func TestFromURLReadsJSONLDRecipe(t *testing.T) {
	e := newEnv(t)
	page := readFixture(t, "pancakes.html")
	srv, userAgent := serve(t, page)

	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}

	if d.Name.Value != "Fluffy Pancakes" || d.Author.Value != "Pat Baker" {
		t.Errorf("name/author = %q/%q", d.Name.Value, d.Author.Value)
	}
	if got := userAgent(); got != testUserAgent {
		t.Errorf("User-Agent = %q, want the configured %q", got, testUserAgent)
	}
	if d.Prep.Value != 10*time.Minute || d.Cook.Value != 15*time.Minute {
		t.Errorf("times = %v / %v", d.Prep.Value, d.Cook.Value)
	}
	if len(d.Components) != 1 || len(d.Components[0].Lines) != 7 || len(d.Components[0].Steps) != 3 {
		t.Fatalf("components = %+v", d.Components)
	}
	for _, l := range d.Components[0].Lines {
		if l.Ingredient == 0 {
			t.Errorf("line %q did not match a catalog ingredient", l.Raw)
		}
	}
	if got := d.Components[0].Steps[2].Duration; got != 2*time.Minute {
		t.Errorf("griddle step duration = %v, want 2m", got)
	}
	if d.BelowThreshold || d.Overall < 0.9 {
		t.Errorf("clean JSON-LD scored %.2f below=%v", d.Overall, d.BelowThreshold)
	}
	if d.Source.Kind != store.SnapshotHTML || !bytes.Equal(d.Source.Content, page) {
		t.Errorf("source snapshot is not the fetched page byte for byte")
	}
	if len(d.Proposed) != 0 {
		t.Errorf("proposed ingredients = %+v, want none", d.Proposed)
	}
}

func TestFromTextSplitsAndParsesPastedText(t *testing.T) {
	e := newEnv(t)
	text := readFixture(t, "garlic_spaghetti.txt")

	d, err := e.pipeline(t, nil).FromText(e.ctx, string(text))
	if err != nil {
		t.Fatalf("FromText: %v", err)
	}
	if d.Name.Value != "Garlic Spaghetti" {
		t.Errorf("name = %q", d.Name.Value)
	}
	lines := d.Components[0].Lines
	if len(lines) != 4 || !lines[3].ToTaste {
		t.Fatalf("lines = %+v", lines)
	}
	if d.Source.Kind != store.SnapshotText || string(d.Source.Content) != string(text) {
		t.Errorf("pasted text must be kept as given")
	}
	if d.BelowThreshold {
		t.Errorf("clean text scored %.2f", d.Overall)
	}
}

func TestLowConfidenceInputIsADraftNotAnError(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, string(readFixture(t, "mystery_stew.txt")))
	if err != nil {
		t.Fatalf("a hard line must not abort the ingest: %v", err)
	}
	if !d.BelowThreshold || d.Overall >= d.Threshold {
		t.Errorf("overall %.2f threshold %.2f below=%v", d.Overall, d.Threshold, d.BelowThreshold)
	}
	var names []string
	for _, p := range d.Proposed {
		names = append(names, p.Name)
	}
	if len(names) == 0 {
		t.Fatalf("unmatched ingredients must be proposed")
	}
	good := d.Components[0].Lines[1]
	bad := d.Components[0].Lines[0]
	if !(bad.Confidence < good.Confidence) {
		t.Errorf("unmatched line %.2f should score below matched line %.2f", bad.Confidence, good.Confidence)
	}
	if len(d.ProposedUnits) != 1 || d.ProposedUnits[0].Name != "bag" {
		t.Errorf("proposed units = %+v, want bag", d.ProposedUnits)
	}
}

func TestThresholdIsConfiguration(t *testing.T) {
	e := newEnv(t)
	cfg := testConfig()
	cfg.ConfidenceThreshold = 1
	p, err := New(cfg, Deps{Catalog: e.catalog})
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.FromText(e.ctx, string(readFixture(t, "mystery_stew.txt")))
	if err != nil {
		t.Fatal(err)
	}
	if d.Threshold != 1 || !d.BelowThreshold {
		t.Errorf("threshold %.2f below=%v", d.Threshold, d.BelowThreshold)
	}
	cfg.ConfidenceThreshold = 0
	p, _ = New(cfg, Deps{Catalog: e.catalog})
	if d, _ = p.FromText(e.ctx, string(readFixture(t, "mystery_stew.txt"))); d.BelowThreshold {
		t.Errorf("threshold 0 must never be below")
	}
}

func TestFetchFailureIsAnError(t *testing.T) {
	e := newEnv(t)
	cases := map[string]struct {
		client HTTPClient
		url    string
	}{
		"http error status": {roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusInternalServerError, "text/html", "boom"), nil
		}), "https://example.com/r"},
		"transport error": {roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}), "https://example.com/r"},
		"not html": {roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, "application/pdf", "%PDF"), nil
		}), "https://example.com/r"},
		"unsupported scheme": {roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("must not fetch")
			return nil, nil
		}), "file:///etc/passwd"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := e.pipeline(t, c.client).FromURL(e.ctx, c.url)
			if !errors.Is(err, ErrFetch) {
				t.Fatalf("err = %v, want ErrFetch", err)
			}
			if !reflect.DeepEqual(d, Draft{}) {
				t.Errorf("a failed fetch must not yield a draft")
			}
		})
	}
}

func TestFetchTimeoutIsAnError(t *testing.T) {
	e := newEnv(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	cfg := testConfig()
	cfg.FetchTimeout = 50 * time.Millisecond
	p, err := New(cfg, Deps{HTTP: srv.Client(), Catalog: e.catalog})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.FromURL(e.ctx, srv.URL); !errors.Is(err, ErrFetch) {
		t.Fatalf("err = %v, want ErrFetch", err)
	}
}

func TestNewRejectsUnusableConfig(t *testing.T) {
	e := newEnv(t)
	for name, mutate := range map[string]func(*Deps){
		"no catalog": func(d *Deps) { d.Catalog = nil },
	} {
		d := Deps{Catalog: e.catalog}
		mutate(&d)
		if _, err := New(testConfig(), d); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	cfg := testConfig()
	cfg.FetchTimeout = 0
	if _, err := New(cfg, Deps{Catalog: e.catalog}); err == nil {
		t.Error("zero fetch timeout: want error")
	}
	cfg = testConfig()
	cfg.UserAgent = ""
	if _, err := New(cfg, Deps{Catalog: e.catalog}); err == nil {
		t.Error("empty user agent: want error")
	}
}

func TestHighConfidenceInputMakesNoProviderCall(t *testing.T) { // ADR-00006 obligation 2
	e := newEnv(t)
	srv, _ := serve(t, readFixture(t, "pancakes.html"))
	p := e.pipeline(t, srv.Client())
	if _, err := p.FromURL(e.ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FromText(e.ctx, string(readFixture(t, "garlic_spaghetti.txt"))); err != nil {
		t.Fatal(err)
	}
	if n := e.spy.count(); n != 0 {
		t.Fatalf("provider called %d times for high-confidence input", n)
	}
}

func TestIngestIsDeterministic(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, readFixture(t, "pancakes.html"))
	for name, in := range map[string]func(*Pipeline) (Draft, error){
		"url":  func(p *Pipeline) (Draft, error) { return p.FromURL(e.ctx, srv.URL+"/p") },
		"text": func(p *Pipeline) (Draft, error) { return p.FromText(e.ctx, string(readFixture(t, "mystery_stew.txt"))) },
	} {
		a, err := in(e.pipeline(t, srv.Client()))
		if err != nil {
			t.Fatal(err)
		}
		b, err := in(e.pipeline(t, srv.Client()))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: same input gave different drafts:\n%+v\n%+v", name, a, b)
		}
		if a.ParserVersion != ParserVersion {
			t.Errorf("%s: draft does not record the parser version", name)
		}
	}
}

// The catalog owns unit names and aliases: "pieces" is its alias of "each",
// while the parser's own table calls it a separate unit "piece".
func TestUnitsResolveThroughTheCatalog(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Ginger Thing\n\nIngredients\n2 pieces garlic\n\nDirections\n1. Mix.\n")
	if err != nil {
		t.Fatal(err)
	}
	l := d.Components[0].Lines[0]
	if l.Amount.Unit.Name != "each" {
		t.Errorf("unit = %q, want the catalog's %q", l.Amount.Unit.Name, "each")
	}
	if len(d.ProposedUnits) != 0 {
		t.Errorf("proposed units = %+v, want none", d.ProposedUnits)
	}
}

func TestPageWithoutARecipeIsALowConfidenceDraft(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte("<html><body></body></html>"))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL)
	if err != nil {
		t.Fatalf("an empty page is data, not an error: %v", err)
	}
	if !d.BelowThreshold || len(d.Components) != 0 {
		t.Errorf("draft = %+v", d)
	}
	if _, err := Confirm(d); err == nil {
		t.Error("an empty draft must not confirm")
	}
}

func TestFromTextRejectsEmptyText(t *testing.T) {
	e := newEnv(t)
	if _, err := e.pipeline(t, nil).FromText(e.ctx, " \n "); err == nil {
		t.Fatal("want error")
	}
}

func TestProposalsNameEachNewIngredientOnce(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Stew\n\nIngredients\n1 cup quuxweed\n2 tbsp quuxweed, chopped\n\nDirections\n1. Stir.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Proposed) != 1 || d.Proposed[0].Name != "quuxweed" {
		t.Errorf("proposed = %+v, want quuxweed once", d.Proposed)
	}
}
