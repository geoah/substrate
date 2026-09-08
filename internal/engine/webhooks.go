package engine

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
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
// The repository is named by its authority: the authority is what the
// repository publishes under, and the username stays the login identifier.
func webhookPath(authority, triggerID string) string {
	return "/webhooks/" + authority + "/" + triggerID
}

// pendingWebhookError is the error an ADMITTED webhook request carries in
// trigger_failures from the door's 202 until its fire settles. The request is
// recorded as a parked failure whose retry is the fire itself, so one row,
// one blob hold (blobs.go parkedBlobsSQL) and one set of ledger effects cover
// an accepted delivery, a running one and a parked one, and a rebuild or a
// restore reproduces all three. A process that stops while a row still
// carries it, before the fire or mid-fire before its effects committed,
// leaves it for the trigger dispatcher's next pass over the repository
// (resumeWebhooks), which runs the fire under the same id. An agent
// fire rewrites it to inFlightError at its claim (settlement.claim), and a
// row left that way waits for a hand, as every interrupted agent delivery
// does (decision 0064). Decision 0068 records the design.
const pendingWebhookError = "delivery accepted: the fire has not settled, and a restart resumes it"

// webhookFire says what receiveWebhook does with an admitted request: hand
// the fire to the background supervisor (the door), run it inline (tests
// asserting on what the delivery wrote) or leave it pending (tests standing
// in for a process that stopped right after the 202).
type webhookFire int

const (
	webhookFireDetached webhookFire = iota
	webhookFireInline
	webhookFireHeld
)

// ReceiveWebhook is the public door (substrate.WebhookReceiver): resolve the
// repository by its authority, the trigger by id, check the key when the
// trigger declares one, spool the file parts and the body into the blob
// store, record the request as a pending delivery in the ledger, then hand
// the fire to the background supervisor and return. The sender gets its
// answer in milliseconds while an agent callable may run for minutes, and
// the answer is given only once the request is durable: a process that stops
// after it leaves the fire to the dispatcher's next pass.
func (s *service) ReceiveWebhook(ctx context.Context, authority, triggerID, key string, req substrate.WebhookRequest) (string, error) {
	return s.receiveWebhook(ctx, authority, triggerID, key, req, webhookFireDetached)
}

// receiveWebhook is ReceiveWebhook with the fire detached, inline or held.
func (s *service) receiveWebhook(ctx context.Context, authority, triggerID, key string, req substrate.WebhookRequest, fire webhookFire) (string, error) {
	repo, err := s.repositoryByAuthority(ctx, authority)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			return "", errWebhookRefused
		}
		return "", err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		// The authority resolved but the repository will not open (a failed
		// upgrade, a quarantined vocabulary, a storage fault). The caller is
		// unauthenticated, so the answer stays the door's one refusal: a 500
		// here would tell a prober that this authority exists. The operator
		// gets the reason instead.
		s.log.Error("substrate: webhook door could not open the repository",
			"authority", logSafeID(authority), "error", err)
		return "", errWebhookRefused
	}
	tr, row, err := ds.admitWebhook(ctx, triggerID, key, req)
	if err != nil {
		return "", err
	}
	switch fire {
	case webhookFireInline:
		ds.fireWebhook(ctx, tr, row)
	case webhookFireDetached:
		// The request is recorded and answered whether or not the supervisor
		// takes the fire: one it refuses (shutdown has begun) stays pending
		// and the next dispatcher pass runs it, so the sender is not asked to
		// redeliver a request the substrate already holds.
		if !ds.spawn("webhook fire", func(ctx context.Context) { ds.fireWebhook(ctx, tr, row) }) {
			ds.svc.log.Info("substrate: webhook fire left pending, the service is shutting down",
				"repository", ds.Repository().Name, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID))
		}
	}
	return row.FireID, nil
}

