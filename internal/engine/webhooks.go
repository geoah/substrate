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

// maxAgentWebhookInline bounds the inline bytes of a request bound for an
// agent callable: its body, or its multipart inline values. An agent reads
// the request in its user message, which is an llm/message record in the
// changelog and a turn sent to the model provider, so it is held to the 1 MiB
// an authenticated agent call's body may carry, whatever `maxBody` the
// trigger declares. A function callable reads the trigger's bound. The door
// checks it at admission and agentFire before every run, because a stored
// request runs through the callable its trigger names at that time.
const maxAgentWebhookInline = 1 << 20

// errAgentWebhookTooLarge parks a stored webhook request whose inline bytes
// pass maxAgentWebhookInline when its trigger now calls an agent: the door
// admitted it for a function. It is deterministic, because the stored bytes
// are as large on every attempt.
var errAgentWebhookTooLarge = errors.New("webhook request too large for an agent callable")

// errWebhookRefused is every refusal the door must not explain: no such
// repository, no such trigger, a trigger of another source, a disabled one, an
// unresolvable callable, a wrong or missing key, and a keyless trigger's
// signature refusal. One answer, so the endpoint space cannot be probed.
var errWebhookRefused = fmt.Errorf("%w: no such webhook", substrate.ErrNotFound)

// webhookPath is a webhook trigger's endpoint, relative to the server root.
// The repository is named by its authority, which is its one name (0071).
func webhookPath(authority, triggerID string) string {
	return "/webhooks/" + authority + "/" + triggerID
}

// pendingWebhookError is the error an ADMITTED webhook request carries in
// trigger_failures from its drain out of the spool until its fire settles. The request is
// recorded as a parked failure whose retry is the fire itself, so one row,
// one blob hold (blobs.go parkedBlobsSQL) and one set of ledger effects cover
// an accepted delivery, a running one and a parked one, and a rebuild or a
// restore reproduces all three. A process that stops while a row still
// carries it, before the fire or mid-fire before its effects committed,
// leaves it for the trigger dispatcher's next pass over the repository
// (resumeWebhooks), which runs the fire under the same id. An agent
// fire rewrites it to inFlightError at its claim (settlement.claim), and a
// row left that way waits for a hand, as every interrupted agent delivery
// does (decision 0064). Decision 0068 records the design, and 0167 moves the
// door's answer ahead of it, to the spool write.
const pendingWebhookError = "delivery accepted: the fire has not settled, and a restart resumes it"

// webhookFire says what receiveWebhook does with a spooled request: kick the
// background drain (the door), drain it here and run the fire inline (tests
// asserting on what the delivery wrote), drain it here and leave it pending
// (tests standing in for a process that stopped right after the drain), or
// leave it in the spool (tests standing in for a process that stopped right
// after the 202).
type webhookFire int

const (
	webhookFireDetached webhookFire = iota
	webhookFireInline
	webhookFireHeld
	webhookFireSpooled
)

// ReceiveWebhook is the public door (substrate.WebhookReceiver): resolve the
// repository by its authority, the trigger by id, check the key and the
// signature when the trigger declares them (decision 0164), write the request
// to the repository's spool, kick the drain and return (decision 0167). The sender gets its answer once the
// request is on disk, without waiting on the changelog lock; the drain
// records it in the ledger and hands its fire to the background, and a
// process that stops after the answer leaves the spool to the next
// dispatcher pass's drain.
func (s *service) ReceiveWebhook(ctx context.Context, authority, triggerID, key string, req substrate.WebhookRequest) (string, error) {
	return s.receiveWebhook(ctx, authority, triggerID, key, req, webhookFireDetached)
}

// CheckWebhook is the door's checks without the request
// (substrate.Service): the HTTP layer asks it before it takes a delivery slot
// or reads a body, so a request for no such webhook costs neither, and reads
// the body under the inline bound it answers. ReceiveWebhook runs the same
// checks again, so a key rotated between the two calls still refuses.
func (s *service) CheckWebhook(ctx context.Context, authority, triggerID, key string) (substrate.WebhookTarget, error) {
	_, tr, err := s.webhookTarget(ctx, authority, triggerID, key)
	if err != nil {
		return substrate.WebhookTarget{}, err
	}
	return substrate.WebhookTarget{
		MaxBody:       tr.webhookMaxBody(),
		SignatureOnly: tr.WebhookKey == "" && tr.WebhookSignature != nil,
	}, nil
}

