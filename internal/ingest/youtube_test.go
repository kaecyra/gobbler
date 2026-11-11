package ingest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
)

const youTubeURL = "https://www.youtube.com/watch?v=" + testVideoID

// stubDescriptions is the YouTube Data API boundary.
type stubDescriptions struct {
	desc  video.Description
	err   error
	asked []string
}

func (s *stubDescriptions) Fetch(_ context.Context, id string) (video.Description, error) {
	s.asked = append(s.asked, id)
	return s.desc, s.err
}

// pages serves a fixed set of URLs to the pipeline's fetch and records each
// request. A URL it does not know answers 404.
type pages struct {
	mu   sync.Mutex
	body map[string][]byte
	got  []string
}

func (p *pages) Do(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, r.URL.String())
	if b, ok := p.body[r.URL.String()]; ok {
		resp := response(200, "text/html", string(b))
		resp.Request = r
		return resp, nil
	}
	return response(404, "text/html", "not here"), nil
}

func (e *env) youTubePipeline(t *testing.T, c HTTPClient, src DescriptionSource, logs *bytes.Buffer) *Pipeline {
	t.Helper()
	var log *slog.Logger
	if logs != nil {
		log = slog.New(slog.NewTextHandler(logs, nil))
	}
	p, err := New(testConfig(), Deps{HTTP: c, Catalog: e.catalog, Provider: e.spy, Descriptions: src, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var spaghettiDescription = video.Description{
	Title: "Garlic Spaghetti",
	Text:  string(readFixtureNoT("garlic_spaghetti.txt")),
}

func readFixtureNoT(name string) []byte {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	// Drop the fixture's own title line: the video title supplies it.
	return bytes.TrimSpace(bytes.TrimPrefix(b, []byte("Garlic Spaghetti\n")))
}

func TestYouTubeDescriptionIsDraftedAsStoredText(t *testing.T) {
	e := newEnv(t)
	src := &stubDescriptions{desc: spaghettiDescription}
	pg := &pages{}
	d, err := e.youTubePipeline(t, pg, src, nil).FromURL(e.ctx, youTubeURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.asked) != 1 || src.asked[0] != testVideoID {
		t.Errorf("asked = %v, want the video ID once", src.asked)
	}
	if d.Source.Kind != store.SnapshotText || d.Source.URL != youTubeURL {
		t.Errorf("source = %s %q, want text under the YouTube address", d.Source.Kind, d.Source.URL)
	}
	if !strings.HasPrefix(string(d.Source.Content), "Garlic Spaghetti\n\nIngredients") {
		t.Errorf("snapshot = %q, want the title then the description", d.Source.Content)
	}
	if d.VideoID != testVideoID {
		t.Errorf("VideoID = %q, want the linked video kept", d.VideoID)
	}
	if d.Name.Value != "Garlic Spaghetti" || len(d.Components) == 0 {
		t.Errorf("draft name %q, %d components; want the rules to have read the text", d.Name.Value, len(d.Components))
	}
	if len(pg.got) != 0 {
		t.Errorf("fetched %v, want no page: the description had no links", pg.got)
	}
	if e.spy.count() != 0 {
		t.Error("the LLM was called on a draft the rules read cleanly")
	}
}

func TestALinkedPageWithARecipeWinsOverTheDescription(t *testing.T) {
	e := newEnv(t)
	blog := "https://blog.example/pancakes"
	src := &stubDescriptions{desc: video.Description{Title: "Pancakes", Text: "Full recipe: " + blog + ".\nIngredients\n1 egg"}}
	pg := &pages{body: map[string][]byte{blog: readFixture(t, "pancakes.html")}}
	d, err := e.youTubePipeline(t, pg, src, nil).FromURL(e.ctx, youTubeURL)
	if err != nil {
		t.Fatal(err)
	}
	if d.Source.Kind != store.SnapshotHTML || d.Source.URL != blog {
		t.Errorf("source = %s %q, want the blog page", d.Source.Kind, d.Source.URL)
	}
	if d.Name.Value != "Fluffy Pancakes" {
		t.Errorf("name = %q, want the page's recipe", d.Name.Value)
	}
	if d.VideoID != testVideoID {
		t.Errorf("VideoID = %q, want the video the person linked", d.VideoID)
	}
	if len(pg.got) != 1 || pg.got[0] != blog {
		t.Errorf("fetched %v, want only the blog (trailing full stop trimmed)", pg.got)
	}
}

func TestALinkedPageWithoutARecipeFallsBackToTheDescription(t *testing.T) {
	e := newEnv(t)
	shop := "https://shop.example/knife"
	src := &stubDescriptions{desc: video.Description{Title: spaghettiDescription.Title, Text: "Knife: " + shop + "\n" + spaghettiDescription.Text}}
	pg := &pages{body: map[string][]byte{shop: readFixture(t, "plain_page.html")}}
	d, err := e.youTubePipeline(t, pg, src, nil).FromURL(e.ctx, youTubeURL)
	if err != nil {
		t.Fatal(err)
	}
	if d.Source.Kind != store.SnapshotText || len(pg.got) != 1 {
		t.Errorf("source %s, fetched %v; want the description after trying the link", d.Source.Kind, pg.got)
	}
}

func TestDescriptionLinksAreCappedDedupedAndNeverYouTube(t *testing.T) {
	text := "https://youtu.be/abc https://www.youtube.com/@chan https://a.example/1 https://a.example/1 " +
		"(https://b.example/2), https://c.example/3; https://d.example/4 https://e.example/5"
	got := descriptionLinks(text)
	want := []string{"https://a.example/1", "https://b.example/2", "https://c.example/3"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("links = %v, want %v", got, want)
	}
}

func TestAFailedLinkDoesNotStopTheNextOne(t *testing.T) {
	e := newEnv(t)
	good := "https://good.example/p"
	src := &stubDescriptions{desc: video.Description{Title: "x", Text: "https://gone.example/p " + good}}
	pg := &pages{body: map[string][]byte{good: readFixture(t, "pancakes.html")}}
	var logs bytes.Buffer
	d, err := e.youTubePipeline(t, pg, src, &logs).FromURL(e.ctx, youTubeURL)
	if err != nil || d.Source.URL != good {
		t.Fatalf("err %v, source %q; want the second link's page", err, d.Source.URL)
	}
	if !strings.Contains(logs.String(), "gone.example") {
		t.Errorf("log = %q, want the unread link logged", logs.String())
	}
}

func TestWithNoKeyAYouTubeLinkIsFetchedAsAnyPage(t *testing.T) {
	e := newEnv(t)
	src := &stubDescriptions{err: video.ErrNoKey}
	pg := &pages{body: map[string][]byte{youTubeURL: readFixture(t, "pancakes.html")}}
	var logs bytes.Buffer
	d, err := e.youTubePipeline(t, pg, src, &logs).FromURL(e.ctx, youTubeURL)
	if err != nil || d.Source.Kind != store.SnapshotHTML || len(pg.got) != 1 {
		t.Fatalf("err %v, kind %s, fetched %v; want today's page fetch", err, d.Source.Kind, pg.got)
	}
	if logs.Len() != 0 {
		t.Errorf("log = %q, want silence: no key is the normal off state", logs.String())
	}
}

func TestANonYouTubeURLNeverAsksYouTube(t *testing.T) {
	e := newEnv(t)
	src := &stubDescriptions{}
	pg := &pages{body: map[string][]byte{"https://blog.example/p": readFixture(t, "pancakes.html")}}
	if _, err := e.youTubePipeline(t, pg, src, nil).FromURL(e.ctx, "https://blog.example/p"); err != nil {
		t.Fatal(err)
	}
	if len(src.asked) != 0 {
		t.Errorf("asked %v", src.asked)
	}
}

func TestAFailedLookupIsAClearErrorNotAnEmptyDraft(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"quota":       {video.ErrQuota, "daily limit"},
		"key":         {video.ErrKeyRejected, "did not accept the saved API key"},
		"unavailable": {video.ErrVideoUnavailable, "private or has been removed"},
		"other":       {video.ErrLookup, "did not answer"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			pg := &pages{}
			var logs bytes.Buffer
			_, err := e.youTubePipeline(t, pg, &stubDescriptions{err: tc.err}, &logs).FromURL(e.ctx, youTubeURL)
			if !errors.Is(err, ErrFetch) || !errors.Is(err, tc.err) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrFetch and %v saying %q", err, tc.err, tc.want)
			}
			if len(pg.got) != 0 {
				t.Errorf("fetched %v after a failed lookup", pg.got)
			}
			if !strings.Contains(logs.String(), "description lookup failed") {
				t.Errorf("log = %q, want the failure logged", logs.String())
			}
		})
	}
}

