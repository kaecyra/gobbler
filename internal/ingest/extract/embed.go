package extract

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/kaecyra/gobbler/internal/video"
)

// applyEmbeddedVideo sets raw.VideoID from the first YouTube embed in the page
// when the Recipe markup named no YouTube video (ADR-00023). Markup wins.
func applyEmbeddedVideo(doc *html.Node, raw *Raw) {
	if raw.VideoID == "" {
		raw.VideoID = embeddedVideoID(doc)
	}
}

// embeddedVideoID is the video ID of the first iframe, in document order,
// whose source is a youtube.com/embed/ or youtube-nocookie.com/embed/ address.
// A lazy-loading iframe keeps its address in data-src until it scrolls into
// view, so that is read when src is empty. video.ParseID owns the host check:
// an iframe on any other host, or at a non-embed YouTube path, is skipped.
func embeddedVideoID(doc *html.Node) string {
	var id string
	walk(doc, func(n *html.Node) bool {
		if id != "" {
			return false
		}
		if !isElem(n, "iframe") {
			return true
		}
		src := strings.TrimSpace(attr(n, "src"))
		if src == "" {
			src = strings.TrimSpace(attr(n, "data-src"))
		}
		if !strings.Contains(src, "/embed/") {
			return true
		}
		if got, err := video.ParseID(src); err == nil && !video.IsID(src) {
			id = got
		}
		return true
	})
	return id
}