// receiveWebhook is ReceiveWebhook with the drain and the fire detached,
// inline, held or not run.
//
// Past the trigger's checks, the declared signature's refusal answers
// errWebhookSignature on a keyed trigger, because by then the request has
// named a live trigger and carried its key, and errWebhookRefused on a
// keyless one. A request too large for an agent callable answers ErrTooLarge
// once it has passed the signature. A repeat of a signature the door admitted
// inside its window answers the first admission's fire id and spools nothing
// (webhookReplays).
func (s *service) receiveWebhook(ctx context.Context, authority, triggerID, key string, req substrate.WebhookRequest, fire webhookFire) (string, error) {
	ds, tr, err := s.webhookTarget(ctx, authority, triggerID, key)
	if err != nil {
		return "", err
	}
	// The signature is checked before anything is stored: a request its
	// provider did not sign writes no spool file, stages no blob and appends
	// no entry (decision 0164). The receipt time it is measured against is
	// the one the spool and the ledger record.
	at := s.webhookNow().UTC().Truncate(time.Microsecond)
	presented, err := ds.checkWebhookSignature(ctx, tr, req, at)
	if err != nil {
		return "", err
	}
	// The door read the body under the trigger's bound; a caller that hands
	// the service a request directly is held to the same one.
	if n := int64(webhookInlineBytes(req)); n > tr.webhookMaxBody() {
		return "", fmt.Errorf("%w: webhook carries %d inline bytes, and this trigger admits at most %d", substrate.ErrTooLarge, n, tr.webhookMaxBody())
	}
	if tr.Agent != nil {
		if n := webhookInlineBytes(req); n > maxAgentWebhookInline {
			return "", fmt.Errorf("%w: webhook carries %d inline bytes, and an agent callable reads at most 1 MiB", substrate.ErrTooLarge, n)
		}
	}
	var replay *webhookReplay
	var replayKey string
	if !presented.signedAt.IsZero() {
		replayKey = webhookReplayKey(tr.ID, presented.mac)
		var first string
		if replay, first = ds.webhookReplays.claim(replayKey, presented.signedAt.Add(tr.WebhookSignature.MaxSkew), at); replay == nil {
			if first == "" {
				return "", errWebhookReplayInFlight
			}
			return first, nil
		}
	}
	name, fid, err := ds.spoolWebhook(ctx, tr, req, at, tr.WebhookSignature != nil)
	if replay != nil {
		if err != nil {
			ds.webhookReplays.release(replayKey, replay)
		} else {
			ds.webhookReplays.admitted(replay, fid)
		}
	}
	if err != nil {
		return "", err
	}
	switch fire {
	case webhookFireDetached:
		// The request is on disk and answered whether or not the supervisor
		// takes the drain: one it refuses (shutdown has begun) stays spooled
		// and the next process's dispatcher pass drains it.
		ds.kickSpoolDrain()
	case webhookFireInline, webhookFireHeld:
		got, err := ds.drainSpooled(withForegroundWrites(ctx), name)
		if err != nil {
			return "", err
		}
		if fire == webhookFireInline && got.recorded {
			ds.fireWebhook(ctx, tr, got.row)
		}
	}
	return fid, nil
}

