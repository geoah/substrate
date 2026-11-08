package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/egress"
)

// imagesRoute is the image proxy (docs/plans/applets.md): an applet draws the
// images its records name (a book's cover) wherever they are hosted, and the
// console fetches each through here with its token rather than from the
// browser, so the image's host never learns who is looking and the console's
// img-src stays its own origin. The fetch goes out through egress's confined
// client, public addresses alone, checked at every connect, a redirect
// included.
//
// It hides the viewer, and nothing more: a URL an applet builds from a record's
// values still carries them to the host it names, so an image host policy is
// owed before anyone but the owner and their assistant writes an applet.
const imagesRoute = "/images"

const (
	// imageMaxBytes bounds one image: a cover is tens of kilobytes, and a
	// proxy must not be a way to make the server download anything big.
	imageMaxBytes = 8 << 20
	// imageMaxRedirects is how many redirects a fetch follows: image hosts
	// redirect to their storage (Open Library's covers to the Internet
	// Archive's), and every hop is held to the confined transport.
	imageMaxRedirects = 3
	// imageCacheSeconds is how long the browser keeps one, privately.
	imageCacheSeconds = 86400
	// imageSlots is how many images the server fetches at once, each held
	// in memory up to imageMaxBytes: a screen of covers queues behind it
	// rather than costing a buffer apiece.
	imageSlots = 8
)

// imageTypes are the media types the proxy answers. SVG is not one: it is a
// document that runs script when opened on its own, and a proxied image is
// served from the console's origin.
var imageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif"}

// imageClient is the confined client with a bounded, HTTPS-only redirect
// policy.
func imageClient() *http.Client {
	c := egress.ConfinedClient()
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > imageMaxRedirects {
			return fmt.Errorf("more than %d redirects", imageMaxRedirects)
		}
		if req.URL.Scheme != "https" {
			return errors.New("a redirect left https")
		}
		return nil
	}
	return c
}

// getImage answers the image an https address names. Every way the address
// fails to give one (unreachable, refused by the confined transport, not 200,
// not an image the proxy serves, too large) is a 404 whose message says
// which: the console draws nothing either way, and the closed error set has
// no gateway code.
func (h *handler) getImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "url is an https:// address of an image")
		return
	}
	// G704: fetching the address the caller names is the proxy's job; the
	// confined client is what keeps it to public hosts.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil) //nolint:gosec // G704: see above
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "url is an https:// address of an image")
		return
	}
	select {
	case h.imageSlots <- struct{}{}:
		defer func() { <-h.imageSlots }()
	case <-r.Context().Done():
		return
	}
	req.Header.Set("Accept", strings.Join(imageTypes, ", "))
	req.Header.Set("User-Agent", "substrate-image-proxy")
	res, err := h.images.Do(req) //nolint:gosec // G704: the confined client dials public addresses alone
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "the image could not be fetched: "+err.Error())
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusNotFound, codeNotFound, fmt.Sprintf("the image's host answered %d", res.StatusCode))
		return
	}
	mediaType, _, _ := strings.Cut(res.Header.Get("Content-Type"), ";")
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if !slices.Contains(imageTypes, mediaType) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address is not an image this proxy serves: "+mediaType)
		return
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, imageMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, "the image could not be read: "+err.Error())
		return
	}
	if len(body) > imageMaxBytes {
		writeError(w, http.StatusNotFound, codeNotFound, "the image is larger than the proxy serves")
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", mediaType)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("Cache-Control", fmt.Sprintf("private, max-age=%d", imageCacheSeconds))
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
