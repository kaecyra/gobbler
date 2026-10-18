package extract

import (
	"strings"
	"testing"
)

// The saved page's Recipe names its author only by reference to a Person node
// in the same @graph, and its video only as an iframe in the post body.
func TestRecipeTinEatsFixtureReadsAuthorAndEmbeddedVideo(t *testing.T) {
	res := mustExtract(t, fixture(t, "real_recipetineats_graph"), fixtureURL)
	if res.Method != MethodJSONLD || res.Recipe == nil {
		t.Fatalf("method = %q, recipe = %v, want a JSON-LD recipe", res.Method, res.Recipe)
	}
	if got := res.Recipe.Author; got != "Yumiko" {
		t.Errorf("author = %q, want Yumiko", got)
	}
	if got := res.Recipe.VideoID; got != "L57S_S22Mf8" {
		t.Errorf("video id = %q, want L57S_S22Mf8", got)
	}
}

func recipePage(recipeExtra, body string) string {
	return `<html><head><script type="application/ld+json">{"@context":"https://schema.org","@graph":[` +
		`{"@type":"Person","@id":"https://x.test/#p1","name":"Ann Cook"},` +
		`{"@type":"Recipe","name":"Soup",` + recipeExtra + `"recipeIngredient":["1 cup water"]}]}</script></head><body>` +
		body + `</body></html>`
}

func TestJSONLDResolvesReferences(t *testing.T) {
	for name, tc := range map[string]struct{ extra, want string }{
		"single reference": {`"author":{"@id":"https://x.test/#p1"},`, "Ann Cook"},
		"list reference":   {`"author":[{"@id":"https://x.test/#p1"}],`, "Ann Cook"},
		"inline wins":      {`"author":{"@id":"https://x.test/#p1","name":"Inline"},`, "Inline"},
		"dangling":         {`"author":{"@id":"https://x.test/#missing"},`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			res := mustExtract(t, recipePage(tc.extra, ""), fixtureURL)
			if got := res.Recipe.Author; got != tc.want {
				t.Errorf("author = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestJSONLDResolvesAVideoReference(t *testing.T) {
	page := strings.Replace(recipePage(`"video":{"@id":"https://x.test/#v"},`, ""), `{"@type":"Person"`,
		`{"@type":"VideoObject","@id":"https://x.test/#v","embedUrl":"https://www.youtube.com/embed/dQw4w9WgXcQ"},{"@type":"Person"`, 1)
	if got := mustExtract(t, page, fixtureURL).Recipe.VideoID; got != "dQw4w9WgXcQ" {
		t.Errorf("video id = %q, want dQw4w9WgXcQ", got)
	}
}

// ADR-00023: markup first, then the first YouTube embed in document order.
func TestEmbeddedVideo(t *testing.T) {
	const a, b = "aaaaaaaaaaa", "bbbbbbbbbbb"
	for name, tc := range map[string]struct{ extra, body, want string }{
		"first embed":      {``, `<iframe src="https://www.youtube.com/embed/` + a + `"></iframe><iframe src="https://www.youtube.com/embed/` + b + `"></iframe>`, a},
		"nocookie":         {``, `<iframe src="https://www.youtube-nocookie.com/embed/` + a + `?rel=0"></iframe>`, a},
		"lazy data-src":    {``, `<iframe data-src="https://www.youtube.com/embed/` + a + `"></iframe>`, a},
		"other host first": {``, `<iframe src="https://player.vimeo.com/embed/1"></iframe><iframe src="https://www.youtube.com/embed/` + b + `"></iframe>`, b},
		"not an embed":     {``, `<iframe src="https://www.youtube.com/watch?v=` + a + `"></iframe>`, ""},
		"look-alike host":  {``, `<iframe src="https://evil.test/youtube.com/embed/` + a + `"></iframe>`, ""},
		"no iframe":        {``, `<a href="https://www.youtube.com/embed/` + a + `">x</a>`, ""},
		"markup wins":      {`"video":{"embedUrl":"https://www.youtube.com/embed/` + b + `"},`, `<iframe src="https://www.youtube.com/embed/` + a + `"></iframe>`, b},
	} {
		t.Run(name, func(t *testing.T) {
			if got := mustExtract(t, recipePage(tc.extra, tc.body), fixtureURL).Recipe.VideoID; got != tc.want {
				t.Errorf("video id = %q, want %q", got, tc.want)
			}
		})
	}
}