// webhookTarget resolves the repository by its authority and the trigger by
// id, and checks that the trigger takes webhooks, is enabled and runnable, and
// that the key matches when it declares one. The door's webhook grant for
// this authority and trigger (substrate.WithWebhookGrant, an app's token)
// stands in for the key and for nothing else. Every check that fails answers
// errWebhookRefused.
func (s *service) webhookTarget(ctx context.Context, authority, triggerID, key string) (*dataset, *trigger, error) {
	repo, err := s.repositoryByAuthority(ctx, authority)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) {
			return nil, nil, errWebhookRefused
		}
		return nil, nil, err
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
		return nil, nil, errWebhookRefused
	}
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		if errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrValidation) {
			return nil, nil, errWebhookRefused
		}
		return nil, nil, err
	}
	if !tr.Webhook || !tr.Enabled || !tr.runnable() {
		return nil, nil, errWebhookRefused
	}
	if tr.WebhookKey != "" && !substrate.WebhookGranted(ctx, authority, triggerID) &&
		subtle.ConstantTimeCompare([]byte(tr.WebhookKey), []byte(key)) != 1 {
		return nil, nil, errWebhookRefused
	}
	return ds, tr, nil
}

// admitWebhook records one spooled request in the ledger (drainSpooled): the
// blob spool and the pending entry. The request is written as a parked
// failure carrying pendingWebhookError, in its parked form (parkedEnvelope:
// the body spooled by digest, the headers narrowed to the body's and the
// declared ones, the query dropped), with the fire id and the receipt time
// the door answered with, on a delivery entry of its own whose seq is the
// row's id. The row returned is what the fire runs and what a restart
// resumes.
//
// The delivery is one changelog transaction: the blobs' bytes are written
// before it, holding no lock (stageWebhook), and their manifests are settled
// inside it, beside the park (recordWebhook), so the drain waits on the
// changelog lock once. after is the changelog head when the request was
// spooled: the transaction refuses with errWebhookRecorded when an entry
// above it already names the fire id. verified says the door checked the
// trigger's declared signature before it spooled the request.
func (ds *dataset) admitWebhook(ctx context.Context, triggerID string, declared map[string]bool, req substrate.WebhookRequest, fid string, at time.Time, after int64, verified bool) (foldFailure, error) {
	blobs := ds.stageBlobs(actorWebhook)
	row, err := ds.stageWebhook(ctx, declared, req, fid, at, verified, blobs)
	if err != nil {
		return foldFailure{}, err
	}
	return ds.recordWebhook(ctx, triggerID, row, blobs, after)
}

// recordWebhook is admitWebhook's transaction: the staged blobs' manifests,
// the pending row and its delivery entry. The blob manifests' entries name
// the webhook actor, the delivery entry the system. It first reads, under
// the changelog lock, whether an entry above after names the row's fire id,
// so no two drains (two datasets, two processes over one copy) record one
// request twice.
func (ds *dataset) recordWebhook(ctx context.Context, triggerID string, row foldFailure, blobs *stagedBlobs, after int64) (foldFailure, error) {
	err := ds.inTx(ctx, actorWebhook, true, func(t *txn) error {
		if err := t.lockChangelog(); err != nil {
			return err
		}
		var recorded bool
		if err := t.row(webhookRecordedSQL, triggerID, typeTrigger, string(substrate.OpDelivery), after, row.FireID).Scan(&recorded); err != nil {
			return err
		}
		if recorded {
			return errWebhookRecorded
		}
		// The manifests append before the seq is reserved: the delivery
		// entry has to be the transaction's last.
		if err := blobs.settle(t); err != nil {
			return err
		}
		id, err := t.reserveSeq()
		if err != nil {
			return err
		}
		row.ID = foldInt(id)
		if err := t.parkTx(triggerID, row); err != nil {
			return err
		}
		return t.appendDeliveryAt(triggerID, id)
	})
	if err != nil {
		return foldFailure{}, fmt.Errorf("substrate: webhook %s: record the request: %w", triggerID, err)
	}
	return row, nil
}

