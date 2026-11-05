package extract

import (
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// ogImage returns the page's og:image URL, or "". Only the first og:image
// counts; og:image:width and the like are different properties.
func ogImage(doc *html.Node) string {
	found := ""
	walk(doc, func(n *html.Node) bool {
		if found != "" {
			return false
		}
		if isElem(n, "meta") && strings.EqualFold(strings.TrimSpace(attr(n, "property")), "og:image") {
			found = collapse(attr(n, "content"))
		}
		return true
	})
	return found
}

// setImages puts the page's og:image ahead of the candidates the recipe's own
// markup gave, resolves each against the page URL and drops repeats, keeping
// the first (ADR-00039). The result is ordered best guess first.
func (r *Raw) setImages(og string, page *url.URL) {
	in := append([]string{og}, r.ImageURLs...)
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if page != nil {
			if ref, err := url.Parse(s); err == nil {
				s = page.ResolveReference(ref).String()
			}
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	r.ImageURLs = out
}

// jsonImages reads a schema.org "image" value as URLs: a string, an
// ImageObject (its url, else contentUrl, never its name or caption), or a
// list of either. A bare {"@id": ...} reference was already resolved by
// resolveRefs when it names an object in the same block.
func jsonImages(v any) []string {
	switch t := v.(type) {
	case string:
		if s := collapse(t); s != "" {
			return []string{s}
		}
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, jsonImages(e)...)
		}
		return out
	case map[string]any:
		for _, k := range []string{"url", "contentUrl"} {
			if s, ok := t[k].(string); ok && collapse(s) != "" {
				return []string{collapse(s)}
			}
		}
	}
	return nil
}

// primaryImage returns the URL of the page's primary ImageObject within a
// JSON-LD block: the object a node names in primaryImageOfPage, or else one
// whose @id ends "#primaryimage" (Yoast's convention), resolved by @id. "" when
// the block names none. Author headshots and logos are also ImageObjects, and
// are never taken for it.
func primaryImage(ids map[string]map[string]any) string {
	refID := func(v any) string {
		switch t := v.(type) {
		case string:
			return t
		case map[string]any:
			id, _ := t["@id"].(string)
			return id
		}
		return ""
	}
	var want []string
	for _, id := range sortedIDs(ids) {
		if strings.HasSuffix(id, "#primaryimage") {
			want = append(want, id)
		}
		if ref := refID(ids[id]["primaryImageOfPage"]); ref != "" {
			want = append(want, ref)
		}
	}
	for _, id := range want {
		if obj, ok := ids[id]; ok && hasType(obj["@type"], "ImageObject") {
			if u := jsonImages(obj); len(u) > 0 {
				return u[0]
			}
		}
	}
	return ""
}

func sortedIDs(ids map[string]map[string]any) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
