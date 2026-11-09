package ingest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	core "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// originals is a saved recipe's original page and first version, by id, with
// a count of the reads.
type originals struct {
	snaps map[int64]store.Snapshot
	first map[int64]recipe.Recipe
	reads atomic.Int32
}

func (o *originals) FirstSnapshot(_ context.Context, id int64, kind store.SnapshotKind) (store.Snapshot, bool, error) {
	o.reads.Add(1)
	if _, ok := o.first[id]; !ok {
		return store.Snapshot{}, false, store.ErrNotFound
	}
	s, ok := o.snaps[id]
	if !ok || s.Kind != kind {
		return store.Snapshot{}, false, nil
	}
	return s, true, nil
}

func (o *originals) GetVersion(_ context.Context, id, version int64) (recipe.Recipe, error) {
	o.reads.Add(1)
	r, ok := o.first[id]
	if !ok || version != 1 {
		return recipe.Recipe{}, store.ErrNotFound
	}
	return r, nil
}

type previewFixture struct {
	svc       *ingest.Service
	provider  *countingProvider
	originals *originals
	page      []byte
}

// newPreviewFixture is the service over the real catalog, which matches every
// ingredient of the pancakes page, with a counting provider. Recipe 1 was
// imported from the pancakes page by parser version 7 and has since had its
// source address edited; recipe 2 was typed in, with only a text snapshot.
func newPreviewFixture(t *testing.T) previewFixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile("../../ingest/testdata/pancakes.html")
	if err != nil {
		t.Fatal(err)
	}
	o := &originals{
		snaps: map[int64]store.Snapshot{
			1: {Kind: store.SnapshotHTML, Content: page, ParserVersion: "7"},
			2: {Kind: store.SnapshotText, Content: []byte(recipeText)},
		},
		first: map[int64]recipe.Recipe{1: {SourceURL: "https://example.test/pancakes"}, 2: {}},
	}
	prov := &countingProvider{}
	svc, err := ingest.New(
		config.Ingestion{ConfidenceThreshold: 0.7, FetchTimeout: time.Second, UserAgent: "t", AllowLoopbackFetch: true},
		ingest.Deps{Catalog: catalog.New(d), Provider: prov, Members: members{"ann@example.com": smiths, "jo@example.com": joneses}, Originals: o},
	)
	if err != nil {
		t.Fatal(err)
	}
	return previewFixture{svc: svc, provider: prov, originals: o, page: page}
}

func servePage(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/recipe"
}

var ann = ingest.Caller{Person: "ann@example.com", Household: smiths}

// ADR-00048 obligation 4, service half: a page the rules read at or above the
// threshold is never sent to the provider, and the preview says the LLM was
// not asked. A page below it is, as for an import (ADR-00006).
func TestPreviewAsksTheLLMOnlyBelowTheThreshold(t *testing.T) {
	f := newPreviewFixture(t)
	p, err := f.svc.Preview(context.Background(), ann, ingest.PreviewSource{URL: servePage(t, f.page)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Draft.BelowThreshold {
		t.Fatalf("the pancakes page scored %v, below the threshold %v: the test needs a high-confidence page", p.Draft.Overall, p.Draft.Threshold)
	}
	if n := f.provider.asked.Load(); n != 0 || p.Draft.AskedLLM() {
		t.Errorf("provider asked %d times, AskedLLM %v; want neither for a high-confidence page", n, p.Draft.AskedLLM())
	}

	low := []byte("<html><body><h1>Stew</h1><p>Some of this and a bit of that, cooked until it is done.</p></body></html>")
	p, err = f.svc.Preview(context.Background(), ann, ingest.PreviewSource{URL: servePage(t, low)})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Draft.BelowThreshold {
		t.Fatalf("the low page scored %v: the test needs a page below the threshold", p.Draft.Overall)
	}
	if n := f.provider.asked.Load(); n != 1 || !p.Draft.AskedLLM() {
		t.Errorf("provider asked %d times, AskedLLM %v; want once and true below the threshold", n, p.Draft.AskedLLM())
	}
}

// ADR-00048, Preview: a saved recipe's preview drafts its original page with
// the address of its first version, fetches nothing, and reports the parser
// version stored with the snapshot beside the current one.
func TestPreviewOfASavedRecipeReplaysItsOriginalPage(t *testing.T) {
	f := newPreviewFixture(t)
	p, err := f.svc.Preview(context.Background(), ann, ingest.PreviewSource{RecipeID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.SnapshotParserVersion != "7" || p.Draft.ParserVersion != core.ParserVersion {
		t.Errorf("parser versions = snapshot %q, draft %q; want 7 and %s", p.SnapshotParserVersion, p.Draft.ParserVersion, core.ParserVersion)
	}
	if p.Draft.Source.URL != "https://example.test/pancakes" || string(p.Draft.Source.Content) != string(f.page) {
		t.Errorf("draft source = %q, want the original page at the first version's address", p.Draft.Source.URL)
	}
	if p.Draft.Name.Value != "Fluffy Pancakes" {
		t.Errorf("draft name = %q, want the page's recipe", p.Draft.Name.Value)
	}
}

func TestPreviewRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		caller ingest.Caller
		src    ingest.PreviewSource
		want   error
	}{
		"no html snapshot": {ann, ingest.PreviewSource{RecipeID: 2}, ingest.ErrNoPageSnapshot},
		"missing recipe":   {ann, ingest.PreviewSource{RecipeID: 9}, store.ErrNotFound},
		"both":             {ann, ingest.PreviewSource{URL: "https://example.test/x", RecipeID: 1}, ingest.ErrPreviewSource},
		"neither":          {ann, ingest.PreviewSource{}, ingest.ErrPreviewSource},
		"not a member":     {ingest.Caller{Person: "nobody@example.com", Household: smiths}, ingest.PreviewSource{RecipeID: 1}, ingest.ErrNotMember},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPreviewFixture(t)
			if _, err := f.svc.Preview(context.Background(), c.caller, c.src); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if errors.Is(c.want, ingest.ErrNotMember) || errors.Is(c.want, ingest.ErrPreviewSource) {
				if n := f.originals.reads.Load(); n != 0 {
					t.Errorf("%d reads of saved recipes before the refusal", n)
				}
			}
			if n := f.provider.asked.Load(); n != 0 {
				t.Errorf("the LLM was asked %d times for a refused preview", n)
			}
		})
	}
}

// Bound.Preview names the Caller from the context like every other operation.
func TestBoundPreviewNamesTheCaller(t *testing.T) {
	f := newPreviewFixture(t)
	b := f.svc.Bind(func(ctx context.Context) ingest.Caller {
		if v, ok := ctx.Value(whoKey{}).(ingest.Caller); ok {
			return v
		}
		return ingest.Caller{}
	})
	if _, err := b.Preview(context.WithValue(context.Background(), whoKey{}, ann), ingest.PreviewSource{RecipeID: 1}); err != nil {
		t.Errorf("a context with a member: %v", err)
	}
	if _, err := b.Preview(context.Background(), ingest.PreviewSource{RecipeID: 1}); !errors.Is(err, ingest.ErrNotMember) {
		t.Errorf("a context with nobody: err = %v, want ErrNotMember", err)
	}
}
