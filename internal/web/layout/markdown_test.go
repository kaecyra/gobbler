package layout

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
	"golang.org/x/net/html"
)

// allowedTags are the elements ADR-00046 lets step and note text become.
var allowedTags = map[string]bool{
	"p": true, "br": true, "ul": true, "ol": true, "li": true, "blockquote": true,
	"em": true, "strong": true, "code": true, "a": true,
}

// elements tokenizes rendered output and returns every start tag with its
// attributes, failing on any element outside the subset or any attribute an
// allowed element does not carry.
func elements(t *testing.T, out string) []html.Token {
	t.Helper()
	var tags []html.Token
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if !errors.Is(z.Err(), io.EOF) {
				t.Fatalf("tokenize %q: %v", out, z.Err())
			}
			return tags
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			if !allowedTags[tok.Data] {
				t.Errorf("element <%s> in %q", tok.Data, out)
			}
			for _, a := range tok.Attr {
				ok := (tok.Data == "a" && (a.Key == "href" || a.Key == "rel" || a.Key == "title")) ||
					(tok.Data == "ol" && a.Key == "start")
				if !ok {
					t.Errorf("attribute %s on <%s> in %q", a.Key, tok.Data, out)
				}
			}
			tags = append(tags, tok)
		}
	}
}

func render(t *testing.T, src string) string {
	t.Helper()
	out, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown(%q): %v", src, err)
	}
	return out
}

// The allowed subset renders as markup (ADR-00046): paragraphs, hard line
// breaks, both kinds of list, blockquotes, emphasis, strong, inline code and
// http(s) links carrying their rel.
func TestMarkdownRendersTheAllowedSubset(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"plain text is one paragraph", "Sear the ribs.", "<p>Sear the ribs.</p>\n"},
		{"empty text renders nothing", "", ""},
		{"a single newline breaks the line", "Sear.\nThen rest.", "<p>Sear.<br>\nThen rest.</p>\n"},
		{"a blank line starts a paragraph", "Sear.\n\nRest.", "<p>Sear.</p>\n<p>Rest.</p>\n"},
		{"emphasis and strong", "Fry **hot** and *fast*.", "<p>Fry <strong>hot</strong> and <em>fast</em>.</p>\n"},
		{"inline code is escaped", "Set `<200°C>`.", "<p>Set <code>&lt;200°C&gt;</code>.</p>\n"},
		{"bulleted list", "- salt\n- pepper", "<ul>\n<li>salt</li>\n<li>pepper</li>\n</ul>\n"},
		{"numbered list keeps its start", "3. fold\n4. rest", "<ol start=\"3\">\n<li>fold</li>\n<li>rest</li>\n</ol>\n"},
		{"blockquote", "> Hot oil spits.", "<blockquote>\n<p>Hot oil spits.</p>\n</blockquote>\n"},
		{"https link", "See [the video](https://example.com/v).",
			`<p>See <a href="https://example.com/v" rel="nofollow noopener noreferrer">the video</a>.</p>` + "\n"},
		{"http link with a title", `[x](http://example.com/a?b=1&c=2 "Tip")`,
			`<p><a href="http://example.com/a?b=1&amp;c=2" rel="nofollow noopener noreferrer" title="Tip">x</a></p>` + "\n"},
		{"a title's escapes and entities are resolved once", `[a](https://x.test "a \"q\" &amp; b")`,
			`<p><a href="https://x.test" rel="nofollow noopener noreferrer" title="a &quot;q&quot; &amp; b">a</a></p>` + "\n"},
		{"http autolink", "<https://example.com/x>",
			`<p><a href="https://example.com/x" rel="nofollow noopener noreferrer">https://example.com/x</a></p>` + "\n"},
		{"angle brackets and ampersands stay text", "Keeps for <3 days> & freezes.", "<p>Keeps for &lt;3 days&gt; &amp; freezes.</p>\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.src)
			if got != tc.want {
				t.Errorf("RenderMarkdown(%q)\n got %q\nwant %q", tc.src, got, tc.want)
			}
			elements(t, got)
		})
	}
}