// admitWebhook is the synchronous half of a delivery: the checks, the blob
// spool and the pending entry. Every check that fails answers
// errWebhookRefused. Past the checks the request is written into the ledger
// as a parked failure carrying pendingWebhookError, in its parked form
// (parkedEnvelope: the body spooled by digest, the headers narrowed, the
// query dropped), with the fire id and the receipt time, on a delivery entry
// of its own whose seq is the row's id. The door answers once that
// transaction has committed, which the write path makes durable (decision
// 0062), so a 202 names a request the substrate holds. The row returned is
// what the fire runs and what a restart resumes.
func (ds *dataset) admitWebhook(ctx context.Context, triggerID, key string, req substrate.WebhookRequest) (*trigger, foldFailure, error) {
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrValidation) {
			return nil, foldFailure{}, errWebhookRefused
		}
		return nil, foldFailure{}, err
	}
	if !tr.Webhook || !tr.Enabled || !tr.runnable() {
		return nil, foldFailure{}, errWebhookRefused
	}
	if tr.WebhookKey != "" && subtle.ConstantTimeCompare([]byte(tr.WebhookKey), []byte(key)) != 1 {
		return nil, foldFailure{}, errWebhookRefused
	}
	parts, err := ds.spoolWebhookParts(ctx, req.Parts)
	if err != nil {
		return nil, foldFailure{}, err
	}
	wid, err := newID()
	if err != nil {
		return nil, foldFailure{}, err
	}
	fid := webhookFirePrefix + wid
	at := nowUTC()
	envelope := runner.FireEnvelope(fid, at, ds.Repository().Name, ds.Repository().Authority)
	envelope["request"] = webhookRequestEnvelope(req, parts)
	payload, err := ds.parkedEnvelope(ctx, envelope)
	if err != nil {
		return nil, foldFailure{}, err
	}
	row := foldFailure{FireID: fid, LastError: pendingWebhookError, ParkedAt: at, Payload: payload}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		id, err := t.reserveSeq()
		if err != nil {
			return err
		}
		row.ID = foldInt(id)
		if err := t.parkTx(tr.ID, row); err != nil {
			return err
		}
		return t.appendDeliveryAt(tr.ID, id)
	})
	if err != nil {
		return nil, foldFailure{}, fmt.Errorf("substrate: webhook %s: record the request: %w", tr.ID, err)
	}
	return tr, row, nil
}

// fireWebhook is the detached half: the pending entry's fire, one
// deliverFire in mode webhook that holds the row in runningClaims from before
// anything runs, retires it in the transaction that commits its effects, or
// rewrites it as parked. The envelope is read back from the row, so the first
// fire and a resumed one run the same bytes. The hold is compare-and-swap
// (settlement.acquire), so a resume racing the door's own spawn, or a hand's
// retry, loses it and starts nothing, and a row a hand retired meanwhile
// ends the fire at its first settlement (errFailureRetired). Failures park
// inside deliverFire; what reaches here is the infrastructure kind, logged
// because nobody is left to answer, and a canceled context (shutdown) leaves
// the row pending for the next dispatcher pass. The log names the trigger
// and the minted fire id, both through logSafeID because the row was built
// beside the request's payload, and classifies the error to a fixed word
// (webhookFireOutcome): an error built from the request (a body the callable
// raised on, a header a decoder quoted) is sender-controlled text and never
// enters the log.
func (ds *dataset) fireWebhook(ctx context.Context, tr *trigger, row foldFailure) {
	var envelope map[string]any
	if err := json.Unmarshal(row.Payload, &envelope); err != nil {
		ds.svc.log.Error("substrate: webhook fire cannot read its recorded request, the entry stays pending",
			"repository", ds.Repository().Name, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID), "failure", int64(row.ID))
		return
	}
	_, err := ds.deliverFire(ctx, tr, runner.ModeWebhook, row.FireID, row.ParkedAt, nil, envelope, &row)
	if err == nil {
		return
	}
	outcome := webhookFireOutcome(err)
	attrs := []any{"repository", ds.Repository().Name, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID), "failure", int64(row.ID), "outcome", outcome}
	switch outcome {
	case fireOutcomeRunning, fireOutcomeRetired:
		ds.svc.log.Info("substrate: webhook fire not run, another hand holds or delivered it", attrs...)
	case fireOutcomeCanceled:
		ds.svc.log.Info("substrate: webhook fire interrupted, the entry stays pending", attrs...)
	default:
		ds.svc.log.Warn("substrate: webhook fire failed", attrs...)
	}
}

// The fixed words a webhook fire's error is logged as.
const (
	fireOutcomeRunning     = "running"     // this process already runs the entry
	fireOutcomeRetired     = "retired"     // a hand delivered the entry meanwhile
	fireOutcomeCanceled    = "canceled"    // the context ended (shutdown)
	fireOutcomeUnavailable = "unavailable" // the store refused the write
	fireOutcomeError       = "error"       // everything else
)

// webhookFireOutcome classifies a fire's error to one of the fixed words,
// so a log line carries the class and never the text.
func webhookFireOutcome(err error) string {
	switch {
	case errors.Is(err, substrate.ErrConflict):
		return fireOutcomeRunning
	case errors.Is(err, errFailureRetired):
		return fireOutcomeRetired
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fireOutcomeCanceled
	case errors.Is(err, substrate.ErrUnavailable):
		return fireOutcomeUnavailable
	}
	return fireOutcomeError
}