func TestAVideoWithNoDescriptionIsAnError(t *testing.T) {
	e := newEnv(t)
	src := &stubDescriptions{desc: video.Description{Title: "Just a title"}}
	_, err := e.youTubePipeline(t, &pages{}, src, nil).FromURL(e.ctx, youTubeURL)
	if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), "no description") {
		t.Fatalf("err = %v", err)
	}
}

// A recipe whose blog source does not name the video keeps it on re-import
// (ADR-00054); the blog is now its source address.
func TestReimportKeepsTheVideoTheBlogDoesNotName(t *testing.T) {
	e := newEnv(t)
	blog := "https://blog.example/pancakes"
	src := &stubDescriptions{desc: video.Description{Title: "P", Text: blog}}
	pg := &pages{body: map[string][]byte{blog: readFixture(t, "pancakes.html")}}
	pipe := e.youTubePipeline(t, pg, src, nil)
	d, err := pipe.FromURL(e.ctx, youTubeURL)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Confirm(d)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := e.saver().Save(e.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	drafts := &heldDrafts{}
	r := NewReimporter(e.store, pipe, drafts)
	if _, err := r.Start(e.ctx, saved.RecipeID, store.Provenance{Person: auth.DevEmail}); err != nil {
		t.Fatal(err)
	}
	if drafts.d.VideoID != testVideoID {
		t.Errorf("re-import VideoID = %q, want %q", drafts.d.VideoID, testVideoID)
	}
}

type heldDrafts struct{ d Draft }

func (h *heldDrafts) Add(_ context.Context, d Draft, _ store.Provenance) (string, error) {
	h.d = d
	return "tok", nil
}

// ADR-00054: an Instagram link is refused before anything is fetched, whatever
// subdomain it is on, and the message says to paste the caption.
func TestAnInstagramLinkIsRefusedBeforeAnyFetch(t *testing.T) {
	for _, link := range []string{
		"https://instagram.com/p/Cabc123/",
		"https://www.instagram.com/p/Cabc123/",
		"https://m.instagram.com/reel/Cabc123/",
		"http://WWW.Instagram.com./p/Cabc123/?igsh=x",
	} {
		t.Run(link, func(t *testing.T) {
			e := newEnv(t)
			pg := &pages{}
			src := &stubDescriptions{}
			_, err := e.youTubePipeline(t, pg, src, nil).FromURL(e.ctx, link)
			if !errors.Is(err, ErrPasteOnly) || !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), "Paste the caption as text") {
				t.Fatalf("err = %v, want ErrPasteOnly (an ErrFetch) telling the person to paste the caption", err)
			}
			if len(pg.got) != 0 || len(src.asked) != 0 {
				t.Errorf("fetched %v, asked %v; want nothing", pg.got, src.asked)
			}
		})
	}
}

