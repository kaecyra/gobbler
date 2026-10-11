package extract

import (
	"encoding/json"
	"errors"
	"flag"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

var update = flag.Bool("update", false, "rewrite the extract golden files")

const fixtureURL = "https://www.example.com/recipes/1"

func mustExtract(t *testing.T, page, src string) Result {
	t.Helper()
	res, err := Extract([]byte(page), src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return res
}

func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".html"), func(t *testing.T) {
			page, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Extract(page, fixtureURL)
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			got, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			golden := strings.TrimSuffix(f, ".html") + ".golden.json"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("output differs from %s (run with -update and review):\ngot:\n%s", golden, got)
			}
			// Deterministic: a second run is byte-identical.
			res2, _ := Extract(page, fixtureURL)
			got2, _ := json.MarshalIndent(res2, "", "  ")
			if string(got2)+"\n" != string(got) {
				t.Error("two runs over the same input differ")
			}
		})
	}
}

func TestMethods(t *testing.T) {
	want := map[string]Method{
		"jsonld_graph": MethodJSONLD, "jsonld_array": MethodJSONLD, "jsonld_object": MethodJSONLD,
		"microdata": MethodMicrodata, "plain": MethodText, "broken": MethodJSONLD,
	}
	for name, m := range want {
		page, err := os.ReadFile("testdata/" + name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		if got := mustExtract(t, string(page), fixtureURL).Method; got != m {
			t.Errorf("%s: method %q, want %q", name, got, m)
		}
	}
}

func TestMalformedBlockDoesNotHideValidBlock(t *testing.T) {
	page, _ := os.ReadFile("testdata/jsonld_graph.html")
	res := mustExtract(t, string(page), fixtureURL)
	if res.Recipe == nil || res.Recipe.Name != "Lemon Bars & Shortbread" {
		t.Fatalf("recipe = %+v", res.Recipe)
	}
	if !reflect.DeepEqual(res.Issues, []string{IssueMalformedJSONLD}) {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestJSONLDWinsOverMicrodata(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{"@type":"Recipe","name":"LD","recipeIngredient":["a"]}</script></head>
<body><div itemscope itemtype="https://schema.org/Recipe"><span itemprop="name">MD</span><span itemprop="recipeIngredient">b</span></div></body></html>`
	res := mustExtract(t, page, fixtureURL)
	if res.Method != MethodJSONLD || res.Recipe.Name != "LD" {
		t.Errorf("got %s %+v", res.Method, res.Recipe)
	}
}

func TestEmptyJSONLDRecipeFallsThroughToMicrodata(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{"@type":"Recipe","name":"Stub"}</script></head>
<body><div itemscope itemtype="https://schema.org/Recipe"><span itemprop="name">MD</span><span itemprop="recipeIngredient">b</span></div></body></html>`
	res := mustExtract(t, page, fixtureURL)
	if res.Method != MethodMicrodata || res.Recipe.Name != "MD" {
		t.Errorf("got %s %+v", res.Method, res.Recipe)
	}
}

type fakeSite struct{ host string }

func (f fakeSite) Host() string { return f.host }
func (f fakeSite) Extract(_ *html.Node, page *url.URL) (Raw, bool) {
	return Raw{Name: "Fake " + page.Path, Ingredients: []string{"1 thing"}}, true
}

func TestSiteExtractorByHost(t *testing.T) {
	var reg Registry
	if err := reg.Register(fakeSite{"Example.com"}); err != nil {
		t.Fatal(err)
	}
	page := []byte(`<html><body><h1>Nothing structured</h1><p>text</p></body></html>`)

	res, err := reg.Extract(page, "https://www.example.com/x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != MethodSite || res.Recipe == nil || res.Recipe.Name != "Fake /x" {
		t.Errorf("registered host: %s %+v", res.Method, res.Recipe)
	}

	res, _ = reg.Extract(page, "https://other.example.org/x")
	if res.Method != MethodText {
		t.Errorf("other host: method %s, want text", res.Method)
	}
}

func TestSiteExtractorRunsAfterStructuredData(t *testing.T) {
	var reg Registry
	_ = reg.Register(fakeSite{"example.com"})
	page := `<html><head><script type="application/ld+json">{"@type":"Recipe","name":"LD","recipeIngredient":["a"]}</script></head></html>`
	res, _ := reg.Extract([]byte(page), "https://example.com/x")
	if res.Method != MethodJSONLD {
		t.Errorf("method %s, want jsonld", res.Method)
	}
}

type decliningSite struct{}

func (decliningSite) Host() string                             { return "example.com" }
func (decliningSite) Extract(*html.Node, *url.URL) (Raw, bool) { return Raw{}, false }

func TestSiteExtractorDeclines(t *testing.T) {
	var reg Registry
	_ = reg.Register(decliningSite{})
	res, _ := reg.Extract([]byte("<p>hello</p>"), "https://example.com/x")
	if res.Method != MethodText {
		t.Errorf("method %s, want text", res.Method)
	}
}

func TestRegisterErrors(t *testing.T) {
	var reg Registry
	if err := reg.Register(fakeSite{"example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeSite{"WWW.example.com"}); err == nil {
		t.Error("duplicate host accepted")
	}
	if err := reg.Register(fakeSite{" "}); err == nil {
		t.Error("empty host accepted")
	}
}

func TestTextFallback(t *testing.T) {
	page, _ := os.ReadFile("testdata/plain.html")
	res := mustExtract(t, string(page), "not a url")
	if res.Method != MethodText || res.Recipe != nil {
		t.Fatalf("method %s recipe %v", res.Method, res.Recipe)
	}
	for _, bad := range []string{"Home", "ignore me", "Copyright", "color:red"} {
		if strings.Contains(res.Text, bad) {
			t.Errorf("text contains chrome %q:\n%s", bad, res.Text)
		}
	}
	for _, good := range []string{"Grandma's Pancakes", "2 cups flour", "Fry on a hot pan."} {
		if !strings.Contains(res.Text, good) {
			t.Errorf("text lacks %q:\n%s", good, res.Text)
		}
	}
	if !reflect.DeepEqual(res.Issues, []string{IssueNoStructuredData}) {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestTextFallbackKeepsH1OutsideMain(t *testing.T) {
	res := mustExtract(t, `<body><h1>Title</h1><main><p>body text</p></main></body>`, "")
	if !strings.HasPrefix(res.Text, "Title\n") || !strings.Contains(res.Text, "body text") {
		t.Errorf("text = %q", res.Text)
	}
}

func TestNoText(t *testing.T) {
	res := mustExtract(t, `<html><head><script>x=1</script></head><body><nav>menu</nav></body></html>`, "")
	if res.Method != MethodNone || res.Text != "" {
		t.Errorf("got %s %q", res.Method, res.Text)
	}
	if !reflect.DeepEqual(res.Issues, []string{IssueNoStructuredData, IssueNoText}) {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestEmptyInput(t *testing.T) {
	for _, in := range []string{"", "  \n\t"} {
		if _, err := Extract([]byte(in), ""); !errors.Is(err, ErrEmptyInput) {
			t.Errorf("Extract(%q) err = %v", in, err)
		}
	}
}

func TestGarbageDoesNotPanic(t *testing.T) {
	for _, in := range []string{
		"\x00\x01<<<>>>", "<script type=application/ld+json>", `<script type="application/ld+json">null</script>`,
		`<script type="application/ld+json">{"@type":"Recipe","recipeInstructions":[null,5,{"@type":"HowToSection"}],"recipeIngredient":7,"nutrition":3}</script>`,
		`<div itemscope itemtype="Recipe"><div itemprop="recipeInstructions" itemscope></div></div>`,
	} {
		if _, err := Extract([]byte(in), "https://x.test/"); err != nil {
			t.Errorf("Extract(%q): %v", in, err)
		}
	}
}

func TestMicrodataNestedItemDoesNotLeakProperties(t *testing.T) {
	page := `<div itemscope itemtype="https://schema.org/Recipe">
<span itemprop="author" itemscope itemtype="https://schema.org/Person"><span itemprop="name">Pat</span></span>
<h1 itemprop="name">Soup</h1><span itemprop="recipeIngredient">water</span></div>`
	res := mustExtract(t, page, "")
	if res.Method != MethodMicrodata || res.Recipe.Name != "Soup" || res.Recipe.Author != "Pat" {
		t.Errorf("got %s %+v", res.Method, res.Recipe)
	}
}
