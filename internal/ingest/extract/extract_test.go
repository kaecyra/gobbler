package extract

import (
	"encoding/json"
	"errors"
	"flag"
	stdhtml "html"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func register(t *testing.T, reg *Registry, e SiteExtractor) {
	t.Helper()
	if err := reg.Register(e); err != nil {
		t.Fatal(err)
	}
}

func extractWith(t *testing.T, reg *Registry, page, src string) Result {
	t.Helper()
	res, err := reg.Extract([]byte(page), src)
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
			res2, err := Extract(page, fixtureURL)
			if err != nil {
				t.Fatal(err)
			}
			got2, err := json.MarshalIndent(res2, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if string(got2)+"\n" != string(got) {
				t.Error("two runs over the same input differ")
			}
		})
	}
}

func TestMethods(t *testing.T) {
	want := map[string]Method{
		"jsonld_graph": MethodJSONLD, "jsonld_array": MethodJSONLD, "jsonld_object": MethodJSONLD,
		"real_yoast_graph": MethodJSONLD, "real_wprm_graph": MethodJSONLD, "real_bare_objects": MethodJSONLD,
		"microdata": MethodMicrodata, "plain": MethodText, "broken": MethodJSONLD,
	}
	for name, m := range want {
		if got := mustExtract(t, fixture(t, name), fixtureURL).Method; got != m {
			t.Errorf("%s: method %q, want %q", name, got, m)
		}
	}
}

func TestMalformedBlockDoesNotHideValidBlock(t *testing.T) {
	res := mustExtract(t, fixture(t, "jsonld_graph"), fixtureURL)
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

	res = extractWith(t, &reg, string(page), "https://other.example.org/x")
	if res.Method != MethodText {
		t.Errorf("other host: method %s, want text", res.Method)
	}
}

func TestSiteExtractorRunsAfterStructuredData(t *testing.T) {
	var reg Registry
	register(t, &reg, fakeSite{"example.com"})
	page := `<html><head><script type="application/ld+json">{"@type":"Recipe","name":"LD","recipeIngredient":["a"]}</script></head></html>`
	res := extractWith(t, &reg, page, "https://example.com/x")
	if res.Method != MethodJSONLD {
		t.Errorf("method %s, want jsonld", res.Method)
	}
}

type decliningSite struct{}

func (decliningSite) Host() string                             { return "example.com" }
func (decliningSite) Extract(*html.Node, *url.URL) (Raw, bool) { return Raw{}, false }

