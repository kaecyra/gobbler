package extract

import (
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"

	"github.com/kaecyra/gobbler/internal/video"
)

// extractJSONLD returns the first usable schema.org Recipe found in the page's
// JSON-LD blocks, in document order. A block that does not parse is skipped
// and reported, so it cannot hide a valid block elsewhere.
func extractJSONLD(doc *nethtml.Node) (Raw, []string, bool) {
	var issues []string
	var found Raw
	ok := false
	walk(doc, func(n *nethtml.Node) bool {
		if ok {
			return false
		}
		if !isElem(n, "script") || !isJSONLDType(attr(n, "type")) {
			return true
		}
		var data any
		if err := json.Unmarshal([]byte(scriptText(n)), &data); err != nil {
			issues = append(issues, IssueMalformedJSONLD)
			return false
		}
		found, ok = findRecipe(data)
		return false
	})
	return found, issues, ok
}

func isJSONLDType(t string) bool {
	t, _, _ = strings.Cut(t, ";")
	return strings.EqualFold(strings.TrimSpace(t), "application/ld+json")
}

func scriptText(n *nethtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	return strings.TrimSpace(b.String())
}

// findRecipe searches a decoded JSON-LD value (object, array or @graph) for
// the first usable Recipe.
func findRecipe(v any) (Raw, bool) {
	return findRecipeDepth(v, 0, indexByID(v))
}

// jsonldMaxDepth bounds how far below the top of a block a Recipe is looked
// for: @graph members and mainEntity sit within a couple of levels.
const jsonldMaxDepth = 4

// findRecipeDepth looks at v, then at @graph, then at every other object or
// array valued property in sorted key order (map order is random and the
// result must be deterministic), so a Recipe under mainEntity is found.
func findRecipeDepth(v any, depth int, ids map[string]map[string]any) (Raw, bool) {
	if depth > jsonldMaxDepth {
		return Raw{}, false
	}
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if r, ok := findRecipeDepth(e, depth+1, ids); ok {
				return r, true
			}
		}
	case map[string]any:
		if hasType(t["@type"], "Recipe") {
			if r := recipeFromJSONLD(t, ids); r.usable() {
				return r, true
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			if k != "@graph" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if _, ok := t["@graph"]; ok {
			keys = append([]string{"@graph"}, keys...)
		}
		for _, k := range keys {
			if r, ok := findRecipeDepth(t[k], depth+1, ids); ok {
				return r, true
			}
		}
	}
	return Raw{}, false
}

// indexByID collects every object in a JSON-LD block that carries an "@id" and
// at least one other property, so a reference elsewhere in the block
// ({"@id": ...} alone, as Yoast writes Recipe.author) can be resolved. The
// first object with an ID wins, so the result is deterministic.
func indexByID(v any) map[string]map[string]any {
	ids := map[string]map[string]any{}
	var rec func(v any)
	rec = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				rec(e)
			}
		case map[string]any:
			if id, ok := t["@id"].(string); ok && len(t) > 1 {
				if _, dup := ids[id]; !dup {
					ids[id] = t
				}
			}
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				rec(t[k])
			}
		}
	}
	rec(v)
	return ids
}

// resolveRefs returns a copy of the Recipe m in which every property value that
// is a bare reference ({"@id": ...}), alone or inside a list, is replaced by
// the object it names. One level only: a reference inside the resolved object
// stays as written, which also makes a reference cycle harmless.
func resolveRefs(m map[string]any, ids map[string]map[string]any) map[string]any {
	deref := func(v any) any {
		ref, ok := v.(map[string]any)
		if !ok || len(ref) != 1 {
			return v
		}
		id, ok := ref["@id"].(string)
		if !ok {
			return v
		}
		if target, found := ids[id]; found {
			return target
		}
		return v
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if arr, ok := v.([]any); ok {
			res := make([]any, len(arr))
			for i, e := range arr {
				res[i] = deref(e)
			}
			out[k] = res
			continue
		}
		out[k] = deref(v)
	}
	return out
}

func hasType(v any, want string) bool {
	match := func(s string) bool {
		return s == want || strings.HasSuffix(s, "/"+want)
	}
	switch t := v.(type) {
	case string:
		return match(t)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && match(s) {
				return true
			}
		}
	}
	return false
}

