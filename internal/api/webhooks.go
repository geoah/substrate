package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The bounds on one public webhook delivery. A non-multipart body rides the
// envelope whole, so it is held to what the runner protocol and an agent's
// user message can carry; a multipart request may carry files, which the
// engine spools to the blob store, so its total is bounded below the blob
// store's own cap while its inline parts share the inline budget.
const (
	maxWebhookInline      = 1 << 20
	maxWebhookBody        = 32 << 20
	maxWebhookHeaderBytes = 16 << 10
	// maxConcurrentWebhooks bounds in-flight deliveries process-wide, the
	// PUT /blobs pattern: a burst past it is answered 503 with Retry-After
	// rather than buffered.
	maxConcurrentWebhooks = 8
)

var webhookSem = make(chan struct{}, maxConcurrentWebhooks)

// The headers a delivery never carries into a callable: whatever credential
// the sender used to reach this door is the door's business.
var webhookHeaderDenylist = map[string]bool{
	"authorization": true, "cookie": true, "proxy-authorization": true,
}

// postWebhook is POST /webhooks/{owner}/{trigger}[/{key}]: the public door
// (decision 0045). No bearer; the path names the repository and the trigger,
// and the trigger's own key, when it declares one, is the credential, read
// from the trailing path segment, `?key=` or a bearer header. The request is
// read into transport facts here and handed to the service, which answers
// 404 for every refusal alike and 202 with the fire id once the delivery is
// on its way; the callable's output is never a response.
func (h *handler) postWebhook(w http.ResponseWriter, r *http.Request) {
	rc, ok := h.svc.(substrate.WebhookReceiver)
	if !ok {
		writeUnsupported(w, "this substrate receives no webhooks")
		return
	}
	select {
	case webhookSem <- struct{}{}:
		defer func() { <-webhookSem }()
	default:
		writeUnavailable(w, time.Second, "webhook door is busy, retry")
		return
	}
	req, status, err := readWebhookRequest(w, r)
	if err != nil {
		writeError(w, status, codeBadRequest, err.Error())
		return
	}
	fid, err := rc.ReceiveWebhook(r.Context(), pathParam(r, "owner"), pathParam(r, "trigger"), webhookKey(r), req)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			writeError(w, http.StatusNotFound, codeNotFound, "no such webhook")
			return
		}
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"fire": fid})
}

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
// for a body past its bound, 400 for multipart the reader cannot parse.
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
			return req, http.StatusRequestEntityTooLarge, errors.New("webhook body exceeds 1 MiB; a larger payload must arrive as multipart/form-data file parts")
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
				return nil, http.StatusRequestEntityTooLarge, errors.New("webhook inline parts exceed 1 MiB")
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
