package extract

import (
	"reflect"
	"testing"
)

func imagesOf(t *testing.T, page, src string) []string {
	t.Helper()
	res := mustExtract(t, page, src)
	if res.Recipe == nil {
		t.Fatalf("no recipe: method %s", res.Method)
	}
	return res.Recipe.ImageURLs
}

const imgPageStart = `<html><head>
<meta property="og:image" content="/img/og.jpg">
<script type="application/ld+json">`

const imgPageEnd = `</script></head><body></body></html>`

func TestImageCandidatesOrder(t *testing.T) {
	page := imgPageStart + `{"@context":"https://schema.org","@graph":[
	 {"@type":"WebPage","@id":"https://www.example.com/r/#webpage","primaryImageOfPage":{"@id":"https://www.example.com/r/#primaryimage"}},
	 {"@type":"ImageObject","@id":"https://www.example.com/r/#primaryimage","url":"https://www.example.com/img/primary.jpg","caption":"Soup"},
	 {"@type":"Recipe","name":"Soup","recipeIngredient":["1 egg"],
	  "image":["https://www.example.com/img/a.jpg","/img/b.jpg","https://www.example.com/img/og.jpg",{"@type":"ImageObject","url":"https://www.example.com/img/c.jpg","name":"a caption"}]}
	]}` + imgPageEnd
	got := imagesOf(t, page, "https://www.example.com/r/")
	want := []string{
		"https://www.example.com/img/og.jpg", // og:image, relative, resolved
		"https://www.example.com/img/a.jpg",
		"https://www.example.com/img/b.jpg",
		"https://www.example.com/img/c.jpg", // the ImageObject's url, not its name
		"https://www.example.com/img/primary.jpg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}

func TestImageCandidatesSkipAuthorAndLogo(t *testing.T) {
	page := imgPageStart + `{"@context":"https://schema.org","@graph":[
	 {"@type":"Organization","@id":"https://www.example.com/#org","logo":{"@type":"ImageObject","@id":"https://www.example.com/#logo","url":"https://www.example.com/logo.png"},
	  "image":{"@id":"https://www.example.com/#logo"}},
	 {"@type":"Person","@id":"https://www.example.com/#me","name":"Pat",
	  "image":{"@type":"ImageObject","@id":"https://www.example.com/me.jpg","url":"https://www.example.com/me.jpg"}},
	 {"@type":"Article","image":{"@id":"https://www.example.com/#logo"}},
	 {"@type":"Recipe","name":"Soup","author":{"@id":"https://www.example.com/#me"},"recipeIngredient":["1 egg"],
	  "image":"https://www.example.com/img/a.jpg"}
	]}` + imgPageEnd
	got := imagesOf(t, page, "https://www.example.com/r/")
	want := []string{"https://www.example.com/img/og.jpg", "https://www.example.com/img/a.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}

func TestImageCandidatesMicrodata(t *testing.T) {
	page := `<html><head><meta property="og:image" content="https://www.example.com/og.jpg"></head><body>
	<div itemscope itemtype="https://schema.org/Recipe"><h1 itemprop="name">Soup</h1>
	<img itemprop="image" src="/one.jpg"><meta itemprop="image" content="https://www.example.com/og.jpg">
	<span itemprop="image" itemscope itemtype="https://schema.org/ImageObject"><meta itemprop="url" content="https://www.example.com/two.jpg"><span itemprop="name">cap</span></span>
	<li itemprop="recipeIngredient">1 egg</li></div></body></html>`
	got := imagesOf(t, page, "https://www.example.com/r/")
	want := []string{"https://www.example.com/og.jpg", "https://www.example.com/one.jpg", "https://www.example.com/two.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}

const yoastWebPage = `<script type="application/ld+json">{"@context":"https://schema.org","@graph":[
 {"@type":"WebPage","@id":"https://www.example.com/r/#webpage","primaryImageOfPage":{"@id":"https://www.example.com/r/#primaryimage"}},
 {"@type":"ImageObject","@id":"https://www.example.com/r/#primaryimage","url":"https://www.example.com/img/primary.jpg"},
 {"@type":"Person","@id":"https://www.example.com/#me","image":{"@type":"ImageObject","@id":"https://www.example.com/me.jpg","url":"https://www.example.com/me.jpg"}}
]}</script>`

func TestImageCandidatesPrimaryFromAnotherBlock(t *testing.T) {
	page := `<html><head><meta property="og:image" content="https://www.example.com/og.jpg">` +
		`<script type="application/ld+json">{"@context":"https://schema.org","@type":"Recipe","name":"Soup","recipeIngredient":["1 egg"],"image":"https://www.example.com/a.jpg"}</script>` +
		yoastWebPage + `</head></html>`
	got := imagesOf(t, page, "https://www.example.com/r/")
	want := []string{"https://www.example.com/og.jpg", "https://www.example.com/a.jpg", "https://www.example.com/img/primary.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}

func TestImageCandidatesPrimaryForMicrodata(t *testing.T) {
	page := `<html><head>` + yoastWebPage + `</head><body>
	<div itemscope itemtype="https://schema.org/Recipe"><h1 itemprop="name">Soup</h1>
	<img itemprop="image" src="/one.jpg"><li itemprop="recipeIngredient">1 egg</li></div></body></html>`
	res := mustExtract(t, page, "https://www.example.com/r/")
	if res.Method != MethodMicrodata {
		t.Fatalf("method %s, want microdata", res.Method)
	}
	want := []string{"https://www.example.com/one.jpg", "https://www.example.com/img/primary.jpg"}
	if got := res.Recipe.ImageURLs; !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}

func TestPrimaryImageRefBeatsSuffix(t *testing.T) {
	// "a" sorts before "z", so an interleaved loop would pick the suffix match.
	page := `<html><head><script type="application/ld+json">{"@graph":[
	 {"@type":"ImageObject","@id":"https://www.example.com/a#primaryimage","url":"https://www.example.com/suffix.jpg"},
	 {"@type":"ImageObject","@id":"https://www.example.com/z-named","url":"https://www.example.com/named.jpg"},
	 {"@type":"WebPage","@id":"https://www.example.com/page","primaryImageOfPage":{"@id":"https://www.example.com/z-named"}},
	 {"@type":"Recipe","name":"Soup","recipeIngredient":["1 egg"]}]}</script></head></html>`
	want := []string{"https://www.example.com/named.jpg"}
	if got := imagesOf(t, page, "https://www.example.com/r/"); !reflect.DeepEqual(got, want) {
		t.Errorf("candidates\n got %q\nwant %q", got, want)
	}
}
