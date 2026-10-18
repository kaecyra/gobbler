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
// that has a YouTube embed address. A lazy-loading iframe keeps its address in
// data-src or data-lazy-src while src holds a placeholder (about:blank), so
// each is tried in turn until one is a YouTube embed. A protocol-relative
// address ("//www.youtube.com/embed/...") gets https. video.ParseID owns the
// host check: an iframe on any other host, or at a non-embed YouTube path, is
// skipped.
func embeddedVideoID(doc *html.Node) string {
	var id string
	walk(doc, func(n *html.Node) bool {
		if id != "" {
			return false
		}
		if !isElem(n, "iframe") {
			return true
		}
		for _, key := range []string{"src", "data-src", "data-lazy-src"} {
			if got := youTubeEmbedID(attr(n, key)); got != "" {
				id = got
				break
			}
		}
		return true
	})
	return id
}

// youTubeEmbedID is the video ID in a YouTube /embed/ address, or "".
func youTubeEmbedID(src string) string {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	if !strings.Contains(src, "/embed/") || video.IsID(src) {
		return ""
	}
	id, err := video.ParseID(src)
	if err != nil {
		return ""
	}
	return id
}