// resumeWebhooks runs the admitted webhook requests whose fire has not
// settled: the rows carrying pendingWebhookError, oldest first, one after
// another in one detached task per dispatcher pass (ProcessTriggers). It is
// the recovery half of the door, and it runs only where the dispatcher
// runs: a process that stopped after the 202 and before the fire's effects
// committed, or a directory imported into an empty database, leaves the row
// pending, and the server's first pass over the repository runs it under its
// original fire id, while an operator's process (a rebuild, a reset) opens
// the repository and fires nothing. A row whose trigger is disabled or does
// not resolve stays pending, listed under the trigger's parked failures, and
// the pass after the trigger runs again picks it up. A pass that finds the
// previous walk still running starts none. A read-only process appends
// nothing.
func (ds *dataset) resumeWebhooks() {
	if ds.svc.readOnly || !ds.resumingWebhooks.CompareAndSwap(false, true) {
		return
	}
	if !ds.spawn("webhook resume", func(ctx context.Context) {
		defer ds.resumingWebhooks.Store(false)
		ds.runPendingWebhooks(ctx)
	}) {
		ds.resumingWebhooks.Store(false)
	}
}

// runPendingWebhooks is one walk of the pending rows. Each row is read again
// right before its fire, so one a hand retired since the walk began is left
// alone, and one this process is already running (the door's own spawn, a
// hand's retry) is skipped; the fire's own hold (settlement.acquire) settles
// the race the read cannot. The triggers that do not run are named once
// each, with the rows they hold back.
func (ds *dataset) runPendingWebhooks(ctx context.Context) {
	pending, err := ds.pendingWebhooks(ctx, 0)
	if err != nil {
		ds.svc.log.Error("substrate: pending webhook deliveries could not be read",
			"repository", ds.Repository().Name, "error", err)
		return
	}
	held := map[string]int{}
	for _, p := range pending {
		if ctx.Err() != nil {
			return
		}
		if _, running := ds.runningClaims.Load(int64(p.row.ID)); running {
			continue
		}
		tr, _, err := ds.triggerByID(ctx, p.trigger)
		if err != nil || !tr.Webhook || !tr.Enabled || !tr.runnable() {
			held[p.trigger]++
			continue
		}
		current, err := ds.pendingWebhooks(ctx, int64(p.row.ID))
		if err != nil {
			ds.svc.log.Error("substrate: pending webhook delivery could not be read again",
				"repository", ds.Repository().Name, "trigger", logSafeID(p.trigger), "fire", p.row.FireID, "failure", int64(p.row.ID), "error", err)
			return
		}
		if len(current) == 0 {
			continue
		}
		ds.fireWebhook(ctx, tr, current[0].row)
	}
	for trigger, n := range held {
		ds.svc.log.Warn("substrate: webhook deliveries left pending, their trigger does not run",
			"repository", ds.Repository().Name, "trigger", logSafeID(trigger), "pending", n)
	}
}

// pendingWebhook is one admitted request the ledger still holds: the trigger
// it was addressed to and its row.
type pendingWebhook struct {
	trigger string
	row     foldFailure
}

