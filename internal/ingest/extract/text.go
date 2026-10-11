package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// Elements whose content is never recipe text.
var textSkip = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"nav": true, "header": true, "footer": true, "aside": true, "form": true,
	"iframe": true, "button": true, "select": true, "head": true,
}

var textBlock = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true,
	"ul": true, "ol": true, "li": true, "br": true, "tr": true, "table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "dl": true, "dt": true, "dd": true, "figure": true,
	"figcaption": true, "pre": true, "hr": true, "fieldset": true,
}

// headingMark stands for the blank line before a heading until renderText
// has collapsed whitespace; NUL cannot occur in parsed text.
const headingMark = "\x00"

var textHeading = map[string]bool{"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true}

// mainText returns the readable text of the page for the text-sectioning
// path: the <main> or <article> element when there is one, else the <body>,
// minus navigation and other chrome. Each block element starts a new line and
// headings are set off by a blank line. If the chosen root holds no <h1> and
// the page has one, it leads the text so the title is not lost.
func mainText(doc *html.Node) string {
	root := firstElement(doc, "main")
	if root == nil {
		root = firstElement(doc, "article")
	}
	if root == nil {
		root = firstElement(doc, "body")
	}
	if root == nil {
		root = doc
	}
	text := renderText(root)
	if firstElement(root, "h1") == nil {
		if h1 := firstElement(doc, "h1"); h1 != nil {
			if t := textOf(h1); t != "" {
				text = strings.TrimSpace(t + "\n\n" + text)
			}
		}
	}
	return text
}

func firstElement(n *html.Node, tag string) *html.Node {
	var found *html.Node
	walk(n, func(c *html.Node) bool {
		if found != nil {
			return false
		}
		if c.Data == tag {
			found = c
			return false
		}
		return true
	})
	return found
}

func renderText(root *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			if textSkip[n.Data] && n != root {
				return
			}
			if textHeading[n.Data] {
				b.WriteString("\n" + headingMark + "\n")
			} else if textBlock[n.Data] {
				b.WriteByte('\n')
			}
			defer func() {
				if textBlock[n.Data] {
					b.WriteByte('\n')
				}
			}()
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(root)

	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		l = collapse(l)
		switch l {
		case "":
		case headingMark:
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
		default:
			lines = append(lines, l)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
