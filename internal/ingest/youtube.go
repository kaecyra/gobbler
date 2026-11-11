package ingest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
)

// A YouTube link is read from the video's description, or from a recipe page
// the description links to (ADR-00054). The description reaches the pipeline
// as stored text, like pasted text; a linked page reaches it as a fetched page,
// like any URL.

// maxDescriptionLinks is how many links of a description are tried, in order,
// for a page with a schema.org Recipe.
const maxDescriptionLinks = 3

// DescriptionSource reads a video's title and description; *video.Descriptions
// satisfies it. It returns an error wrapping video.ErrNoKey when the lookup is
// off, which is not a failure.
type DescriptionSource interface {
	Fetch(ctx context.Context, id string) (video.Description, error)
}

// ErrPasteOnly is returned (wrapped) for a link gobbler declines to fetch
// because its recipe can only be pasted: an Instagram post's page is a sign-in
// wall (ADR-00054). It wraps ErrFetch, so every caller that already reports a
// failed fetch reports it, and the message tells the person what to do.
var ErrPasteOnly = errors.New("this site cannot be read from a link")

type pasteOnlyError struct{ msg string }

func (e pasteOnlyError) Error() string   { return ErrFetch.Error() + ": " + e.msg }
func (e pasteOnlyError) Unwrap() []error { return []error{ErrFetch, ErrPasteOnly} }

// checkPasteOnly refuses an Instagram link (instagram.com and every subdomain,
// www. and m. included) before anything is fetched.
func checkPasteOnly(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil // the fetch reports a malformed URL
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "instagram.com" || strings.HasSuffix(host, ".instagram.com") {
		return pasteOnlyError{msg: "Instagram links cannot be read. Paste the caption as text instead"}
	}
	return nil
}

// youTubeVideoID returns the video ID of rawURL when it is a YouTube video
// link (not a bare ID).
func youTubeVideoID(rawURL string) (string, bool) {
	s := strings.TrimSpace(rawURL)
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "", false
	}
	id, err := video.ParseID(s)
	return id, err == nil && id != ""
}

// fromYouTube drafts a YouTube link's recipe. handled is false when the
// description lookup is off (no key saved), and the caller then ingests the
// link as any page.
func (p *Pipeline) fromYouTube(ctx context.Context, rawURL, id string) (d Draft, handled bool, err error) {
	desc, err := p.deps.Descriptions.Fetch(ctx, id)
	switch {
	case errors.Is(err, video.ErrNoKey):
		return Draft{}, false, nil
	case err != nil:
		// ErrFetch gives the caller its 502 and its wording; the lookup's own
		// sentinel stays matchable. A degraded draft from the watch page would
		// be an empty one with no explanation (error-handling).
		p.Fallback().log().WarnContext(ctx, "YouTube description lookup failed", "video_id", id, "error", err.Error())
		return Draft{}, true, lookupError{msg: youTubeFailure(err), cause: err}
	}
	log := p.Fallback().log()
	for _, link := range descriptionLinks(desc.Text) {
		page, final, ferr := p.fetch(ctx, link)
		if ferr != nil {
			log.WarnContext(ctx, "a link in a video description was not read", "video_id", id, "link", link, "error", ferr.Error())
			continue
		}
		res, xerr := extract.Extract(page, final)
		if xerr != nil || res.Recipe == nil {
			// A page without structured recipe data is not preferred over the
			// description, which is structured by the same text rules.
			continue
		}
		d, err = p.draftWithFallback(ctx, newSource(store.SnapshotHTML, final, page), res)
		if err != nil {
			return Draft{}, true, err
		}
		if d.VideoID == "" {
			d.VideoID = id
		}
		return d, true, nil
	}
	text := strings.TrimSpace(desc.Title + "\n\n" + desc.Text)
	if desc.Text == "" {
		return Draft{}, true, fmt.Errorf("%w: the video has no description to read a recipe from", ErrFetch)
	}
	res := extract.Result{Method: extract.MethodText, Text: text}
	d, err = p.draftWithFallback(ctx, newSource(store.SnapshotText, strings.TrimSpace(rawURL), []byte(text)), res)
	if err != nil {
		return Draft{}, true, err
	}
	d.VideoID = id
	return d, true, nil
}

// lookupError is a failed description lookup as the person is told of it: the
// message alone, with the API's own detail only in the log. It is an ErrFetch,
// and the lookup's sentinel stays matchable.
type lookupError struct {
	msg   string
	cause error
}

func (e lookupError) Error() string   { return ErrFetch.Error() + ": " + e.msg }
func (e lookupError) Unwrap() []error { return []error{ErrFetch, e.cause} }

// youTubeFailure is what a person is told when the lookup failed.
func youTubeFailure(err error) string {
	switch {
	case errors.Is(err, video.ErrQuota):
		return "YouTube's daily limit for this deployment's API key is used up, so the video's description could not be read. Try again tomorrow, or paste the recipe text"
	case errors.Is(err, video.ErrKeyRejected):
		return "YouTube did not accept the saved API key, so the video's description could not be read. An operator can fix it on the Deployment page, or paste the recipe text"
	case errors.Is(err, video.ErrVideoUnavailable):
		return "that video is private or has been removed"
	}
	return "YouTube did not answer, so the video's description could not be read"
}

var linkPattern = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+`)

// descriptionLinks are the http(s) links in text, in order, without repeats,
// leaving out YouTube's own, at most maxDescriptionLinks.
func descriptionLinks(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range linkPattern.FindAllString(text, -1) {
		m = strings.TrimRight(m, ".,;:!?*")
		u, err := url.Parse(m)
		if err != nil || u.Host == "" || video.IsYouTubeHost(u.Hostname()) || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) == maxDescriptionLinks {
			break
		}
	}
	return out
}
