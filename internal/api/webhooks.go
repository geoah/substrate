package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// The bounds on one public webhook delivery. A non-multipart body is held in
// memory whole and rides the envelope whole into the callable, so it is held
// to the trigger's inline bound: substrate.WebhookInlineDefault, 1 MiB, unless
// the trigger declares `source.webhook.maxBody`, up to
// substrate.WebhookInlineMax (a day of minute-grouped Health Auto Export
// samples passes 1 MiB). The door learns the bound from CheckWebhook before
// it reads a byte, so a sender that names no trigger, or a keyed trigger
// without its key, never gets the larger one. A multipart request may carry
// files, which the engine spools to the blob store, so its total is bounded
// below the blob store's own cap while its inline parts share the inline
// bound.
const (
	maxWebhookBody        = 32 << 20
	maxWebhookHeaderBytes = 16 << 10
	// maxConcurrentWebhooks bounds in-flight deliveries process-wide, the
	// PUT /blobs pattern: a burst past it is answered 503 with Retry-After
	// rather than buffered.
	maxConcurrentWebhooks = 8
)

// A sender has this long from its headers to the end of its body, so one
// that stalls mid-body cannot hold a delivery slot. The inline deadline is
// per started MiB of the trigger's bound (30 s for the 1 MiB default, 4 min
// for 8 MiB), so a larger bound keeps the rate the default asks for and a
// trigger that declares none holds a slot no longer than before.
var (
	webhookInlineDeadline    = 30 * time.Second
	webhookMultipartDeadline = 2 * time.Minute
)

// inlineDeadline is the inline deadline for a trigger's inline bound.
func inlineDeadline(maxBody int64) time.Duration {
	mib := (maxBody + 1<<20 - 1) >> 20
	if mib < 1 {
		mib = 1
	}
	return time.Duration(mib) * webhookInlineDeadline
}

// sizeText names a byte bound the way a 413 says it: whole MiB where it is
// one, bytes otherwise.
func sizeText(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

var webhookSem = make(chan struct{}, maxConcurrentWebhooks)

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
// from the trailing path segment, `?key=` or a bearer header; an app's token
// as the bearer stands in for it under `webhook:` (webhookGranted). The
// request is read into transport facts here and handed to the service, which
// answers 404 for every refusal alike, 401 for a request that carried a keyed
// trigger's key and that the trigger's declared signature refuses (decision
// 0164), and 202 with the fire id once the delivery is on its way; the
// callable's output is never a response.
//
// The service's checks run before the door takes a slot or reads a byte, so
// a request for no such webhook holds neither: eight of them with bodies that
// never finish would otherwise hold every slot and close the door for every
// repository.
func (h *handler) postWebhook(w http.ResponseWriter, r *http.Request) {
	// The deadline runs from the start and stays set on every refusal, so
	// net/http's drain of a refused request's unread body ends with it too.
	start := time.Now()
	multipartBody := false
	deadline := webhookInlineDeadline
	if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType == "multipart/form-data" {
		multipartBody = true
		deadline = webhookMultipartDeadline
	}
	clear := bodyDeadline(w, deadline)
	authority, trigger, key := pathParam(r, "authority"), pathParam(r, "trigger"), webhookKey(r)
	ctx := r.Context()
	granted, bearer := h.webhookGranted(r, authority, trigger, key), key
	if granted {
		ctx = substrate.WithWebhookGrant(ctx, authority, trigger)
		key = ""
	}
	target, err := h.svc.CheckWebhook(ctx, authority, trigger, key)
	if err != nil {
		// net/http reads the rest of a body before it answers, unless the
		// answer closes the connection: a refusal reads none of it.
		w.Header().Set("Connection", "close")
		refuseUnread(w, r, err)
		return
	}
	// A signed trigger refuses a multipart request whatever it carries (the
	// door keeps no raw bytes to check a MAC over), so the body is not read.
	if target.SignatureOnly && multipartBody {
		w.Header().Set("Connection", "close")
		refuseUnread(w, r, substrate.ErrNotFound)
		return
	}
	maxBody := target.MaxBody
	if maxBody <= 0 {
		maxBody = substrate.WebhookInlineDefault
	}
	if !multipartBody {
		if d := inlineDeadline(maxBody); d > deadline {
			clear = bodyDeadline(w, d-time.Since(start))
		}
	}
	select {
	case webhookSem <- struct{}{}:
		defer func() { <-webhookSem }()
	default:
		w.Header().Set("Connection", "close")
		writeUnavailable(w, time.Second, "webhook door is busy, retry")
		return
	}
	req, status, err := readWebhookRequest(w, r, maxBody)
	if err != nil {
		if target.SignatureOnly {
			w.Header().Set("Connection", "close")
			writeWebhookError(w, r, substrate.ErrNotFound)
			return
		}
		writeError(w, status, codeBadRequest, err.Error())
		return
	}
	clear()
	// The body may take minutes: a token revoked or lapsed meanwhile no
	// longer delivers, and is refused as a wrong key is.
	if granted && !h.webhookGranted(r, authority, trigger, bearer) {
		writeWebhookError(w, r, substrate.ErrNotFound)
		return
	}
	fid, err := h.svc.ReceiveWebhook(ctx, authority, trigger, key, req)
	if err != nil {
		writeWebhookError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, substrate.WebhookAccepted{Fire: fid})
}

// refuseUnread answers a refusal the door makes before it takes a slot. While
// every slot is held, a 404 here answers as the busy door does: a keyless
// signed trigger reaches the slot and gets the busy 503, so a missing one
// answering 404 at the same moment would tell the two apart.
func refuseUnread(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, substrate.ErrNotFound) && len(webhookSem) == cap(webhookSem) {
		writeUnavailable(w, time.Second, "webhook door is busy, retry")
		return
	}
	writeWebhookError(w, r, err)
}