func TestSiteExtractorDeclines(t *testing.T) {
	var reg Registry
	register(t, &reg, decliningSite{})
	res := extractWith(t, &reg, "<p>hello</p>", "https://example.com/x")
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
	res := mustExtract(t, fixture(t, "plain"), "not a url")
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

func TestRealPagesYieldFullRecipes(t *testing.T) {
	for _, name := range []string{"real_yoast_graph", "real_wprm_graph", "real_bare_objects"} {
		res := mustExtract(t, fixture(t, name), fixtureURL)
		r := res.Recipe
		if res.Method != MethodJSONLD || r == nil || r.Name == "" || len(r.Ingredients) < 2 || len(r.Instructions) < 2 {
			t.Errorf("%s: %s %+v", name, res.Method, r)
			continue
		}
		if r.TotalDuration == 0 && r.PrepDuration == 0 && r.CookDuration == 0 {
			t.Errorf("%s: no time parsed: %+v", name, r)
		}
		if len(res.Issues) != 0 {
			t.Errorf("%s: issues %v", name, res.Issues)
		}
	}
}

func TestMicrodataNestedItemWithoutItempropIsNotTheRecipe(t *testing.T) {
	page := `<div itemscope itemtype="https://schema.org/Recipe">
<div itemscope itemtype="https://schema.org/Review"><span itemprop="author">Bob</span><p itemprop="description">Loved it</p><span itemprop="name">Review title</span></div>
<h1 itemprop="name">Soup</h1><span itemprop="author">Ann</span><p itemprop="description">Real</p><span itemprop="recipeIngredient">water</span></div>`
	r := mustExtract(t, page, "").Recipe
	if r == nil || r.Name != "Soup" || r.Author != "Ann" || r.Description != "Real" {
		t.Errorf("recipe = %+v", r)
	}
}

func TestTextFallbackKeepsH1InsideSkippedHeader(t *testing.T) {
	res := mustExtract(t, `<body><header><h1>Pancakes</h1></header><div><p>mix and fry</p></div></body>`, "")
	if !strings.HasPrefix(res.Text, "Pancakes\n") || !strings.Contains(res.Text, "mix and fry") {
		t.Errorf("text = %q", res.Text)
	}
}

func TestJSONLDRecipeUnderMainEntity(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"WebPage","mainEntity":{"@type":"Recipe","name":"Deep","recipeIngredient":["x"]}}</script>`
	r := mustExtract(t, page, "").Recipe
	if r == nil || r.Name != "Deep" {
		t.Errorf("recipe = %+v", r)
	}
	// Too deep is not chased.
	deep := `{"@type":"Recipe","name":"Deep","recipeIngredient":["x"]}`
	for i := 0; i < 8; i++ {
		deep = `{"a":` + deep + `}`
	}
	res := mustExtract(t, `<script type="application/ld+json">`+deep+`</script>`, "")
	if res.Recipe != nil {
		t.Errorf("found recipe past the depth bound: %+v", res.Recipe)
	}
}

func TestDurations(t *testing.T) {
	const h, m = time.Hour, time.Minute
	for _, tc := range []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{"PT1H30M", h + 30*m, false},
		{"PT90M", 90 * m, false},
		{"P0DT0H45M", 45 * m, false},
		{"PT45M", 45 * m, false},
		{"P1DT2H", 26 * h, false},
		{"30 minutes", 30 * m, false},
		{"", 0, false},
		{"PT0.5H", 0, true}, // fractions are not accepted by the shared duration parser
		{"1-2 hours", 0, true},
		{"overnight", 0, true},
		{"PT", 0, true},
	} {
		raw := Raw{CookTime: tc.in}
		issues := raw.parseDurations()
		if raw.CookDuration != tc.want || (len(issues) == 1 && issues[0] != IssueUnparsedDuration) || (len(issues) > 0) != tc.bad {
			t.Errorf("%q: duration %v issues %v, want %v bad=%v", tc.in, raw.CookDuration, issues, tc.want, tc.bad)
		}
	}
}

func TestBadDurationIsIssueNotError(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"Recipe","name":"R","recipeIngredient":["x"],"prepTime":"PT10M","cookTime":"soonish"}</script>`
	res := mustExtract(t, page, "")
	if res.Recipe == nil || res.Recipe.PrepDuration != 10*time.Minute || res.Recipe.CookDuration != 0 || res.Recipe.CookTime != "soonish" {
		t.Fatalf("recipe = %+v", res.Recipe)
	}
	if !reflect.DeepEqual(res.Issues, []string{IssueUnparsedDuration}) {
		t.Errorf("issues = %v", res.Issues)
	}
}

