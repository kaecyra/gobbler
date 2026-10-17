package extract

import (
	"strings"

	"golang.org/x/net/html"
)

// extractMicrodata returns the first usable schema.org Recipe item in the page.
func extractMicrodata(doc *html.Node) (Raw, bool) {
	var found Raw
	ok := false
	walk(doc, func(n *html.Node) bool {
		if ok {
			return false
		}
		if hasAttr(n, "itemscope") && itemIsType(n, "Recipe") {
			if r := recipeFromMicrodata(n); r.usable() {
				found, ok = r, true
				return false
			}
		}
		return true
	})
	return found, ok
}

func itemIsType(n *html.Node, want string) bool {
	for _, t := range strings.Fields(attr(n, "itemtype")) {
		if t == want || strings.HasSuffix(t, "/"+want) {
			return true
		}
	}
	return false
}

// itemProp is one property node of an item.
type itemProp struct {
	name string
	node *html.Node
}

// itemProps lists the properties that belong to item, in document order. A
// nested item is never descended into: if it has an itemprop it is listed
// as one property, otherwise it is skipped; either way its own properties
// belong to it.
func itemProps(item *html.Node) []itemProp {
	var out []itemProp
	var rec func(*html.Node)
	rec = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if p := attr(c, "itemprop"); p != "" {
				for _, name := range strings.Fields(p) {
					out = append(out, itemProp{name, c})
				}
			}
			// A nested item owns its properties, with or without itemprop.
			if hasAttr(c, "itemscope") {
				continue
			}
			rec(c)
		}
	}
	rec(item)
	return out
}

func recipeFromMicrodata(item *html.Node) Raw {
	var r Raw
	var authors []string
	for _, p := range itemProps(item) {
		v := propValue(p.node)
		switch p.name {
		case "name":
			if r.Name == "" {
				r.Name = v
			}
		case "author":
			if v != "" {
				authors = append(authors, v)
			}
		case "description":
			if r.Description == "" {
				r.Description = v
			}
		case "recipeYield":
			if v != "" {
				r.Yield = append(r.Yield, v)
			}
		case "prepTime":
			r.PrepTime = firstNonEmpty(r.PrepTime, v)
		case "cookTime":
			r.CookTime = firstNonEmpty(r.CookTime, v)
		case "totalTime":
			r.TotalTime = firstNonEmpty(r.TotalTime, v)
		case "image":
			r.ImageURL = firstNonEmpty(r.ImageURL, v)
		case "recipeIngredient", "ingredients":
			if v != "" {
				r.Ingredients = append(r.Ingredients, v)
			}
		case "recipeCategory":
			if v != "" {
				r.Categories = append(r.Categories, v)
			}
		case "tool":
			if v != "" {
				r.Tools = append(r.Tools, v)
			}
		case "nutrition":
			r.Calories = firstNonEmpty(r.Calories, nestedProp(p.node, "calories"))
		case "recipeInstructions":
			r.Instructions = append(r.Instructions, microdataSteps(p.node)...)
		}
	}
	r.Author = strings.Join(authors, ", ")
	return r
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// propValue reads a property per the microdata value rules; an item property
// reads as its own "name", or its text.
func propValue(n *html.Node) string {
	if hasAttr(n, "itemscope") {
		return firstNonEmpty(nestedProp(n, "name"), textOf(n))
	}
	switch n.Data {
	case "meta":
		return collapse(attr(n, "content"))
	case "a", "area", "link":
		return attr(n, "href")
	case "img", "audio", "video", "source", "embed", "iframe":
		return attr(n, "src")
	case "time":
		if hasAttr(n, "datetime") {
			return collapse(attr(n, "datetime"))
		}
	}
	if hasAttr(n, "content") {
		return collapse(attr(n, "content"))
	}
	return textOf(n)
}

func nestedProp(item *html.Node, name string) string {
	for _, p := range itemProps(item) {
		if p.name == name {
			return propValue(p.node)
		}
	}
	return ""
}

// microdataSteps reads one recipeInstructions property: a HowToStep item, a
// container of list items, or plain text.
func microdataSteps(n *html.Node) []InstructionStep {
	if hasAttr(n, "itemscope") {
		if t := firstNonEmpty(nestedProp(n, "text"), propValue(n)); t != "" {
			return []InstructionStep{{Text: t, Title: stepTitle(nestedProp(n, "name"), t)}}
		}
		return nil
	}
	var out []InstructionStep
	walk(n, func(c *html.Node) bool {
		if isElem(c, "li") {
			if t := textOf(c); t != "" {
				out = append(out, InstructionStep{Text: t})
			}
			return false
		}
		return true
	})
	if len(out) == 0 {
		if t := textOf(n); t != "" {
			out = append(out, InstructionStep{Text: t})
		}
	}
	return out
}
