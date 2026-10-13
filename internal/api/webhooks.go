package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The bounds on one public webhook delivery. A non-multipart body is held in
// memory whole and rides the envelope whole into the callable, so it is held
// to 8 MiB: a slot's worth of memory times maxConcurrentWebhooks, and enough
// for a day of minute-grouped Health Auto Export samples. The ledger keeps
// the body in the blob store by digest. An agent callable reads the body in
// its user message, which becomes an llm/message record and a model turn, so
// the engine holds a body bound for an agent to 1 MiB (ErrTooLarge, 413). A
// multipart request may carry files, which the engine spools to the blob
// store, so its total is bounded below the blob store's own cap while its
// inline parts share the inline budget.
const (
	maxWebhookInline      = 8 << 20
	maxWebhookBody        = 32 << 20
	maxWebhookHeaderBytes = 16 << 10
	// maxConcurrentWebhooks bounds in-flight deliveries process-wide, the
	// PUT /blobs pattern: a burst past it is answered 503 with Retry-After
	// rather than buffered.
	maxConcurrentWebhooks = 8
)

var webhookSem = make(chan struct{}, maxConcurrentWebhooks)

// webhookReadTimeout bounds how long a delivery holding a slot may take to
// send a body held to the inline cap. The server sets no whole-request read
// deadline, so without this a sender trickling bytes holds its slot for as
// long as it likes, and eight of them close the door to every repository.
// Two minutes carries the 8 MiB inline cap at about 560 kbit/s.
var webhookReadTimeout = 2 * time.Minute

// webhookBodyTimeout is the read deadline for r's body: webhookReadTimeout,
// and for a multipart request the same time per byte of its larger bound
// (eight minutes for 32 MiB), so a sender at the rate the inline cap needs
// meets either deadline.
func webhookBodyTimeout(r *http.Request) time.Duration {
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType == "multipart/form-data" {
		return webhookReadTimeout * (maxWebhookBody / maxWebhookInline)
	}
	return webhookReadTimeout
}

// The headers a delivery never carries into a callable: whatever credential
// the sender used to reach this door is the door's business. `set-cookie` is
// a response header, listed so a request that carries one anyway is treated
// the same way; the engine refuses a trigger that declares any of the four
// (decision 0097).
var webhookHeaderDenylist = map[string]bool{
	"authorization": true, "cookie": true, "proxy-authorization": true, "set-cookie": true,
}

// postWebhook is POST /webhooks/{authority}/{trigger}[/{key}]: the public door
// (decision 0045). No bearer; the path names the repository and the trigger,
// and the trigger's own key, when it declares one, is the credential, read
// from the trailing path segment, `?key=` or a bearer header. The request is
// read into transport facts here and handed to the service, which answers
// 404 for every refusal alike, 401 for a request that carried a keyed
// trigger's key and that the trigger's declared signature refuses, and 202
// with the fire id once the delivery is on its way; the callable's output is
// never a response.
func (h *handler) postWebhook(w http.ResponseWriter, r *http.Request) {
	select {
	case webhookSem <- struct{}{}:
		defer func() { <-webhookSem }()
	default:
		writeUnavailable(w, time.Second, "webhook door is busy, retry")
		return
	}
	// The deadline covers the body alone: the server clears it when the body
	// reaches EOF, so it never cancels the service's work below. A refused
	// body keeps it, which bounds the server's read of what is left. A
	// writer that cannot take a deadline (a test's recorder) has no
	// connection to hold.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(webhookBodyTimeout(r)))
	req, status, err := readWebhookRequest(w, r)
	if err != nil {
		writeError(w, status, codeBadRequest, err.Error())
		return
	}
	fid, err := h.svc.ReceiveWebhook(r.Context(), pathParam(r, "authority"), pathParam(r, "trigger"), webhookKey(r), req)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "no such webhook")
			return
		}
		// The trigger's declared signature refused the request (decision
		// 0164). The reason stays in the server: the sender is
		// unauthenticated, and which check failed helps a forger.
		if errors.Is(err, substrate.ErrAuth) {
			writeError(w, http.StatusUnauthorized, codeAuth, "webhook signature refused")
			return
		}
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, substrate.WebhookAccepted{Fire: fid})
}