func recipeFromJSONLD(m map[string]any, ids map[string]map[string]any) Raw {
	m = resolveRefs(m, ids)
	r := Raw{
		Name:        jsonString(m["name"]),
		Author:      strings.Join(jsonStrings(m["author"]), ", "),
		Description: jsonString(m["description"]),
		Yield:       jsonStrings(m["recipeYield"]),
		PrepTime:    jsonString(m["prepTime"]),
		CookTime:    jsonString(m["cookTime"]),
		TotalTime:   jsonString(m["totalTime"]),
		ImageURL:    firstOf(jsonStrings(m["image"])),
	}
	r.Ingredients = jsonStrings(m["recipeIngredient"])
	if len(r.Ingredients) == 0 {
		r.Ingredients = jsonStrings(m["ingredients"])
	}
	r.Tools = jsonStrings(m["tool"])
	r.Categories = jsonStrings(m["recipeCategory"])
	r.VideoID = youTubeID(m["video"])
	if n, ok := m["nutrition"].(map[string]any); ok {
		r.Calories = jsonString(n["calories"])
	}
	r.Instructions = jsonInstructions(m["recipeInstructions"], "")
	return r
}

// youTubeID reads the schema.org "video" property (a VideoObject, a link, or a
// list of either) into the ID of the first YouTube video it names, or "".
// Within a VideoObject the order is embedUrl, contentUrl, url.
// Only the address properties of a VideoObject count, never its name or
// thumbnail, and the link is read by video.ParseID, the one parser of YouTube
// links; a video on any other host is ignored (ADR-00021 decision 5). A bare
// string is read only as a link: video.ParseID would take any 11 characters of
// letters, digits, "-" and "_" as a bare ID, so free text such as "Dinner-Idea"
// would become a video that does not exist.
func youTubeID(v any) string {
	switch t := v.(type) {
	case string:
		if id, err := video.ParseID(t); err == nil && !video.IsID(strings.TrimSpace(t)) {
			return id
		}
	case []any:
		for _, e := range t {
			if id := youTubeID(e); id != "" {
				return id
			}
		}
	case map[string]any:
		for _, k := range []string{"embedUrl", "contentUrl", "url"} {
			if id := youTubeID(t[k]); id != "" {
				return id
			}
		}
	}
	return ""
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// cleanText turns a string that may carry HTML into one line of text; see
// renderMarkup for the joining rules.
func cleanText(s string) string {
	return collapse(renderMarkup(s))
}

// rawText reads a scalar or a {"@value": ...} wrapper as text without
// cleaning it, so markup can still be split into steps.
func rawText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		return rawText(t["@value"])
	}
	return ""
}

// jsonString reads a scalar, or the "name"/"url"/"text"/"@value" of an object.
func jsonString(v any) string {
	switch t := v.(type) {
	case string:
		return cleanText(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		for _, k := range []string{"name", "text", "@value", "url", "contentUrl"} {
			if s := jsonString(t[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

// jsonStrings reads a scalar or an array of scalars/objects as a list,
// dropping empty entries.
func jsonStrings(v any) []string {
	var out []string
	if arr, ok := v.([]any); ok {
		for _, e := range arr {
			if s := jsonString(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if s := jsonString(v); s != "" {
		out = append(out, s)
	}
	return out
}

// stepsFromText splits one instruction string into steps: at newlines and at
// block-level markup, which some sites embed as a single HTML string,
// sometimes entity-escaped. A string with no such structure stays one step.
func stepsFromText(s, section string) []InstructionStep {
	if markupEscapedBlockRE.MatchString(s) {
		s = html.UnescapeString(s)
	}
	var out []InstructionStep
	for _, line := range strings.Split(renderMarkup(s), "\n") {
		if t := collapse(line); t != "" {
			out = append(out, InstructionStep{Section: section, Text: t})
		}
	}
	return out
}

// jsonInstructions flattens recipeInstructions: a plain string (one step per
// line), an array of strings, HowToStep objects, or HowToSection objects whose
// steps carry the section name.
func jsonInstructions(v any, section string) []InstructionStep {
	var out []InstructionStep
	switch t := v.(type) {
	case string:
		out = append(out, stepsFromText(t, section)...)
	case []any:
		for _, e := range t {
			out = append(out, jsonInstructions(e, section)...)
		}
	case map[string]any:
		if hasType(t["@type"], "HowToSection") {
			name := jsonString(t["name"])
			if name == "" {
				name = section
			}
			return jsonInstructions(t["itemListElement"], name)
		}
		raw := rawText(t["text"])
		if cleanText(raw) == "" {
			raw = rawText(t["name"])
		}
		steps := stepsFromText(raw, section)
		if len(steps) == 1 {
			steps[0].Title = stepTitle(jsonString(t["name"]), steps[0].Text)
		}
		out = append(out, steps...)
	}
	return out
}

// bareStepRE matches a name that only numbers the step ("Step 3"), which is
// no title.
var bareStepRE = regexp.MustCompile(`(?i)^step\s*\d+\.?$`)

// stepTitle is a HowToStep's name as its title, or "" when the name is empty,
// only numbers the step, or repeats the text (ignoring case and spacing),
// which is also how a step with a name and no text reads.
func stepTitle(name, text string) string {
	name = collapse(name)
	if name == "" || bareStepRE.MatchString(name) || strings.EqualFold(name, collapse(text)) {
		return ""
	}
	return name
}
