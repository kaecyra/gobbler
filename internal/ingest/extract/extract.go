package extract

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/kaecyra/gobbler/internal/parse"
)

// ErrEmptyInput is returned for HTML with no content. It is the only error:
// anything else, however broken, yields a Result.
var ErrEmptyInput = errors.New("html is empty")

// Method says which rule produced a Result.
type Method string

// Methods, in the order they are tried (ADR-00006).
const (
	// MethodJSONLD: a schema.org Recipe in a JSON-LD script block.
	MethodJSONLD Method = "jsonld"
	// MethodMicrodata: a schema.org Recipe marked up with itemtype/itemprop.
	MethodMicrodata Method = "microdata"
	// MethodSite: a registered site extractor matched the host.
	MethodSite Method = "site"
	// MethodText: nothing structured matched; Result.Text holds the page's
	// main text, to be sectioned as pasted text.
	MethodText Method = "text"
	// MethodNone: nothing structured matched and the page has no text.
	MethodNone Method = "none"
)

// Issues Extract can report: data about a less certain result, not failures.
const (
	// IssueMalformedJSONLD: a JSON-LD block could not be parsed and was skipped.
	IssueMalformedJSONLD = "malformed_jsonld"
	// IssueNoStructuredData: no JSON-LD, microdata or site extractor matched.
	IssueNoStructuredData = "no_structured_data"
	// IssueNoText: the page has no readable text.
	IssueNoText = "no_text"
	// IssueUnparsedDuration: a time field was not a single clean duration
	// that parse.Duration reads (a range, a fraction such as PT0.5H, or mixed
	// with other text), so its Duration field is zero. The raw string is kept.
	IssueUnparsedDuration = "unparsed_duration"
)

// InstructionStep is one instruction text. Section is the HowToSection name it
// sat under, empty for a flat list.
type InstructionStep struct {
	Section string `json:"section,omitempty"`
	Text    string `json:"text"`
}

// Raw is a recipe as the page states it. Ingredient lines are the page's own
// strings, to be parsed downstream. Time fields keep the page's own strings
// (usually ISO 8601) and are also parsed into the matching Duration fields.
type Raw struct {
	Name        string   `json:"name,omitempty"`
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Yield       []string `json:"yield,omitempty"`
	PrepTime    string   `json:"prep_time,omitempty"`
	CookTime    string   `json:"cook_time,omitempty"`
	TotalTime   string   `json:"total_time,omitempty"`
	// PrepDuration, CookDuration and TotalDuration are the parsed time
	// fields; zero when the field is absent or was not read (see
	// IssueUnparsedDuration).
	PrepDuration  time.Duration `json:"prep_duration,omitempty"`
	CookDuration  time.Duration `json:"cook_duration,omitempty"`
	TotalDuration time.Duration `json:"total_duration,omitempty"`
	Ingredients   []string      `json:"ingredients,omitempty"`
	// IngredientGroups is the same ingredients as Ingredients, grouped under
	// the headings a recipe plugin on the page prints. Set only when the
	// groups hold exactly Ingredients, in order; nil otherwise.
	IngredientGroups []IngredientGroup `json:"ingredient_groups,omitempty"`
	Instructions     []InstructionStep `json:"instructions,omitempty"`
	Calories         string            `json:"calories,omitempty"`
	ImageURL         string            `json:"image_url,omitempty"`
}

// usable reports whether r holds enough to be worth reviewing: a name plus
// ingredients or instructions. Anything less is treated as no match so the
// next method gets a turn.
func (r Raw) usable() bool {
	return r.Name != "" && (len(r.Ingredients) > 0 || len(r.Instructions) > 0)
}

// Result is the outcome of Extract.
type Result struct {
	Method    Method `json:"method"`
	SourceURL string `json:"source_url,omitempty"`
	// Recipe is set for the structured methods.
	Recipe *Raw `json:"recipe,omitempty"`
	// Text is the page's main text for MethodText.
	Text   string   `json:"text,omitempty"`
	Issues []string `json:"issues,omitempty"`
}