// errWebhookReadTimeout is the 408 a delivery gets when its body does not
// arrive inside webhookBodyTimeout.
var errWebhookReadTimeout = errors.New("webhook body not received in time")

// webhookKey reads the credential from wherever the sender could put it: the
// trailing path segment, the `key` query parameter, or a bearer header. A
// sender that cannot set headers (a provider's webhook form) uses the URL;
// one that can keeps the URL clean.
func webhookKey(r *http.Request) string {
	if k := pathParam(r, "key"); k != "" {
		return k
	}
	if k := r.URL.Query().Get("key"); k != "" {
		return k
	}
	if auth := r.Header.Get("Authorization"); len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

// readWebhookRequest turns the HTTP request into the seam's transport facts,
// or names the HTTP status that refuses it: 431 for headers past the cap, 413
// for a body past its bound, 408 for a body not received inside
// webhookBodyTimeout, 400 for multipart the reader cannot parse.
func readWebhookRequest(w http.ResponseWriter, r *http.Request) (substrate.WebhookRequest, int, error) {
	req := substrate.WebhookRequest{Method: r.Method, Headers: map[string]string{}, Query: map[string][]string{}}
	size := 0
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if webhookHeaderDenylist[lower] {
			continue
		}
		joined := strings.Join(values, ", ")
		size += len(lower) + len(joined)
		if size > maxWebhookHeaderBytes {
			return req, http.StatusRequestHeaderFieldsTooLarge, errors.New("webhook headers exceed 16 KiB")
		}
		req.Headers[lower] = joined
	}
	for name, values := range r.URL.Query() {
		if name == "key" {
			continue
		}
		req.Query[name] = values
	}
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	req.ContentType = mediaType
	if mediaType == "multipart/form-data" {
		parts, status, err := readWebhookParts(w, r, params["boundary"])
		if err != nil {
			return req, status, err
		}
		req.Parts = parts
		return req, 0, nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookInline))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return req, http.StatusRequestEntityTooLarge, errors.New("webhook body exceeds 8 MiB; a larger payload must arrive as multipart/form-data file parts")
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return req, http.StatusRequestTimeout, errWebhookReadTimeout
		}
		return req, http.StatusBadRequest, errors.New("webhook body unreadable")
	}
	if len(body) > 0 {
		req.Body = body
	}
	return req, 0, nil
}

// readWebhookParts walks a multipart body. A part is a FILE when it declares a
// filename or a media type outside text/*: a ring's `audio` part arrives as
// audio/mp4 with no filename, and it is still bytes for the blob store, not a
// value for the envelope. Inline parts share the inline budget; the request
// as a whole is held to the multipart bound.
func readWebhookParts(w http.ResponseWriter, r *http.Request, boundary string) ([]substrate.WebhookPart, int, error) {
	if boundary == "" {
		return nil, http.StatusBadRequest, errors.New("multipart/form-data without a boundary")
	}
	mr := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxWebhookBody), boundary)
	var parts []substrate.WebhookPart
	inline := 0
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return nil, http.StatusRequestEntityTooLarge, errors.New("webhook multipart body exceeds 32 MiB")
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, http.StatusRequestTimeout, errWebhookReadTimeout
			}
			return nil, http.StatusBadRequest, errors.New("webhook multipart body unreadable")
		}
		partType, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		isFile := p.FileName() != "" || (partType != "" && !strings.HasPrefix(partType, "text/"))
		data, err := io.ReadAll(p)
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return nil, http.StatusRequestEntityTooLarge, errors.New("webhook multipart body exceeds 32 MiB")
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, http.StatusRequestTimeout, errWebhookReadTimeout
			}
			return nil, http.StatusBadRequest, errors.New("webhook multipart body unreadable")
		}
		part := substrate.WebhookPart{Name: p.FormName(), Filename: p.FileName(), MediaType: partType}
		if isFile {
			if data == nil {
				data = []byte{}
			}
			part.Data = data
		} else {
			inline += len(data)
			if inline > maxWebhookInline {
				return nil, http.StatusRequestEntityTooLarge, errors.New("webhook inline parts exceed 8 MiB")
			}
			part.Value = string(data)
		}
		parts = append(parts, part)
	}
	if parts == nil {
		parts = []substrate.WebhookPart{}
	}
	return parts, 0, nil
}
