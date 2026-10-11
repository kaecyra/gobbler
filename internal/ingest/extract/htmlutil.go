package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// attr returns the value of attribute key on n, or "".
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func isElem(n *html.Node, tag string) bool {
	return n.Type == html.ElementNode && n.Data == tag
}

// walk calls f for n and every descendant element in document order. When f
// returns false the subtree below that node is skipped.
func walk(n *html.Node, f func(*html.Node) bool) {
	if n.Type == html.ElementNode && !f(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, f)
	}
}

// textOf returns the whitespace-collapsed text content of n.
func textOf(n *html.Node) string {
	var b strings.Builder
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(n.Data)
		case n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style"):
			return
		case isElem(n, "br"):
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rec(c)
		}
	}
	rec(n)
	return collapse(b.String())
}

// collapse trims s and folds every run of whitespace (including non-breaking
// spaces) to one space.
func collapse(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || strings.ContainsRune(" \t\n\r\f\v", r)
	}), " ")
}
