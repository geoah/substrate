package api

import (
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/site"
)

// sitePolicy replaces the console's Content-Security-Policy on a site file.
// A site file is the deployer's, not the console's: the console fetches the
// landing fragment and renders it under its own policy, so a file opened at
// its own URL needs no script at all. `sandbox` gives a page opened there an
// opaque origin, so nothing in it reaches the console's localStorage, where
// the bearer token sits.
const sitePolicy = "sandbox; default-src 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
	"font-src 'self' data:; frame-ancestors 'none'"

// cacheRevalidate is for a site file: a deployer edits one in place without
// a restart, so every use asks again, and the ETag lets an unchanged file
// answer 304. The ETag carries the modification time to the nanosecond, so a
// file replaced within the same second is not answered 304 the way a bare
// Last-Modified would answer it.
const cacheRevalidate = "no-cache"

// serveSite serves GET and HEAD of a file in the site directory
// (SUBSTRATE_SITE_DIR) at /site/<path>. Everything else under /site/ is a
// 404: a directory, a dotfile, a path with `..` or an empty segment, a
// symlink out of the directory, a missing file and any other method. Mounted
// only where a site is configured; without one /site/ is the console's like
// any other path.
func (h *handler) serveSite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	rel := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(site.URLPrefix, "/"))
	rel = strings.TrimPrefix(rel, "/")
	f, info, err := h.site.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", sitePolicy)
	hdr.Set("Cache-Control", cacheRevalidate)
	hdr.Set("ETag", `"`+strconv.FormatInt(info.Size(), 36)+"-"+strconv.FormatInt(info.ModTime().UnixNano(), 36)+`"`)
	// The type comes from the extension alone. ServeContent would sniff the
	// body for a file without one, and a sniffed text/html is a page served
	// from this origin that its deployer never named as one.
	ctype := mime.TypeByExtension(path.Ext(rel))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	hdr.Set("Content-Type", ctype)
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
