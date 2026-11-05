package extract

import (
	"encoding/json"
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

// setImages orders the candidates: the page's og:image, then those the
// recipe's own markup gave, then the page's primary ImageObject. It resolves
// each against the page URL and drops repeats, keeping the first (ADR-00039).
func (r *Raw) setImages(og, primary string, page *url.URL) {
	in := append([]string{og}, r.ImageURLs...)
	in = append(in, primary)
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

// pageIDs is indexByID over every JSON-LD block on the page, in document
// order with the first object for an @id winning. Yoast puts its graph in one
// script and a recipe plugin's Recipe in another, so the primary image is
// looked up across all of them. A block that does not parse is skipped here;
// extractJSONLD reports it.
func pageIDs(doc *html.Node) map[string]map[string]any {
	all := map[string]map[string]any{}
	walk(doc, func(n *html.Node) bool {
		if !isElem(n, "script") || !isJSONLDType(attr(n, "type")) {
			return true
		}
		var data any
		if json.Unmarshal([]byte(scriptText(n)), &data) == nil {
			for id, obj := range indexByID(data) {
				if _, dup := all[id]; !dup {
					all[id] = obj
				}
			}
		}
		return false
	})
	return all
}

// primaryImage returns the URL of the page's primary ImageObject within the
// page's JSON-LD: the object a node names in primaryImageOfPage, or else one
// whose @id ends "#primaryimage" (Yoast's convention), resolved by @id. "" when
// the page names none. Author headshots and logos are also ImageObjects, and
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
	// primaryImageOfPage refs first, then #primaryimage ids; each group in
	// sorted @id order so the result is deterministic.
	var want, bySuffix []string
	for _, id := range sortedIDs(ids) {
		if ref := refID(ids[id]["primaryImageOfPage"]); ref != "" {
			want = append(want, ref)
		}
		if strings.HasSuffix(id, "#primaryimage") {
			bySuffix = append(bySuffix, id)
		}
	}
	want = append(want, bySuffix...)
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