// writeWebhookError answers a service refusal as the door's one 404, a
// declared signature's refusal as a terse 401, and anything else as the
// error it is. Every 404 closes the connection, the one the door makes before
// it reads the body and the one a keyless signed trigger's signature check
// makes after it, so neither the headers nor the connection's reuse tell a
// missing trigger from a signed one.
func writeWebhookError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, substrate.ErrNotFound) {
		w.Header().Set("Connection", "close")
		writeError(w, http.StatusNotFound, codeNotFound, "no such webhook")
		return
	}
	// The trigger's declared signature refused the request (decision 0164).
	// The reason stays in the server: the sender is unauthenticated, and
	// which check failed helps a forger.
	if errors.Is(err, substrate.ErrAuth) {
		writeError(w, http.StatusUnauthorized, codeAuth, "webhook signature refused")
		return
	}
	writeSubstrateError(w, r, err)
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

// webhookGranted reports whether the bearer, the request's only credential,
// is an app's token that delivers to this trigger in this repository
// (`webhook:`, docs/plans/app-setup.md), which the engine then takes in place
// of the trigger's key. Anything short of that, a revoked or lapsed token, a
// full-access one, one of another repository or one without the scope, stays
// the key the bearer always was, so it is refused exactly as a wrong key is
// and the answer tells a sender nothing about the token.
func (h *handler) webhookGranted(r *http.Request, authority, trigger, key string) bool {
	secret := bearerSecret(r)
	if secret == "" || secret != key || !strings.HasPrefix(secret, substrate.TokenSecretPrefix) {
		return false
	}
	ds, tok, err := h.svc.Authenticate(r.Context(), secret)
	if err != nil || !tok.Scoped() {
		return false
	}
	if exp := tok.ExpiresAt; exp != nil && h.now().After(*exp) {
		return false
	}
	grant, err := scope.NewGrant(tok.Scopes)
	return err == nil && grant.Delivers(trigger) && ds.Repository().Authority == authority
}

// readWebhookRequest turns the HTTP request into the seam's transport facts,
// or names the HTTP status that refuses it: 431 for headers past the cap, 413
// for a body past its bound (maxInline for a non-multipart body and for the
// inline parts of a multipart one), 400 for multipart the reader cannot parse
// or a body that did not arrive within its deadline.
func readWebhookRequest(w http.ResponseWriter, r *http.Request, maxInline int64) (substrate.WebhookRequest, int, error) {
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
		parts, status, err := readWebhookParts(w, r, params["boundary"], maxInline)
		if err != nil {
			return req, status, err
		}
		req.Parts = parts
		return req, 0, nil
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxInline))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return req, http.StatusRequestEntityTooLarge, fmt.Errorf("webhook body exceeds %s; a larger payload must arrive as multipart/form-data file parts", sizeText(maxInline))
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
func readWebhookParts(w http.ResponseWriter, r *http.Request, boundary string, maxInline int64) ([]substrate.WebhookPart, int, error) {
	if boundary == "" {
		return nil, http.StatusBadRequest, errors.New("multipart/form-data without a boundary")
	}
	body := http.MaxBytesReader(w, r.Body, maxWebhookBody)
	mr := multipart.NewReader(body, boundary)
	var parts []substrate.WebhookPart
	var inline int64
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
			inline += int64(len(data))
			if inline > maxInline {
				return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("webhook inline parts exceed %s", sizeText(maxInline))
			}
			part.Value = string(data)
		}
		parts = append(parts, part)
	}
	// The closing boundary need not end the body. Whatever follows it is read
	// here, under the deadline, rather than left to net/http's drain before
	// the answer, which has none once the caller clears it.
	if _, err := io.Copy(io.Discard, body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, http.StatusRequestEntityTooLarge, errors.New("webhook multipart body exceeds 32 MiB")
		}
		return nil, http.StatusBadRequest, errors.New("webhook multipart body unreadable")
	}
	if parts == nil {
		parts = []substrate.WebhookPart{}
	}
	return parts, 0, nil
}