// pendingWebhooks reads the admitted requests whose fire has not settled,
// oldest first: every one, or the one row id names when it is not zero.
func (ds *dataset) pendingWebhooks(ctx context.Context, id int64) ([]pendingWebhook, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, trigger_id, fire_id, attempts, parked_at, payload
		FROM trigger_failures WHERE last_error = $1 AND ($2 = 0 OR id = $2) ORDER BY id`, pendingWebhookError, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []pendingWebhook
	for rows.Next() {
		p := pendingWebhook{row: foldFailure{LastError: pendingWebhookError}}
		var payload []byte
		if err := rows.Scan(&p.row.ID, &p.trigger, &p.row.FireID, &p.row.Attempts, &p.row.ParkedAt, &payload); err != nil {
			return nil, err
		}
		p.row.ParkedAt = p.row.ParkedAt.UTC()
		p.row.Payload = json.RawMessage(payload)
		out = append(out, p)
	}
	return out, rows.Err()
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

// fireEnvelope is the envelope a fire delivers: the one the caller built (a
// webhook delivery carrying its request) or, absent that, the bare fire. A
// parked envelope (parkedEnvelope) comes back with its body referenced, and
// the bytes are read back into it here so the callable sees the request as
// it arrived. A stored envelope was written by whatever binary parked it, so
// a park from before the repository carried an authority holds `repository:
// {owner}` alone; the current names fill what it lacks, since a body that
// reads repository.authority must not see an empty string on a retry.
func (ds *dataset) fireEnvelope(ctx context.Context, envelope map[string]any, fid string, at time.Time) (map[string]any, error) {
	owner, authority := ds.Repository().Name, ds.Repository().Authority
	if envelope == nil {
		return runner.FireEnvelope(fid, at, owner, authority), nil
	}
	repo, _ := envelope["repository"].(map[string]any)
	if repo == nil {
		repo = map[string]any{}
		envelope["repository"] = repo
	}
	if name, _ := repo["owner"].(string); name == "" {
		repo["owner"] = owner
	}
	if name, _ := repo["authority"].(string); name == "" {
		repo["authority"] = authority
	}
	if err := ds.restoreParkedRequest(ctx, envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}

// parkedHeaderNames are the headers a parked envelope keeps, by exact name.
// The changelog is history nothing can scrub, so the parked copy of a request
// carries only what a retry needs to deliver the request again and nothing
// that could be a credential: the headers that describe the body, and the
// headers the providers whose webhooks the shipped kinds receive use to
// identify and sign a delivery. It is a closed list, never a pattern: a name
// that merely contains a known word (`x-event-authorization`) is not on it.
// Every other header is dropped from the parked copy and absent on the retry.
// The webhook arm declares no header of its own; its one field is the
// substrate's own `key`, which the door checks and never forwards. A provider
// whose header is missing here is added here, by name.
var parkedHeaderNames = map[string]bool{
	// The body.
	"content-type": true, "content-length": true, "content-encoding": true, "user-agent": true, "date": true,
	// GitHub.
	"x-github-event": true, "x-github-delivery": true, "x-github-hook-id": true,
	"x-hub-signature": true, "x-hub-signature-256": true,
	// Stripe.
	"stripe-signature": true,
	// Slack.
	"x-slack-signature": true, "x-slack-request-timestamp": true,
	// Linear.
	"linear-event": true, "linear-delivery": true, "linear-signature": true,
	// Standard Webhooks (Svix, Notion and others).
	"webhook-id": true, "webhook-timestamp": true, "webhook-signature": true,
	// Request identity a sender attaches for its own retries.
	"idempotency-key": true, "x-request-id": true,
	// The Pebble sample's webhook reads its mode from this header.
	"x-pebble-mode": true,
}

// parkedHeaderKept reports a header the parked copy of a request keeps.
func parkedHeaderKept(name string) bool {
	return parkedHeaderNames[strings.ToLower(name)]
}

// parkedEnvelope is the parked form of a built envelope, the JSON the failure
// row and the changelog carry, or nil when the fire carried none. It is what
// the door records at admission (admitWebhook) and what a park rewrites. What
// differs from the delivered envelope is the policy for a payload that lives
// in append-only history: the request's headers narrow to the ones
// parkedHeaderKept admits, the query string is dropped, a multipart request's
// inline part values leave the envelope for the blob store like its file
// parts already have (spoolParkedParts), and the raw body leaves the envelope
// for the blob store, referenced by digest as `body: {blob, encoding}`. The
// bytes then live where every other attachment lives, plaintext in the
// repository's blob store (decision 0031), recoverable with the directory,
// held against the orphan sweep while the row stands (blobs.go
// parkedBlobsSQL) and collected once it retires. The changelog line holds
// the digest and never the body.
func (ds *dataset) parkedEnvelope(ctx context.Context, envelope map[string]any) (json.RawMessage, error) {
	if envelope == nil {
		return nil, nil
	}
	parked := make(map[string]any, len(envelope))
	for k, v := range envelope {
		parked[k] = v
	}
	if req, ok := envelope["request"].(map[string]any); ok {
		copied := make(map[string]any, len(req))
		for k, v := range req {
			copied[k] = v
		}
		if headers, ok := req["headers"].(map[string]any); ok {
			kept := make(map[string]any, len(headers))
			for name, v := range headers {
				if parkedHeaderKept(name) {
					kept[name] = v
				}
			}
			copied["headers"] = kept
		}
		// The query string goes whole: no shipped webhook body reads it, and
		// `?token=` is where a sender that cannot set headers puts its
		// credential. The retry delivers the request with an empty query.
		if _, ok := req["query"]; ok {
			copied["query"] = map[string]any{}
		}
		if body, ok := req["body"].(map[string]any); ok {
			spooled, err := ds.spoolParkedBody(ctx, body, req)
			if err != nil {
				return nil, err
			}
			copied["body"] = spooled
		}
		if parts, ok := req["parts"].([]any); ok {
			spooled, err := ds.spoolParkedParts(ctx, parts)
			if err != nil {
				return nil, err
			}
			copied["parts"] = spooled
		}
		parked["request"] = copied
	}
	raw, err := json.Marshal(parked)
	if err != nil {
		return nil, fmt.Errorf("substrate: parked fire envelope: %w", err)
	}
	return raw, nil
}

// parkedPayload runs a STORED failure payload through parkedEnvelope: the
// legacy row a binary before the ledger parked holds every header, the query
// and the body, and none of that may enter the changelog when the row is
// adopted (delivery.go adoptLegacyLedger) or rewritten by a retry. A payload
// already in the parked form passes through unchanged; an empty one stays
// empty.
func (ds *dataset) parkedPayload(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("substrate: parked payload: %w", err)
	}
	return ds.parkedEnvelope(ctx, envelope)
}

// spoolParkedBody stores a delivered body's bytes content-addressed under
// the webhook actor and returns the reference the parked envelope carries:
// `{blob, encoding}`, the encoding saying whether the callable read it as
// `text` or `base64`, so the retry rebuilds the same shape. A body already
// referenced (a re-park of a retried failure) passes through.
func (ds *dataset) spoolParkedBody(ctx context.Context, body, req map[string]any) (map[string]any, error) {
	if _, referenced := body["blob"]; referenced {
		return body, nil
	}
	var data []byte
	encoding := ""
	switch {
	case body["text"] != nil:
		text, _ := body["text"].(string)
		data, encoding = []byte(text), "text"
	case body["base64"] != nil:
		enc, _ := body["base64"].(string)
		decoded, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("substrate: parked body: %w", err)
		}
		data, encoding = decoded, "base64"
	default:
		return body, nil
	}
	mediaType, _ := req["contentType"].(string)
	info, err := ds.PutBlob(ctx, actorWebhook, substrate.BlobUpload{MediaType: mediaType}, data, "")
	if err != nil {
		return nil, fmt.Errorf("substrate: parked body: %w", err)
	}
	return map[string]any{"blob": info.Digest, "encoding": encoding}, nil
}

