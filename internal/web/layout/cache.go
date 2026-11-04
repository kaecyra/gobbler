package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

// AssetETag is the entity tag of an embedded asset: a hash of its bytes, so it
// changes exactly when a new build changes the file.
func AssetETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// SetAssetCache sets the caching headers of an embedded asset. The browser may
// keep a copy but asks again on each use, and the unchanged file costs a 304.
// Asset URLs carry no build stamp, so a max-age let a browser keep the previous
// release's stylesheet against the new release's pages for up to an hour.
// Serve the body with http.ServeContent or an http.FileServer after this call:
// both answer If-None-Match against the ETag set here.
func SetAssetCache(h http.Header, etag string) {
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", etag)
}