// SiteExtractor reads a recipe from a page of one host that publishes neither
// JSON-LD nor microdata. Host is the lower-case host without "www.". Extract
// returns false when the page does not match.
type SiteExtractor interface {
	Host() string
	Extract(doc *html.Node, page *url.URL) (Raw, bool)
}

// Registry holds site extractors keyed by host. The zero value is ready to use.
type Registry struct {
	byHost map[string]SiteExtractor
}

// Register adds an extractor. A second extractor for a host is an error, not
// a silent replacement.
func (r *Registry) Register(e SiteExtractor) error {
	host := normalizeHost(e.Host())
	if host == "" {
		return errors.New("register site extractor: empty host")
	}
	if _, dup := r.byHost[host]; dup {
		return fmt.Errorf("register site extractor: host %q already registered", host)
	}
	if r.byHost == nil {
		r.byHost = map[string]SiteExtractor{}
	}
	r.byHost[host] = e
	return nil
}

func (r *Registry) lookup(host string) (SiteExtractor, bool) {
	e, ok := r.byHost[normalizeHost(host)]
	return e, ok
}

func normalizeHost(h string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h)), "www.")
}

// Default is the registry used by Extract. Site extractors register here.
var Default = &Registry{}

// Extract reads a recipe from page HTML fetched from sourceURL, using the
// Default registry. See Registry.Extract.
func Extract(page []byte, sourceURL string) (Result, error) {
	return Default.Extract(page, sourceURL)
}

// Extract tries JSON-LD, then microdata, then the site extractor for the
// source host, then falls back to the page's main text. It is pure and
// deterministic. Whitespace-only input returns ErrEmptyInput; any other input
// returns a Result. An unparseable sourceURL only disables the site step.
func (r *Registry) Extract(page []byte, sourceURL string) (Result, error) {
	if len(bytes.TrimSpace(page)) == 0 {
		return Result{}, ErrEmptyInput
	}
	// html.Parse accepts any input and never fails on malformed markup.
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return Result{}, fmt.Errorf("parse html: %w", err)
	}
	res := Result{SourceURL: sourceURL}

	raw, issues, ok := extractJSONLD(doc)
	res.Issues = append(res.Issues, issues...)
	if ok {
		applyPlugins(doc, &raw)
		return res.withRecipe(MethodJSONLD, raw), nil
	}
	if raw, ok := extractMicrodata(doc); ok {
		applyPlugins(doc, &raw)
		return res.withRecipe(MethodMicrodata, raw), nil
	}
	if u, perr := url.Parse(sourceURL); perr == nil && u.Host != "" {
		if e, found := r.lookup(u.Hostname()); found {
			if raw, ok := e.Extract(doc, u); ok && raw.usable() {
				return res.withRecipe(MethodSite, raw), nil
			}
		}
	}

	res.Issues = append(res.Issues, IssueNoStructuredData)
	res.Text = mainText(doc)
	if res.Text == "" {
		res.Method = MethodNone
		res.Issues = append(res.Issues, IssueNoText)
		return res, nil
	}
	res.Method = MethodText
	return res, nil
}

// withRecipe sets a structured result, parsing the recipe's time fields.
func (r Result) withRecipe(m Method, raw Raw) Result {
	r.Method = m
	r.Issues = append(r.Issues, raw.parseDurations()...)
	r.Recipe = &raw
	return r
}

// parseDurations fills the Duration fields from the raw time strings with
// parse.Duration. A field that is not one clean duration is left zero and
// reported once as IssueUnparsedDuration: low confidence is data, not an error.
func (r *Raw) parseDurations() []string {
	bad := false
	for _, f := range []struct {
		raw string
		dst *time.Duration
	}{{r.PrepTime, &r.PrepDuration}, {r.CookTime, &r.CookDuration}, {r.TotalTime, &r.TotalDuration}} {
		if strings.TrimSpace(f.raw) == "" {
			continue
		}
		d := parse.Duration(f.raw)
		if !d.Found || d.Range || d.Confidence < 1 {
			bad = true
			continue
		}
		*f.dst = d.Min
	}
	if bad {
		return []string{IssueUnparsedDuration}
	}
	return nil
}