func TestInstructionsGivenAsOneHTMLString(t *testing.T) {
	want := []string{"Mix the flour.", "Bake it.", "Cool & serve."}
	for name, in := range map[string]string{
		"list":         `<ol><li>Mix the flour.</li><li>Bake it.</li><li>Cool &amp; serve.</li></ol>`,
		"paragraphs":   `<p>Mix the flour.</p><p>Bake it.</p><p>Cool &amp; serve.</p>`,
		"breaks":       `Mix the flour.<br>Bake it.<br/>Cool &amp; serve.`,
		"escaped list": `&lt;ol&gt;&lt;li&gt;Mix the flour.&lt;/li&gt;&lt;li&gt;Bake it.&lt;/li&gt;&lt;li&gt;Cool &amp;amp; serve.&lt;/li&gt;&lt;/ol&gt;`,
		"inline kept":  `<ul><li>Mix the <b>flour</b>.</li><li>Bake it.</li><li>Cool &amp; serve.</li></ul>`,
	} {
		page := `<script type="application/ld+json">{"@type":"Recipe","name":"R","recipeIngredient":["x"],"recipeInstructions":` + mustJSON(t, in) + `}</script>`
		r := mustExtract(t, page, "").Recipe
		var got []string
		for _, s := range r.Instructions {
			got = append(got, s.Text)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: steps = %q, want %q", name, got, want)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCleanTextMarkup(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<b>a</b><i>b</i>", "a b"},
		{"<h3>Notes</h3>Serve warm.", "Notes Serve warm."},
		{"1<sup>1</sup>/<sub>2</sub> cups flour", "1 1/2 cups flour"},
		{"1<sup>1</sup>&frasl;<sub>2</sub> cups flour", "1 1/2 cups flour"},
		{"1<sup>1</sup>&#8260;<sub>2</sub> cups flour", "1 1/2 cups flour"},
		{"1<sup>1</sup>\u2044<sub>2</sub> cups flour", "1 1/2 cups flour"},
		{"1<sup>1</sup>/2 cups flour", "1 1/2 cups flour"},
		{"2 <b>cups</b><i>flour</i>", "2 cups flour"},
		{"<b>flour</b>.", "flour."},
		{"<b>flour</b>, sifted", "flour, sifted"},
		{"(<b>sifted</b>)", "(sifted)"},
		{"a<hr>b", "a b"},
		{"<td>a</td><td>b</td>", "a b"},
		{"Cool &amp; serve&nbsp;now", "Cool & serve now"},
		{"Add .5 cups <b>water</b>", "Add .5 cups water"},
		{"1 cup <i>plus</i> .5 tsp salt", "1 cup plus .5 tsp salt"},
		{"<b>1</b>/3 cup", "1/3 cup"},
		{"the 1<sup>st</sup> rise", "the 1st rise"},
		{"Heat to 350<sup>\u00b0</sup>F", "Heat to 350\u00b0F"},
		{"Heat to 350<sup>&deg;</sup>F", "Heat to 350\u00b0F"},
		{"flour<sup>1</sup> and sugar<sup>2</sup>", "flour and sugar"},
		{"serves 4<sup>1</sup>", "serves 4 1"},
		{"Heat to 350<sup>o</sup>F", "Heat to 350\u00b0F"},
		{"Heat to 350<sup>\u00ba</sup>F", "Heat to 350\u00b0F"},
		{"foo<sup>o</sup>", "foo o"},
		{"flour,<sup>1</sup> sifted", "flour, sifted"},
		{"Sift the flour.<sup>2</sup> Mix", "Sift the flour. Mix"},
		{"(flour)<sup>3</sup>", "(flour)"},
		{"a < b and c > d", "a < b and c > d"},
	} {
		if got := cleanText(tc.in); got != tc.want {
			t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Text the page wrote is never reshaped: with no markup, cleanText is
// entity decoding plus whitespace collapse and nothing else.
func TestCleanTextLeavesMarkupFreeTextAlone(t *testing.T) {
	for _, in := range []string{
		"Add .5 cups water", "1 cup plus .5 tsp salt", "a ( b ) c", "x , y ; z", "3 < 4 > 2",
		"  two   spaces\tand\nnewline ", "&amp; &lt;b&gt; &frac12; cup", "1 1/2 cups", "caf\u00e9 \u2044 x", "",
	} {
		if got, want := cleanText(in), collapse(stdhtml.UnescapeString(in)); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlockElementsSharedWithTextFallback(t *testing.T) {
	for _, tag := range []string{"td", "th", "thead", "tbody", "tfoot", "h1", "hr", "blockquote", "section", "figcaption", "pre", "dd"} {
		if !blockElements[tag] {
			t.Errorf("%s is not a block element", tag)
		}
	}
	// The fallback text splits at a table cell, the same set cleanText uses.
	res := mustExtract(t, "<body><table><tr><td>one</td><td>two</td></tr></table></body>", "")
	if res.Text != "one\ntwo" {
		t.Errorf("text = %q", res.Text)
	}
}

func TestInstructionSplitsAtHeadingsAndTableCells(t *testing.T) {
	got := stepsFromText("<h3>Prep</h3>Chop it.<table><tr><td>Fry it.</td><td>Eat it.</td></tr></table>", "")
	var texts []string
	for _, s := range got {
		texts = append(texts, s.Text)
	}
	if want := []string{"Prep", "Chop it.", "Fry it.", "Eat it."}; !reflect.DeepEqual(texts, want) {
		t.Errorf("steps = %q, want %q", texts, want)
	}
}

func TestHowToStepNonStringText(t *testing.T) {
	page := `<script type="application/ld+json">{"@type":"Recipe","name":"R","recipeIngredient":["x"],"recipeInstructions":[
{"@type":"HowToStep","text":{"@value":"Stir <b>well</b>."}},{"@type":"HowToStep","text":350}]}</script>`
	r := mustExtract(t, page, "").Recipe
	var got []string
	for _, s := range r.Instructions {
		got = append(got, s.Text)
	}
	if want := []string{"Stir well.", "350"}; !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

// Unclosed "<" must not make the scan quadratic: each "<" used to rescan to
// the end of the input.
func TestRenderMarkupIsLinearOnUnclosedTags(t *testing.T) {
	for name, in := range map[string]string{
		"a":        strings.Repeat("<a", 32<<10),
		"sup":      strings.Repeat("<sup", 16<<10),
		"spaced":   strings.Repeat("< ", 32<<10) + ">",
		"sup+gt":   strings.Repeat("<sup", 16<<10) + ">",
		"mixed":    strings.Repeat("x<b", 32<<10),
		"fraction": strings.Repeat("<sup>1</sup>/", 8<<10),
	} {
		done := make(chan struct{})
		go func() {
			renderMarkup(in)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("%s: renderMarkup of %d bytes took over 500ms", name, len(in))
		}
	}
}

func BenchmarkRenderMarkupUnclosedTags(b *testing.B) {
	in := strings.Repeat("<a", 32<<10)
	b.SetBytes(int64(len(in)))
	for i := 0; i < b.N; i++ {
		renderMarkup(in)
	}
}
