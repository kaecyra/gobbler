// Package video handles the YouTube video a recipe may carry (ADR-00021): it
// reads a pasted link or bare ID into the 11-character video ID the recipe
// stores, names the two YouTube addresses the application ever uses (the
// thumbnail the server downloads at save, and the privacy-enhanced embed the
// browser loads only after Play is pressed), and downloads the thumbnail. It
// stores nothing; the store owns the thumbnail table.
package video

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// IDLength is the length of a YouTube video ID.
const IDLength = 11

// ErrInvalidLink is returned (wrapped) by ParseID for text that is neither a
// supported YouTube link nor a bare video ID.
var ErrInvalidLink = errors.New("not a YouTube video link")

// ErrInvalidID is returned for an ID that is not 11 characters of letters,
// digits, "-" and "_".
var ErrInvalidID = errors.New("not a YouTube video ID")

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// IsID reports whether s is a well-formed video ID.
func IsID(s string) bool { return idPattern.MatchString(s) }

// ParseID reads what the editor field holds. Empty (after trimming) means the
// recipe has no video and returns "" with no error. A bare ID is returned as
// is. Otherwise s must be a link to youtube.com (or its www., m. and music.
// hosts) in the form /watch?v=ID, /shorts/ID or /embed/ID, a youtu.be/ID
// short link, or a youtube-nocookie.com/embed/ID link; the scheme may be left
// off. Anything else, including a link to another host, wraps ErrInvalidLink.
//
// The forms were checked against YouTube on 2026-10-17: the watch, shorts,
// youtu.be short-link and nocookie embed addresses of a public video all
// answer (200, or 303 to the watch page for the short forms).
func ParseID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if IsID(s) {
		return s, nil
	}
	raw := s
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidLink, s)
	}
	host := strings.ToLower(u.Hostname())
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		id = idFromYouTubePath(u)
	case "youtube-nocookie.com", "www.youtube-nocookie.com":
		if rest, ok := strings.CutPrefix(u.Path, "/embed/"); ok {
			id = strings.TrimSuffix(rest, "/")
		}
	}
	if !IsID(id) {
		return "", fmt.Errorf("%w: %q", ErrInvalidLink, s)
	}
	return id, nil
}

func idFromYouTubePath(u *url.URL) string {
	switch {
	case u.Path == "/watch" || u.Path == "/watch/":
		return u.Query().Get("v")
	case strings.HasPrefix(u.Path, "/shorts/"):
		return strings.TrimSuffix(strings.TrimPrefix(u.Path, "/shorts/"), "/")
	case strings.HasPrefix(u.Path, "/embed/"):
		return strings.TrimSuffix(strings.TrimPrefix(u.Path, "/embed/"), "/")
	}
	return ""
}

// Message words ErrInvalidLink for the editor.
const Message = "Enter a YouTube link (a watch, youtu.be, shorts or embed address) or the 11-character video ID, or leave it empty."

// EmbedURL is the address of the privacy-enhanced player for id, set to start
// playing at once. It is loaded only after a person presses Play (ADR-00021).
func EmbedURL(id string) string {
	return "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1&rel=0"
}

// thumbnailPath is the path of the thumbnail on YouTube's image host.
func thumbnailPath(id string) string { return "/vi/" + id + "/hqdefault.jpg" }
