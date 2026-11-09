package ingest

import (
	"bytes"
	"net/http"
	"reflect"
	"testing"
)

// ADR-00048, Preview: drafting from a URL is a fetch followed by FromContent,
// so a draft made from the fetched page and one made from the same bytes and
// address cannot differ.
func TestFromContentIsWhatFromURLRunsAfterItsFetch(t *testing.T) {
	e := newEnv(t)
	page := readFixture(t, "pancakes.html")
	srv, _ := serve(t, page)
	p := e.pipeline(t, srv.Client())

	fetched, err := p.FromURL(e.ctx, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	held, err := p.FromContent(e.ctx, page, srv.URL+"/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fetched, held) {
		t.Errorf("FromContent differs from FromURL for the same page:\n url:     %+v\n content: %+v", fetched, held)
	}
}

// FromContent fetches nothing: its source is the page it was given, byte for
// byte, at the address it was given.
func TestFromContentFetchesNothing(t *testing.T) {
	e := newEnv(t)
	page := readFixture(t, "pancakes.html")
	noFetch := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("FromContent fetched %s", r.URL)
		return response(http.StatusInternalServerError, "", ""), nil
	})
	d, err := e.pipeline(t, noFetch).FromContent(e.ctx, page, "https://example.test/pancakes")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name.Value != "Fluffy Pancakes" || d.Source.URL != "https://example.test/pancakes" || !bytes.Equal(d.Source.Content, page) || d.ParserVersion != ParserVersion {
		t.Errorf("draft = %q from %q at parser %q; want the page's recipe, its address and the current parser", d.Name.Value, d.Source.URL, d.ParserVersion)
	}
}