// stageWebhook is admitWebhook up to its transaction: the request's bytes
// staged into the blob store and the pending row built around their digests.
// It writes nothing to the changelog, so a process that stops after it
// leaves only objects no manifest names, for the orphan sweep.
func (ds *dataset) stageWebhook(ctx context.Context, declared map[string]bool, req substrate.WebhookRequest, fid string, at time.Time, verified bool, blobs blobSpool) (foldFailure, error) {
	parts, err := ds.spoolWebhookParts(ctx, blobs, req.Parts)
	if err != nil {
		return foldFailure{}, err
	}
	envelope := runner.FireEnvelope(fid, at, ds.Repository().Authority)
	request := webhookRequestEnvelope(req, parts)
	// The mark a later retry or resume reads (checkStoredWebhookSignature):
	// this request passed the declared signature, so it is not checked
	// again against a timestamp the retry's clock has left behind.
	if verified {
		request[requestSignatureVerified] = true
	}
	envelope["request"] = request
	payload, err := ds.parkedEnvelope(ctx, envelope, declared, blobs)
	if err != nil {
		return foldFailure{}, err
	}
	return foldFailure{FireID: fid, LastError: pendingWebhookError, ParkedAt: at, Payload: payload}, nil
}

// blobSpool is where a webhook request's bytes go before its envelope names
// them by digest: staged for the admission's transaction to settle
// (stagedBlobs), or stored outright (putBlobs).
type blobSpool interface {
	stage(ctx context.Context, up substrate.BlobUpload, data []byte) (string, error)
}

// putBlobs is the blobSpool for a park with no transaction of its own to
// settle manifests in: each blob is stored and its manifest settled before
// stage returns (PutBlob), under the webhook actor.
type putBlobs struct{ ds *dataset }