func TestOnlyInstagramHostsArePasteOnly(t *testing.T) {
	for _, link := range []string{"https://notinstagram.com/p/1", "https://instagram.com.evil.example/p/1", "https://blog.example/instagram.com"} {
		if err := checkPasteOnly(link); err != nil {
			t.Errorf("%s: %v", link, err)
		}
	}
}

// Only a YouTube video link with a scheme reaches the description lookup: a bare
// ID is not a URL, and a scheme-less link is not one the fetch accepts either.
func TestOnlySchemedYouTubeVideoLinksReachTheLookup(t *testing.T) {
	for link, want := range map[string]bool{
		"https://www.youtube.com/watch?v=" + testVideoID: true,
		"http://youtube.com/watch?v=" + testVideoID:      true,
		"https://youtu.be/" + testVideoID:                true,
		"https://www.youtube.com/shorts/" + testVideoID:  true,
		"https://www.youtube.com/embed/" + testVideoID:   true,
		testVideoID:                                   false,
		"youtu.be/" + testVideoID:                     false,
		"www.youtube.com/watch?v=" + testVideoID:      false,
		"https://www.youtube.com/@somechannel":        false,
		"https://blog.example/watch?v=" + testVideoID: false,
		"https://vimeo.com/" + testVideoID:            false,
	} {
		t.Run(link, func(t *testing.T) {
			e := newEnv(t)
			src := &stubDescriptions{desc: spaghettiDescription}
			// Whatever is not looked up is fetched (and 404s, which is fine here).
			_, _ = e.youTubePipeline(t, &pages{}, src, nil).FromURL(e.ctx, link)
			if got := len(src.asked) == 1; got != want {
				t.Errorf("asked = %v, want lookup = %v", src.asked, want)
			}
			if _, ok := youTubeVideoID(link); ok != want {
				t.Errorf("youTubeVideoID ok = %v, want %v", ok, want)
			}
		})
	}
}
