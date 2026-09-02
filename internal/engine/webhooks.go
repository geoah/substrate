package engine

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"time"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
)

// actorWebhook is the public webhook door's writer identity: the blobs a
// multipart delivery spools before its fire are stored under it, so a
// manifest's createdBy names the hand that put the bytes there. It shares the
// `substrate.` namespace with actorOAuth, which no request may claim.
const actorWebhook substrate.Actor = "substrate.webhook"

// webhookFirePrefix distinguishes a public delivery's fire id from an
// authenticated wake's ("wake-").
const webhookFirePrefix = "hook-"

// errWebhookRefused is every refusal the door must not explain: no such
// repository, no such trigger, a trigger of another source, a disabled one, an
// unresolvable callable, a wrong or missing key. One answer, so the endpoint
// space cannot be probed.
var errWebhookRefused = fmt.Errorf("%w: no such webhook", substrate.ErrNotFound)

// webhookPath is a webhook trigger's endpoint, relative to the server root.
func webhookPath(owner, triggerID string) string {
	return "/webhooks/" + owner + "/" + triggerID
}

// ReceiveWebhook is the public door (substrate.WebhookReceiver): resolve the
// repository by its owner, the trigger by id, check the key when the trigger
// declares one, spool the file parts into the blob store, then hand the fire
// to the background supervisor and return. The sender gets its answer in
// milliseconds while an agent callable may run for minutes, and a fire that
// has STARTED is durable through deliverFire's park.
func (s *service) ReceiveWebhook(ctx context.Context, owner, triggerID, key string, req substrate.WebhookRequest) (string, error) {
	return s.receiveWebhook(ctx, owner, triggerID, key, req, false)
}

// receiveWebhook is ReceiveWebhook with the fire either detached (the door)
// or run inline (tests asserting on what the delivery wrote).
func (s *service) receiveWebhook(ctx context.Context, owner, triggerID, key string, req substrate.WebhookRequest, inline bool) (string, error) {
	dsAny, err := s.Dataset(ctx, owner)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrAuth) {
			return "", errWebhookRefused
		}
		return "", err
	}
	ds := dsAny.(*dataset)
	tr, fid, at, envelope, err := ds.admitWebhook(ctx, triggerID, key, req)
	if err != nil {
		return "", err
	}
	if inline {
		ds.fireWebhook(ctx, tr, fid, at, envelope)
		return fid, nil
	}
	if !ds.spawn("webhook fire", func(ctx context.Context) {
		ds.fireWebhook(ctx, tr, fid, at, envelope)
	}) {
		return "", errors.New("substrate: webhook refused, the service is shutting down")
	}
	return fid, nil
}

// admitWebhook is the synchronous half of a delivery: the checks, the blob
// spool and the envelope. Every check that fails answers errWebhookRefused.
func (ds *dataset) admitWebhook(ctx context.Context, triggerID, key string, req substrate.WebhookRequest) (*trigger, string, time.Time, map[string]any, error) {
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrValidation) {
			return nil, "", time.Time{}, nil, errWebhookRefused
		}
		return nil, "", time.Time{}, nil, err
	}
	if !tr.Webhook || !tr.Enabled || !tr.runnable() {
		return nil, "", time.Time{}, nil, errWebhookRefused
	}
	if tr.WebhookKey != "" && subtle.ConstantTimeCompare([]byte(tr.WebhookKey), []byte(key)) != 1 {
		return nil, "", time.Time{}, nil, errWebhookRefused
	}
	parts, err := ds.spoolWebhookParts(ctx, req.Parts)
	if err != nil {
		return nil, "", time.Time{}, nil, err
	}
	wid, err := newID()
	if err != nil {
		return nil, "", time.Time{}, nil, err
	}
	fid := webhookFirePrefix + wid
	at := nowUTC()
	envelope := runner.FireEnvelope(fid, at, ds.Repository().Name)
	envelope["request"] = webhookRequestEnvelope(req, parts)
	return tr, fid, at, envelope, nil
}

// fireWebhook is the detached half: one deliverFire, mode webhook, with the
// built envelope. Failures park inside deliverFire; what reaches here is the
// infrastructure kind, logged because nobody is left to answer.
func (ds *dataset) fireWebhook(ctx context.Context, tr *trigger, fid string, at time.Time, envelope map[string]any) {
	if _, err := ds.deliverFire(ctx, tr, runner.ModeWebhook, fid, at, nil, envelope); err != nil {
		ds.svc.log.Warn("substrate: webhook fire failed",
			"repository", ds.Repository().Name, "trigger", tr.ID, "fire", fid, "error", err)
	}
}

// spoolWebhookParts stores every file part content-addressed and returns the
// parts as the envelope carries them: inline fields as {name, value}, files
// as {name, filename, mediaType, size, blob}. The bytes never enter the
// envelope, which the runner protocol and an agent's user message both
// bound; the digest does, and dedup by digest makes a sender's retry a no-op
// on the store.
func (ds *dataset) spoolWebhookParts(ctx context.Context, parts []substrate.WebhookPart) ([]map[string]any, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		if p.Data == nil {
			out = append(out, map[string]any{"name": p.Name, "value": p.Value})
			continue
		}
		info, err := ds.PutBlob(ctx, actorWebhook,
			substrate.BlobUpload{Name: blobNameOf(p.Filename), MediaType: p.MediaType}, p.Data, "")
		if err != nil {
			return nil, fmt.Errorf("substrate: webhook part %q: %w", p.Name, err)
		}
		part := map[string]any{
			"name": p.Name, "filename": p.Filename, "mediaType": p.MediaType,
			"size": info.Size, "blob": info.Digest,
		}
		out = append(out, part)
	}
	return out, nil
}

// blobNameOf reduces a sender's filename to the display name a manifest
// takes: the base name, or nothing when there is none. A name is descriptive
// and optional, so a filename the store would refuse costs the name, never
// the delivery.
func blobNameOf(filename string) string {
	base := path.Base(filename)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if _, err := checkBlobName(base); err != nil {
		return ""
	}
	return base
}

// webhookRequestEnvelope is the `request` the callable reads: method, media
// type, the filtered headers, the query, and either the raw body (text when
// it is valid UTF-8, else base64, both byte-exact so a signature over the
// body verifies) or the parts of a multipart request.
func webhookRequestEnvelope(req substrate.WebhookRequest, parts []map[string]any) map[string]any {
	headers := make(map[string]any, len(req.Headers))
	for k, v := range req.Headers {
		headers[k] = v
	}
	query := make(map[string]any, len(req.Query))
	for k, vs := range req.Query {
		vals := make([]any, len(vs))
		for i, v := range vs {
			vals[i] = v
		}
		query[k] = vals
	}
	out := map[string]any{
		"method":      req.Method,
		"contentType": req.ContentType,
		"headers":     headers,
		"query":       query,
	}
	switch {
	case parts != nil:
		list := make([]any, len(parts))
		for i, p := range parts {
			list[i] = p
		}
		out["parts"] = list
	case req.Body != nil:
		if utf8.Valid(req.Body) {
			out["body"] = map[string]any{"text": string(req.Body)}
		} else {
			out["body"] = map[string]any{"base64": base64.StdEncoding.EncodeToString(req.Body)}
		}
	}
	return out
}