func (s putBlobs) stage(ctx context.Context, up substrate.BlobUpload, data []byte) (string, error) {
	info, err := s.ds.PutBlob(ctx, actorWebhook, up, data, "")
	if err != nil {
		return "", err
	}
	return info.Digest, nil
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
	// An agent at a spend cap leaves the request pending, unclaimed: the
	// next pass's resume fires it once the cap clears (spend.go).
	ctx, held, err := ds.holdAgentDelivery(ctx, tr)
	if err != nil {
		ds.svc.log.Error("substrate: webhook fire could not read the spend cap, the entry stays pending",
			"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID), "failure", int64(row.ID))
		return
	}
	if held {
		return
	}
	var envelope map[string]any
	if err := json.Unmarshal(row.Payload, &envelope); err != nil {
		ds.svc.log.Error("substrate: webhook fire cannot read its recorded request, the entry stays pending",
			"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID), "failure", int64(row.ID))
		return
	}
	_, _, err = ds.deliverFire(ctx, tr, runner.ModeWebhook, row.FireID, row.ParkedAt, nil, envelope, &row)
	if err == nil {
		return
	}
	outcome := webhookFireOutcome(err)
	attrs := []any{"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID), "failure", int64(row.ID), "outcome", outcome}
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
// runs: a process that stopped after the drain and before the fire's effects
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
			"repository", ds.Repository().ID, "error", err)
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
				"repository", ds.Repository().ID, "trigger", logSafeID(p.trigger), "fire", p.row.FireID, "failure", int64(p.row.ID), "error", err)
			return
		}
		if len(current) == 0 {
			continue
		}
		ds.fireWebhook(ctx, tr, current[0].row)
	}
	for trigger, n := range held {
		ds.svc.log.Warn("substrate: webhook deliveries left pending, their trigger does not run",
			"repository", ds.Repository().ID, "trigger", logSafeID(trigger), "pending", n)
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
func (ds *dataset) spoolWebhookParts(ctx context.Context, blobs blobSpool, parts []substrate.WebhookPart) ([]map[string]any, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		if p.Data == nil {
			out = append(out, map[string]any{"name": p.Name, "value": p.Value})
			continue
		}
		digest, err := blobs.stage(ctx,
			substrate.BlobUpload{Name: blobNameOf(p.Filename), MediaType: p.MediaType}, p.Data)
		if err != nil {
			return nil, fmt.Errorf("substrate: webhook part %q: %w", p.Name, err)
		}
		part := map[string]any{
			"name": p.Name, "filename": p.Filename, "mediaType": p.MediaType,
			"size": int64(len(p.Data)), "blob": digest,
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
// {owner}` alone; the authority fills what it lacks, since a body that reads
// repository.authority must not see an empty string on a retry.
func (ds *dataset) fireEnvelope(ctx context.Context, envelope map[string]any, fid string, at time.Time) (map[string]any, error) {
	authority := ds.Repository().Authority
	if envelope == nil {
		return runner.FireEnvelope(fid, at, authority), nil
	}
	repo, _ := envelope["repository"].(map[string]any)
	if repo == nil {
		repo = map[string]any{}
		envelope["repository"] = repo
	}
	if name, _ := repo["authority"].(string); name == "" {
		repo["authority"] = authority
	}
	if err := ds.restoreParkedRequest(ctx, envelope); err != nil {
		return nil, err
	}
	return envelope, nil
}

// bodyHeaderNames are the headers every webhook fire keeps whatever its
// trigger declares: the ones that describe the body a callable has to read
// it, and that a replay has to deliver it again. Everything else arrives
// only because the trigger record named it in `source.webhook.headers`
// (decision 0097), matched case-insensitively by exact name and never by
// pattern. The narrowing is the same for the fire and for the parked copy,
// because the fire runs the parked bytes: the changelog is history nothing
// can scrub, so a header nobody declared is dropped before it is written.
var bodyHeaderNames = map[string]bool{
	"content-type": true, "content-length": true, "content-encoding": true, "user-agent": true, "date": true,
}

// parkedHeaderKept reports a header the fire and its parked copy keep:
// body-describing, or one of the names the trigger declared (lowercased at
// parse time, as the door's names already are).
func parkedHeaderKept(name string, declared map[string]bool) bool {
	lower := strings.ToLower(name)
	return bodyHeaderNames[lower] || declared[lower]
}

// parkedEnvelope is the parked form of a built envelope, the JSON the failure
// row and the changelog carry, or nil when the fire carried none. It is what
// the door records at admission (admitWebhook) and what a park rewrites. What
// differs from the delivered envelope is the policy for a payload that lives
// in append-only history: the request's headers narrow to the ones
// parkedHeaderKept admits under the trigger's declared set, the query string
// is dropped, a multipart request's inline part values leave the envelope for
// the blob store like its file parts already have (spoolParkedParts), and the
// raw body leaves the envelope
// for the blob store, referenced by digest as `body: {blob, encoding}`. The
// bytes then live where every other attachment lives, plaintext in the
// repository's blob store (decision 0031), recoverable with the directory,
// held against the orphan sweep while the row stands (blobs.go
// parkedBlobsSQL) and collected once it retires. The changelog line holds
// the digest and never the body.
func (ds *dataset) parkedEnvelope(ctx context.Context, envelope map[string]any, declared map[string]bool, blobs blobSpool) (json.RawMessage, error) {
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
				if parkedHeaderKept(name, declared) {
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
			spooled, err := spoolParkedBody(ctx, blobs, body, req)
			if err != nil {
				return nil, err
			}
			copied["body"] = spooled
		}
		if parts, ok := req["parts"].([]any); ok {
			spooled, err := spoolParkedParts(ctx, blobs, parts)
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

// spoolParkedBody stores a delivered body's bytes content-addressed through
// blobs and returns the reference the parked envelope carries:
// `{blob, encoding}`, the encoding saying whether the callable read it as
// `text` or `base64`, so the retry rebuilds the same shape. A body already
// referenced (a re-park of a retried failure) passes through.
func spoolParkedBody(ctx context.Context, blobs blobSpool, body, req map[string]any) (map[string]any, error) {
	if _, referenced := body["blob"]; referenced {
		return body, nil
	}
	data, encoding, ok, err := inlineBodyBytes(body)
	if err != nil {
		return nil, fmt.Errorf("substrate: parked body: %w", err)
	}
	if !ok {
		return body, nil
	}
	mediaType, _ := req["contentType"].(string)
	digest, err := blobs.stage(ctx, substrate.BlobUpload{MediaType: mediaType}, data)
	if err != nil {
		return nil, fmt.Errorf("substrate: parked body: %w", err)
	}
	return map[string]any{"blob": digest, "encoding": encoding}, nil
}

// spoolParkedParts is the multipart half of the parking policy: a file part
// already names its bytes by digest (spoolWebhookParts), and an inline part's
// value, which is where a form sender puts a token as readily as a
// transcript, goes the same way, stored content-addressed and named as
// `valueBlob`. Name, filename, media type and size stay in the entry; no
// value does. A part already referenced (a re-park) passes through.
func spoolParkedParts(ctx context.Context, blobs blobSpool, parts []any) ([]any, error) {
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
		digest, err := blobs.stage(ctx, substrate.BlobUpload{MediaType: "text/plain"}, []byte(value))
		if err != nil {
			return nil, fmt.Errorf("substrate: parked part %q: %w", part["name"], err)
		}
		copied := make(map[string]any, len(part))
		for k, v := range part {
			if k != "value" {
				copied[k] = v
			}
		}
		copied["valueBlob"] = digest
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
	if body, _ := req["body"].(map[string]any); body != nil && body["blob"] != nil {
		data, encoding, _, err := ds.storedBodyBytes(ctx, body)
		if err != nil {
			return fmt.Errorf("substrate: parked body: %w", err)
		}
		if encoding == "base64" {
			req["body"] = map[string]any{"base64": base64.StdEncoding.EncodeToString(data)}
		} else {
			req["body"] = map[string]any{"text": string(data)}
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

// storedBodyBytes reads a stored request body's raw bytes in any of its
// shapes: `{text}` and `{base64}` as the callable reads it, and `{blob,
// encoding}` as the ledger parks it, read back from the blob store. encoding
// is the shape the callable reads the bytes in, `text` or `base64`. ok is
// false for a body in none of them. Every reader of a stored body goes
// through here, so a new shape is one case and no reader is left decoding
// it as empty.
func (ds *dataset) storedBodyBytes(ctx context.Context, body map[string]any) (data []byte, encoding string, ok bool, err error) {
	if body["blob"] == nil {
		return inlineBodyBytes(body)
	}
	digest, _ := body["blob"].(string)
	if _, data, err = ds.GetBlob(ctx, digest); err != nil {
		return nil, "", false, fmt.Errorf("blob %s: %w", digest, err)
	}
	if encoding, _ = body["encoding"].(string); encoding != "base64" {
		encoding = "text"
	}
	return data, encoding, true, nil
}

// inlineBodyBytes is storedBodyBytes for the two shapes that carry their
// bytes in the envelope, `{text}` and `{base64}`, which need no blob store.
func inlineBodyBytes(body map[string]any) (data []byte, encoding string, ok bool, err error) {
	switch {
	case body["base64"] != nil:
		enc, _ := body["base64"].(string)
		if data, err = base64.StdEncoding.DecodeString(enc); err != nil {
			return nil, "", false, err
		}
		return data, "base64", true, nil
	case body["text"] != nil:
		text, _ := body["text"].(string)
		return []byte(text), "text", true, nil
	}
	return nil, "", false, nil
}

// webhookInlineBytes is what of a request rides the envelope inline: the
// body, or a multipart request's inline part values. File parts go to the
// blob store and ride by digest.
func webhookInlineBytes(req substrate.WebhookRequest) int {
	n := len(req.Body)
	for _, p := range req.Parts {
		if p.Data == nil {
			n += len(p.Value)
		}
	}
	return n
}

// deliveredInlineBytes is webhookInlineBytes over a delivered envelope's
// request, as fireEnvelope restored it: the body's raw bytes, counted from
// its text or its padded base64 without decoding, or the multipart inline
// part values. A file part rides by digest.
func deliveredInlineBytes(envelope map[string]any) int {
	req, _ := envelope["request"].(map[string]any)
	n := 0
	if body, _ := req["body"].(map[string]any); body != nil {
		if text, ok := body["text"].(string); ok {
			n += len(text)
		} else if enc, ok := body["base64"].(string); ok {
			n += base64.StdEncoding.DecodedLen(len(enc)) - strings.Count(enc[max(len(enc)-2, 0):], "=")
		}
	}
	parts, _ := req["parts"].([]any)
	for _, raw := range parts {
		if part, ok := raw.(map[string]any); ok {
			if value, ok := part["value"].(string); ok {
				n += len(value)
			}
		}
	}
	return n
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
