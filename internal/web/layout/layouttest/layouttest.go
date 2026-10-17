// Package layouttest is the one check behind "no third-party scripts,
// stylesheets, fonts or remote assets" (AGENTS.md Boundaries, ADR-00002). The
// page tests of every web feature call it instead of each grepping for
// "https://", which also rejected plain navigation links.
//
// It rejects a remote origin in what a browser loads on its own: script src,
// link href for a stylesheet, icon, preload or manifest, src of img, source,
// iframe, video, audio, embed and track, srcset, object data, and in CSS any
// url() or @import (which covers @font-face). An <a href> is navigation, not a
// load, and is allowed.
package layouttest

import (
	"regexp"
	"strings"
	"testing"
)

var (
	tagRe   = regexp.MustCompile(`(?is)<(script|link|img|source|iframe|video|audio|embed|track|object)\b([^>]*)>`)
	attrRe  = regexp.MustCompile(`(?is)([a-z][a-z0-9:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	styleRe = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`)
	cssURL  = regexp.MustCompile(`(?is)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)"']*))\s*\)`)
	cssImp  = regexp.MustCompile(`(?is)@import\s+(?:url\(\s*)?["']?([^"')\s;]*)`)

	loadRels = map[string]bool{
		"stylesheet": true, "icon": true, "shortcut": true, "apple-touch-icon": true,
		"preload": true, "modulepreload": true, "prefetch": true, "manifest": true,
	}
)

// IsRemote reports whether ref points at another origin: an http(s) URL or a
// protocol-relative one. Relative paths and data: URLs are local.
func IsRemote(ref string) bool {
	r := strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(r, "http://") || strings.HasPrefix(r, "https://") || strings.HasPrefix(r, "//")
}

// RemoteAssets returns every remote reference html makes to something the
// browser loads, and how many asset references it examined, so a caller can
// refuse a check that looked at nothing.
func RemoteAssets(html string) (remote []string, checked int) {
	note := func(ref string) {
		checked++
		if IsRemote(ref) {
			remote = append(remote, ref)
		}
	}
	for _, m := range tagRe.FindAllStringSubmatch(html, -1) {
		tag := strings.ToLower(m[1])
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(m[2], -1) {
			attrs[strings.ToLower(a[1])] = a[2] + a[3] + a[4]
		}
		switch tag {
		case "link":
			for _, rel := range strings.Fields(strings.ToLower(attrs["rel"])) {
				if loadRels[rel] {
					note(attrs["href"])
					break
				}
			}
		case "object":
			if v, ok := attrs["data"]; ok {
				note(v)
			}
		default:
			if v, ok := attrs["src"]; ok {
				note(v)
			}
		}
		if v, ok := attrs["srcset"]; ok {
			for _, cand := range strings.Split(v, ",") {
				if f := strings.Fields(cand); len(f) > 0 {
					note(f[0])
				}
			}
		}
	}
	for _, m := range styleRe.FindAllStringSubmatch(html, -1) {
		r, c := RemoteCSS(m[1])
		remote, checked = append(remote, r...), checked+c
	}
	// Inline style attributes.
	for _, a := range attrRe.FindAllStringSubmatch(html, -1) {
		if strings.EqualFold(a[1], "style") {
			r, c := RemoteCSS(a[2] + a[3] + a[4])
			remote, checked = append(remote, r...), checked+c
		}
	}
	return remote, checked
}

// RemoteCSS returns every remote url() and @import in css, with the number of
// references examined.
func RemoteCSS(css string) (remote []string, checked int) {
	for _, m := range cssURL.FindAllStringSubmatch(css, -1) {
		checked++
		if ref := m[1] + m[2] + m[3]; IsRemote(ref) {
			remote = append(remote, ref)
		}
	}
	for _, m := range cssImp.FindAllStringSubmatch(css, -1) {
		checked++
		// An @import is a load even when its target is a relative path on
		// another origin's stylesheet, but a local one is allowed here.
		if IsRemote(m[1]) {
			remote = append(remote, m[1])
		}
	}
	return remote, checked
}

// AssertNoRemoteAssets fails t for each remote asset reference in the page
// html, and when the page names no asset at all (a page with no stylesheet is
// not a page this check understands, and would pass vacuously).
func AssertNoRemoteAssets(t *testing.T, label, html string) {
	t.Helper()
	remote, checked := RemoteAssets(html)
	if checked == 0 {
		t.Errorf("%s: no asset references found; the check would pass vacuously", label)
	}
	for _, ref := range remote {
		t.Errorf("%s loads a remote asset %q", label, ref)
	}
}

// AssertNoRemoteCSS fails t for each remote url() or @import in a stylesheet.
func AssertNoRemoteCSS(t *testing.T, label, css string) {
	t.Helper()
	remote, _ := RemoteCSS(css)
	for _, ref := range remote {
		t.Errorf("%s loads a remote asset %q", label, ref)
	}
}