// ADR-00046 obligation 2: what is outside the subset never becomes an
// element. Each case renders as its own source, escaped, inside a paragraph.
func TestMarkdownRendersEverythingElseAsSourceText(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		// text is what the reader sees: the output with tags stripped and
		// entities decoded. It is the source, line breaks aside.
		text string
	}{
		{"script tag", "<script>alert(1)</script>", "<script>alert(1)</script>"},
		{"img tag", `a <img src=x onerror="alert(1)"> b`, `a <img src=x onerror="alert(1)"> b`},
		{"html block", "<div onclick=\"x()\">\nhi\n</div>", "<div onclick=\"x()\">hi</div>"},
		{"javascript link", "[go](javascript:alert(1)) now", "[go](javascript:alert(1)) now"},
		{"javascript link in capitals", "[go](JavaScript:alert(1))", "[go](JavaScript:alert(1))"},
		{"data link", "[go](data:text/html,<b>1</b>)", "[go](data:text/html,<b>1</b>)"},
		{"relative link", "[go](/recipes/1)", "[go](/recipes/1)"},
		{"scheme-relative link", "[go](//example.com)", "[go](//example.com)"},
		{"javascript autolink", "<javascript:alert(1)>", "<javascript:alert(1)>"},
		{"email autolink", "<cook@example.com>", "<cook@example.com>"},
		{"atx heading", "# Heading", "# Heading"},
		{"setext heading", "Heading\n===", "Heading===" /* the line break is a <br> */},
		{"image", "![a pan](https://example.com/pan.png) hot", "![a pan](https://example.com/pan.png) hot"},
		{"image with a javascript source", "![x](javascript:alert(1))", "![x](javascript:alert(1))"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |", "| a | b ||---|---|| 1 | 2 |"},
		{"fenced code block", "```\nrm -rf\n```", "```rm -rf```"},
		{"indented code block", "    indented", "indented"},
		{"horizontal rule", "---", "---"},
		{"horizontal rule of stars", "* * *", "* * *"},
		{"horizontal rule of dashes", "- - -", "- - -"},
		{"link reference definition", "[go][r]\n\n[r]: https://example.com", "[go][r][r]: https://example.com"},
		{"escaped entity", "&lt;script&gt;", "<script>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.src)
			for _, tok := range elements(t, got) {
				if tok.Data != "p" && tok.Data != "br" {
					t.Errorf("RenderMarkdown(%q) made <%s>: %q", tc.src, tok.Data, got)
				}
			}
			if txt := visibleText(t, got); txt != tc.text {
				t.Errorf("RenderMarkdown(%q) reads %q, want %q", tc.src, txt, tc.text)
			}
		})
	}
}

// visibleText is the output's text, as a browser would show it, with the
// newlines after line breaks dropped.
func visibleText(t *testing.T, out string) string {
	t.Helper()
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.ReplaceAll(b.String(), "\n", "")
		case html.TextToken:
			b.Write(z.Text())
		}
	}
}

// Every link that renders carries the rel and an http(s) destination, however
// the source nests it.
func TestMarkdownLinksAreWebOnlyWithRel(t *testing.T) {
	for _, src := range []string{
		"[a [b](javascript:x)](https://example.com)",
		"> - [x](https://example.com) and [y](vbscript:z)",
		"**[x](http://example.com)**",
	} {
		for _, tok := range elements(t, render(t, src)) {
			if tok.Data != "a" {
				continue
			}
			attrs := map[string]string{}
			for _, a := range tok.Attr {
				attrs[a.Key] = a.Val
			}
			if !strings.HasPrefix(attrs["href"], "http://") && !strings.HasPrefix(attrs["href"], "https://") {
				t.Errorf("RenderMarkdown(%q) links to %q", src, attrs["href"])
			}
			if attrs["rel"] != linkRel {
				t.Errorf("RenderMarkdown(%q) link rel = %q, want %q", src, attrs["rel"], linkRel)
			}
		}
	}
}

// The renderer refuses a node kind outside the subset instead of writing it,
// so a parser change that let one through fails the page loudly.
func TestMarkdownRefusesANodeOutsideTheSubset(t *testing.T) {
	for _, n := range []ast.Node{ast.NewHeading(1), ast.NewThematicBreak(), ast.NewCodeBlock(), ast.NewRawHTML(), ast.NewImage(ast.NewLink())} {
		doc := ast.NewDocument()
		para := ast.NewParagraph()
		doc.AppendChild(doc, para)
		para.AppendChild(para, n)
		var b bytes.Buffer
		if err := mdRenderer.Render(&b, nil, doc); err == nil {
			t.Errorf("rendered %s as %q, want an error", n.Kind(), b.String())
		}
	}
	link := ast.NewLink()
	link.Destination = []byte("javascript:alert(1)")
	doc := ast.NewDocument()
	doc.AppendChild(doc, link)
	var b bytes.Buffer
	if err := mdRenderer.Render(&b, nil, doc); err == nil {
		t.Errorf("rendered a javascript: link as %q, want an error", b.String())
	}
}

func TestHorizontalRuleMatchesCommonMark(t *testing.T) {
	for line, want := range map[string]bool{
		"---": true, "  - - -": true, "___": true, "*\t*\t*": true, "-----  ": true,
		"    ---": false, "--": false, "-*-": false, "--- x": false, "": false,
	} {
		if got := horizontalRule([]byte(line)); got != want {
			t.Errorf("horizontalRule(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestMarkdownComponentWritesTheRenderedHTML(t *testing.T) {
	var b strings.Builder
	if err := Markdown("**hot**").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if b.String() != "<p><strong>hot</strong></p>\n" {
		t.Errorf("Markdown wrote %q", b.String())
	}
}

func TestMarkdownTextIsWhatTheReaderSees(t *testing.T) {
	for src, want := range map[string]string{
		"Mix **well**.":                      "Mix well.",
		"Use `00` flour & *cold* water <ok>": "Use 00 flour & cold water <ok>",
		"- one\n- two":                       "one two",
		"line one\nline two":                 "line one line two",
		"> rest it\n\nthen bake":             "rest it then bake",
		"see [the video](https://x.test/v)":  "see the video",
		"# not a heading":                    "# not a heading",
		"":                                   "",
	} {
		got, err := MarkdownText(src)
		if err != nil {
			t.Fatalf("MarkdownText(%q): %v", src, err)
		}
		if got != want {
			t.Errorf("MarkdownText(%q) = %q, want %q", src, got, want)
		}
	}
}