// spoolParkedParts is the multipart half of the parking policy: a file part
// already names its bytes by digest (spoolWebhookParts), and an inline part's
// value, which is where a form sender puts a token as readily as a
// transcript, goes the same way, stored content-addressed and named as
// `valueBlob`. Name, filename, media type and size stay in the entry; no
// value does. A part already referenced (a re-park) passes through.
func (ds *dataset) spoolParkedParts(ctx context.Context, parts []any) ([]any, error) {
	out := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		value, inline := part["value"].(string)
		if !inline {
			out = append(out, part)
			continue
		}
		info, err := ds.PutBlob(ctx, actorWebhook, substrate.BlobUpload{MediaType: "text/plain"}, []byte(value), "")
		if err != nil {
			return nil, fmt.Errorf("substrate: parked part %q: %w", part["name"], err)
		}
		copied := make(map[string]any, len(part))
		for k, v := range part {
			if k != "value" {
				copied[k] = v
			}
		}
		copied["valueBlob"] = info.Digest
		out = append(out, copied)
	}
	return out, nil
}

// restoreParkedRequest reads a parked envelope's body and inline part values
// back from the blob store into the shape the callable read at delivery. A
// request nothing references (a live delivery, a fire with no body) is left
// alone.
func (ds *dataset) restoreParkedRequest(ctx context.Context, envelope map[string]any) error {
	req, _ := envelope["request"].(map[string]any)
	if body, _ := req["body"].(map[string]any); body != nil {
		if digest, _ := body["blob"].(string); digest != "" {
			_, data, err := ds.GetBlob(ctx, digest)
			if err != nil {
				return fmt.Errorf("substrate: parked body %s: %w", digest, err)
			}
			if encoding, _ := body["encoding"].(string); encoding == "base64" {
				req["body"] = map[string]any{"base64": base64.StdEncoding.EncodeToString(data)}
			} else {
				req["body"] = map[string]any{"text": string(data)}
			}
		}
	}
	parts, _ := req["parts"].([]any)
	for i, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		digest, _ := part["valueBlob"].(string)
		if digest == "" {
			continue
		}
		_, data, err := ds.GetBlob(ctx, digest)
		if err != nil {
			return fmt.Errorf("substrate: parked part %q %s: %w", part["name"], digest, err)
		}
		restored := make(map[string]any, len(part))
		for k, v := range part {
			if k != "valueBlob" {
				restored[k] = v
			}
		}
		restored["value"] = string(data)
		parts[i] = restored
	}
	return nil
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
