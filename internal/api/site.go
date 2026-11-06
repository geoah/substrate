package api

import (
	"bytes"
	"html"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
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
	h.serveSiteFile(w, r, strings.TrimPrefix(rel, "/"))
}

// serveTabIcon answers /favicon.ico with the site's tab icon, under the
// headers and the type its own /site/ URL carries.
func (h *handler) serveTabIcon(w http.ResponseWriter, r *http.Request) {
	href, _ := h.site.TabIcon()
	rel, err := url.PathUnescape(strings.TrimPrefix(href, site.URLPrefix))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.serveSiteFile(w, r, rel)
}

// serveSiteFile serves the site file at the slash-separated path rel.
func (h *handler) serveSiteFile(w http.ResponseWriter, r *http.Request, rel string) {
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

var (
	shellIcon  = regexp.MustCompile(`(?i)<link\s[^>]*\brel=["']?icon["']?[^>]*>`)
	shellTitle = regexp.MustCompile(`(?is)<title>.*?</title>`)
)

// brandShell returns the rewrite the console's index.html takes on a server
// with a site: its tab icon becomes the site's and its title the site's name.
// The console makes the same change once discovery answers, but a tab draws
// the icon the HTML names before any script runs, and a bookmark or a link
// preview never runs one.
func brandShell(s *site.Site) func([]byte) []byte {
	title := []byte("<title>" + html.EscapeString(s.Info().Name) + "</title>")
	var icon []byte
	if href, typ := s.TabIcon(); href != "" {
		tag := `<link rel="icon"`
		if typ != "" {
			tag += ` type="` + html.EscapeString(typ) + `"`
		}
		icon = []byte(tag + ` href="` + html.EscapeString(href) + `" />`)
	}
	return func(page []byte) []byte {
		page = replaceFirst(shellTitle, page, title)
		if icon == nil {
			return page
		}
		if shellIcon.Match(page) {
			return replaceFirst(shellIcon, page, icon)
		}
		if i := bytes.Index(bytes.ToLower(page), []byte("</head>")); i >= 0 {
			return slices.Concat(page[:i], icon, page[i:])
		}
		return page
	}
}

// replaceFirst replaces re's first match in b with the literal repl.
func replaceFirst(re *regexp.Regexp, b, repl []byte) []byte {
	loc := re.FindIndex(b)
	if loc == nil {
		return b
	}
	return slices.Concat(b[:loc[0]], repl, b[loc[1]:])
}
