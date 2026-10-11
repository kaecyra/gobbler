package extract

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
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
)

// InstructionStep is one instruction text. Section is the HowToSection name it
// sat under, empty for a flat list.
type InstructionStep struct {
	Section string `json:"section,omitempty"`
	Text    string `json:"text"`
}

// Raw is a recipe as the page states it. Ingredient lines and time fields are
// the page's own strings (times are usually ISO 8601); parse them downstream.
type Raw struct {
	Name         string            `json:"name,omitempty"`
	Author       string            `json:"author,omitempty"`
	Description  string            `json:"description,omitempty"`
	Yield        []string          `json:"yield,omitempty"`
	PrepTime     string            `json:"prep_time,omitempty"`
	CookTime     string            `json:"cook_time,omitempty"`
	TotalTime    string            `json:"total_time,omitempty"`
	Ingredients  []string          `json:"ingredients,omitempty"`
	Instructions []InstructionStep `json:"instructions,omitempty"`
	Calories     string            `json:"calories,omitempty"`
	ImageURL     string            `json:"image_url,omitempty"`
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
		res.Method, res.Recipe = MethodJSONLD, &raw
		return res, nil
	}
	if raw, ok := extractMicrodata(doc); ok {
		res.Method, res.Recipe = MethodMicrodata, &raw
		return res, nil
	}
	if u, perr := url.Parse(sourceURL); perr == nil && u.Host != "" {
		if e, found := r.lookup(u.Hostname()); found {
			if raw, ok := e.Extract(doc, u); ok && raw.usable() {
				res.Method, res.Recipe = MethodSite, &raw
				return res, nil
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
