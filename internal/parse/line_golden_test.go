package parse

import (
	"bytes"
	"encoding/json"
	"flag"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// lineUpdate regenerates testdata/lines/corpus.golden.json. It is never on by
// default; review the diff of the golden file before committing it.
var lineUpdate = flag.Bool("update", false, "rewrite the line parser golden files")

const (
	lineCorpusPath = "testdata/lines/corpus.txt"
	lineGoldenPath = "testdata/lines/corpus.golden.json"
)

// lineGolden is the stable, human-reviewable rendering of a LineResult.
type lineGolden struct {
	Raw        string           `json:"raw"`
	Quantity   string           `json:"quantity,omitempty"`
	Range      bool             `json:"range,omitempty"`
	Unit       string           `json:"unit,omitempty"`
	Package    string           `json:"package,omitempty"`
	ToTaste    bool             `json:"to_taste,omitempty"`
	Optional   bool             `json:"optional,omitempty"`
	Name       string           `json:"name"`
	Match      string           `json:"match,omitempty"`
	Qualifiers []lineGoldenQual `json:"qualifiers,omitempty"`
	Confidence float64          `json:"confidence"`
	Issues     []string         `json:"issues,omitempty"`
}

type lineGoldenQual struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func lineToGolden(r LineResult) lineGolden {
	g := lineGolden{
		Raw: r.Raw, Quantity: r.Amount.Qty.String(), Range: r.Amount.Qty.IsRange(),
		Unit: r.Amount.Unit.Name, ToTaste: r.ToTaste, Optional: r.Optional,
		Name: r.Name, Confidence: r.Confidence, Issues: r.Issues,
	}
	if r.Amount.HasPackage {
		g.Package = quantity.Amount{Qty: r.Amount.Package.Qty, Unit: r.Amount.Package.Unit}.String()
	}
	if r.Matched {
		g.Match = r.Match.Name
	}
	for _, q := range r.Qualifiers {
		g.Qualifiers = append(g.Qualifiers, lineGoldenQual{string(q.Kind), q.Text})
	}
	return g
}

func lineLoadCorpus(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(lineCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimRight(l, "\r"); strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func lineRenderCorpus(t *testing.T, lines []string) []byte {
	t.Helper()
	c := lineLoadCatalog(t)
	out := make([]lineGolden, len(lines))
	for i, l := range lines {
		out[i] = lineToGolden(IngredientLine(l, c))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLineGolden(t *testing.T) {
	got := lineRenderCorpus(t, lineLoadCorpus(t))
	if *lineUpdate {
		if err := os.WriteFile(lineGoldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(lineGoldenPath)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/parse -run TestLineGolden -update to create it)", err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var g, w []lineGolden
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	for i := range g {
		if i >= len(w) {
			t.Errorf("line %q has no golden entry", g[i].Raw)
		} else if !reflect.DeepEqual(g[i], w[i]) {
			t.Errorf("line %q:\n got  %+v\n want %+v", g[i].Raw, g[i], w[i])
		}
	}
	if len(w) > len(g) {
		t.Errorf("golden has %d entries beyond the corpus", len(w)-len(g))
	}
	t.Error("golden mismatch; if the change is intended, rerun with -update and review the diff")
}

// TestLineDeterminism parses the corpus twice and in shuffled order: a
// line's result depends on that line alone.
func TestLineDeterminism(t *testing.T) {
	lines := lineLoadCorpus(t)
	c := lineLoadCatalog(t)
	first := make(map[string]lineGolden, len(lines))
	for _, l := range lines {
		first[l] = lineToGolden(IngredientLine(l, c))
	}

	check := func(name string, order []string) {
		for _, l := range order {
			if got := lineToGolden(IngredientLine(l, c)); !reflect.DeepEqual(got, first[l]) {
				t.Errorf("%s: %q gave %+v, first run gave %+v", name, l, got, first[l])
			}
		}
	}
	check("second pass", lines)

	shuffled := append([]string(nil), lines...)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	check("shuffled", shuffled)
}

// TestLineCorpusNeverErrors proves low confidence is data: every corpus line,
// including hard ones, yields a result with a confidence in range.
func TestLineCorpusNeverErrors(t *testing.T) {
	c := lineLoadCatalog(t)
	for _, l := range lineLoadCorpus(t) {
		r := IngredientLine(l, c)
		if r.Raw != l || r.Confidence < 0 || r.Confidence > 1 {
			t.Errorf("%q: raw %q confidence %v", l, r.Raw, r.Confidence)
		}
	}
}
