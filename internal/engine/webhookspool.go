package engine

// The webhook spool (decision 0167): the door writes an admitted request to
// a file under the repository directory's spool/, sealed under the
// repository's DEK and fsynced with its directory, and answers 202. A drain
// records each spooled request in the delivery ledger through the admission
// transaction (webhooks.go admitWebhook), oldest first, deletes the file once
// that transaction has committed, and hands the fire to the background. The
// sender's answer never waits on the changelog lock.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/metrics"
	"github.com/geoah/substrate/internal/substrate"
)

// The spool's bounds per repository. A drain that cannot record (a database
// that refuses writes, a request it cannot admit) stops at the oldest
// request, and the door refuses with ErrWebhookSpoolFull once either bound is
// reached, so a stuck drain costs at most this much disk.
var (
	webhookSpoolMaxBytes   int64 = 256 << 20
	webhookSpoolMaxEntries       = 10000
)

// The drain's wait after a failed pass, doubled per consecutive failure up to
// the cap.
var (
	webhookSpoolRetryMin = time.Second
	webhookSpoolRetryMax = 30 * time.Second
)

// ErrWebhookSpoolFull is the door's refusal while the repository's spool is
// at a bound: unavailable, so the sender is told to retry later (503 with
// Retry-After) rather than that the request was wrong.
var ErrWebhookSpoolFull = fmt.Errorf("%w: the webhook spool of this repository is full; retry later", substrate.ErrUnavailable)

// errWebhookRecorded is the admission's refusal of a request the changelog
// already holds: the drain deletes its file and records nothing.
var errWebhookRecorded = errors.New("substrate/engine: the webhook request is already recorded")

// webhookRecordedSQL reports whether a delivery entry above a seq names a
// fire id. Only the trigger's own entries can, and the changelog indexes
// them by record id.
const webhookRecordedSQL = `
	SELECT EXISTS (SELECT 1 FROM changelog
		WHERE record_id = $1 AND kind = $2 AND op = $3 AND seq > $4
		  AND jsonb_path_exists(payload, '$.** ? (@.fireId == $fid)', jsonb_build_object('fid', $5::text)))`

// errSpoolUnreadable marks a spooled file the drain cannot open or decode.
// The drain sets it aside and moves on, because no retry will read it.
var errSpoolUnreadable = errors.New("substrate/engine: spooled webhook request is unreadable")

// spoolFormat is the spooled request's format. A file of another format is
// left in place for a binary that reads it.
const spoolFormat = 1

// spooledWebhook is a spooled request as its sealed file carries it: the
// trigger it was addressed to, the fire id and receipt time the door
// answered with, the changelog head when it was spooled (every entry the
// drain could have written for it is above that seq), the header names the
// trigger declared then, and the request in the form the ledger would record
// it: the headers already narrowed and the query dropped.
type spooledWebhook struct {
	Format     int       `json:"format"`
	Trigger    string    `json:"trigger"`
	FireID     string    `json:"fireId"`
	ReceivedAt time.Time `json:"receivedAt"`
	After      int64     `json:"after"`
	Declared   []string  `json:"declared,omitempty"`
	// Verified is set when the door checked the trigger's declared
	// signature before it spooled the request (decision 0164).
	Verified bool           `json:"signatureVerified,omitempty"`
	Request  spooledRequest `json:"request"`
}

// spooledRequest is substrate.WebhookRequest less its query. Body and a
// part's Data keep nil apart from empty: a nil body is a multipart request
// and a nil Data an inline field.
type spooledRequest struct {
	Method      string            `json:"method"`
	ContentType string            `json:"contentType"`
	Headers     map[string]string `json:"headers"`
	Body        []byte            `json:"body"`
	Parts       []spooledPart     `json:"parts,omitempty"`
}

type spooledPart struct {
	Name      string `json:"name"`
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Value     string `json:"value,omitempty"`
	Data      []byte `json:"data"`
}

func (r spooledRequest) webhookRequest() substrate.WebhookRequest {
	out := substrate.WebhookRequest{
		Method: r.Method, ContentType: r.ContentType, Headers: r.Headers,
		Query: map[string][]string{}, Body: r.Body,
	}
	for _, p := range r.Parts {
		out.Parts = append(out.Parts, substrate.WebhookPart{
			Name: p.Name, Filename: p.Filename, MediaType: p.MediaType, Value: p.Value, Data: p.Data,
		})
	}
	return out
}

// declaredSet is the declared header names as parkedEnvelope takes them.
func (sp spooledWebhook) declaredSet() map[string]bool {
	out := make(map[string]bool, len(sp.Declared))
	for _, name := range sp.Declared {
		out[name] = true
	}
	return out
}

// spoolAAD binds a spooled file's ciphertext to its repository and its fire
// id, so a file copied into another repository's spool, or renamed to
// another fire, does not open.
func spoolAAD(repoID, fireID string) []byte {
	return []byte("webhook-spool\x00" + repoID + "\x00" + fireID)
}

// webhookSpool is one repository's spool as this process knows it: the files
// on disk by name, loaded at first use, plus the drain's state.
type webhookSpool struct {
	mu      sync.Mutex
	loaded  bool
	entries map[string]*spoolEntry
	bytes   int64
	// last is the highest ordinal handed out or found, so names keep
	// arrival order when the clock does not move between two requests.
	last int64

	// drainMu is held around each request's admission, from the read of its
	// file to the file's deletion, so two drains never record one request.
	drainMu sync.Mutex
	// draining is set while a drain task runs; again is a kick that came
	// while it ran.
	draining atomic.Bool
	again    atomic.Bool
}

// spoolEntry is one spooled file. writing is set from the door's
// reservation until its file is on disk, and the drain skips it until then.
type spoolEntry struct {
	size    int64
	at      time.Time
	writing bool
}

// loadSpoolLocked fills the index from the spool directory the first time it
// is needed, and removes the half-written files a stop left: their requests
// were never answered. Called with spool.mu held.
func (ds *dataset) loadSpoolLocked() error {
	sp := &ds.spool
	if sp.loaded {
		return nil
	}
	if !ds.svc.readOnly {
		if err := changelogfile.RemoveSpoolTemps(ds.dir); err != nil {
			return err
		}
	}
	files, err := changelogfile.SpoolFiles(ds.dir)
	if err != nil {
		return err
	}
	sp.entries = make(map[string]*spoolEntry, len(files))
	for _, f := range files {
		ordinal, _, err := changelogfile.ParseSpoolFileName(f.Name)
		if err != nil {
			continue
		}
		sp.entries[f.Name] = &spoolEntry{size: f.Size, at: changelogfile.SpoolOrdinalTime(ordinal)}
		sp.bytes += f.Size
		sp.last = max(sp.last, ordinal)
	}
	sp.loaded = true
	return nil
}

// spoolStats is the spool's reading for its metrics.
func (ds *dataset) spoolStats() metrics.SpoolStats {
	sp := &ds.spool
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if err := ds.loadSpoolLocked(); err != nil {
		return metrics.SpoolStats{}
	}
	var out metrics.SpoolStats
	for _, e := range sp.entries {
		out.Entries++
		out.Bytes += e.size
		if out.Oldest.IsZero() || e.at.Before(out.Oldest) {
			out.Oldest = e.at
		}
	}
	return out
}

// spoolWebhook is the door's write: the request narrowed as the ledger would
// record it (parkedHeaderKept, no query), sealed under the DEK and written
// durably to the spool. It returns the file's name and the fire id the
// sender is answered with. It refuses what inTx would refuse before it
// writes anything, so the door does not answer for a request no drain in
// this process could record, and it refuses with ErrWebhookSpoolFull at a
// bound. at is the door's receipt time and verified says the door checked
// the trigger's declared signature.
func (ds *dataset) spoolWebhook(ctx context.Context, tr *trigger, req substrate.WebhookRequest, at time.Time, verified bool) (string, string, error) {
	if ds.svc.readOnly {
		return "", "", ErrDirectoryReadOnly
	}
	if err := ds.svc.lease.err(); err != nil {
		return "", "", err
	}
	if err := ds.doorDirectoryErr(); err != nil {
		return "", "", err
	}
	wid, err := newID()
	if err != nil {
		return "", "", err
	}
	fid := webhookFirePrefix + wid
	var after int64
	if err := ds.db.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&after); err != nil {
		return "", "", err
	}
	entry := spooledWebhook{
		Format: spoolFormat, Trigger: tr.ID, FireID: fid, ReceivedAt: at, After: after, Verified: verified,
		Request: spooledRequest{
			Method: req.Method, ContentType: req.ContentType,
			Headers: make(map[string]string, len(req.Headers)), Body: req.Body,
		},
	}
	for name := range tr.WebhookHeaders {
		entry.Declared = append(entry.Declared, name)
	}
	sort.Strings(entry.Declared)
	for name, v := range req.Headers {
		if parkedHeaderKept(name, tr.WebhookHeaders) {
			entry.Request.Headers[name] = v
		}
	}
	for _, p := range req.Parts {
		entry.Request.Parts = append(entry.Request.Parts, spooledPart{
			Name: p.Name, Filename: p.Filename, MediaType: p.MediaType, Value: p.Value, Data: p.Data,
		})
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return "", "", err
	}
	sealed, err := ds.sealPayload(raw, spoolAAD(ds.Repository().ID, fid))
	if err != nil {
		return "", "", err
	}

	sp := &ds.spool
	sp.mu.Lock()
	if err := ds.loadSpoolLocked(); err != nil {
		sp.mu.Unlock()
		return "", "", err
	}
	size := int64(len(sealed))
	if len(sp.entries)+1 > webhookSpoolMaxEntries || sp.bytes+size > webhookSpoolMaxBytes {
		sp.mu.Unlock()
		metrics.WebhookSpoolRefusals.WithLabelValues(ds.Repository().ID).Inc()
		return "", "", ErrWebhookSpoolFull
	}
	ordinal := max(sp.last+1, at.UnixNano())
	sp.last = ordinal
	name := changelogfile.SpoolFileName(ordinal, fid)
	e := &spoolEntry{size: size, at: at, writing: true}
	sp.entries[name] = e
	sp.bytes += size
	sp.mu.Unlock()

	if err := changelogfile.WriteSpoolFile(ds.dir, name, sealed); err != nil {
		sp.mu.Lock()
		delete(sp.entries, name)
		sp.bytes -= size
		sp.mu.Unlock()
		return "", "", fmt.Errorf("%w: spool the webhook request: %w", substrate.ErrUnavailable, err)
	}
	sp.mu.Lock()
	e.writing = false
	sp.mu.Unlock()
	return name, fid, nil
}

// spooledNames is the spooled requests the drain may take, oldest first.
func (ds *dataset) spooledNames() ([]string, error) {
	sp := &ds.spool
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if err := ds.loadSpoolLocked(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sp.entries))
	for name, e := range sp.entries {
		if !e.writing {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// forgetSpooled drops a file the drain deleted or set aside from the index.
func (ds *dataset) forgetSpooled(name string) {
	sp := &ds.spool
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if e, ok := sp.entries[name]; ok {
		sp.bytes -= e.size
		delete(sp.entries, name)
	}
}

// drainedWebhook is what one drained request left: the pending row the
// admission wrote, or nothing when the request was already in the ledger or
// its file was gone; and the trigger the request was addressed to.
type drainedWebhook struct {
	row     foldFailure
	trigger string
	// recorded is set when this drain wrote the pending row.
	recorded bool
}

// drainSpooled records one spooled request: it reads and opens the file,
// writes the pending row and its delivery entry unless the changelog already
// holds an entry for the fire id above the spool's head (a stop between the
// commit and the deletion, or a copy taken between them), and then deletes
// the file. The row is recorded at most once, because the check and the
// admission run under drainMu and the check reads the changelog itself.
func (ds *dataset) drainSpooled(ctx context.Context, name string) (drainedWebhook, error) {
	sp := &ds.spool
	sp.drainMu.Lock()
	defer sp.drainMu.Unlock()
	_, fid, err := changelogfile.ParseSpoolFileName(name)
	if err != nil {
		return drainedWebhook{}, err
	}
	sealed, err := changelogfile.ReadSpoolFile(ds.dir, name)
	if errors.Is(err, os.ErrNotExist) {
		ds.forgetSpooled(name)
		return drainedWebhook{}, nil
	}
	if err != nil {
		return drainedWebhook{}, err
	}
	raw, err := ds.openPayload(sealed, spoolAAD(ds.Repository().ID, fid))
	if err != nil {
		return drainedWebhook{}, fmt.Errorf("%w: %s: %w", errSpoolUnreadable, name, err)
	}
	var entry spooledWebhook
	if err := json.Unmarshal(raw, &entry); err != nil {
		return drainedWebhook{}, fmt.Errorf("%w: %s: %w", errSpoolUnreadable, name, err)
	}
	if entry.Format != spoolFormat {
		return drainedWebhook{}, fmt.Errorf("substrate/engine: spooled webhook request %s is format %d, this binary reads %d", name, entry.Format, spoolFormat)
	}
	if entry.FireID != fid {
		return drainedWebhook{}, fmt.Errorf("%w: %s holds fire %s", errSpoolUnreadable, name, entry.FireID)
	}
	out := drainedWebhook{trigger: entry.Trigger}
	// A read before the blobs are staged spares a recorded request their
	// bytes; the admission reads again under the changelog lock.
	recorded, err := ds.webhookRecorded(ctx, entry)
	if err != nil {
		return drainedWebhook{}, err
	}
	if !recorded {
		row, err := ds.admitWebhook(ctx, entry.Trigger, entry.declaredSet(), entry.Request.webhookRequest(), fid, entry.ReceivedAt, entry.After, entry.Verified)
		switch {
		case errors.Is(err, errWebhookRecorded):
		case err != nil:
			return drainedWebhook{}, err
		default:
			out.row, out.recorded = row, true
		}
	}
	if err := changelogfile.RemoveSpoolFile(ds.dir, name); err != nil {
		// The row is recorded; the file stays for the next drain, which
		// finds the entry in the changelog and only deletes it.
		return out, fmt.Errorf("substrate/engine: delete the drained webhook request %s: %w", name, err)
	}
	ds.forgetSpooled(name)
	return out, nil
}

// webhookRecorded reports whether the changelog already holds a delivery
// entry naming the request's fire id above the spool's head.
func (ds *dataset) webhookRecorded(ctx context.Context, entry spooledWebhook) (bool, error) {
	var found bool
	err := ds.db.QueryRowContext(ctx, webhookRecordedSQL,
		entry.Trigger, typeTrigger, string(substrate.OpDelivery), entry.After, entry.FireID).Scan(&found)
	return found, err
}

// kickSpoolDrain starts the drain task unless one runs, in which case that
// one takes another pass before it returns. The door kicks it after every
// spooled request and the dispatcher at every pass, which is how a restart
// drains what the last process left. A read-only process drains nothing.
func (ds *dataset) kickSpoolDrain() {
	if ds.svc.readOnly {
		return
	}
	sp := &ds.spool
	sp.again.Store(true)
	if !sp.draining.CompareAndSwap(false, true) {
		return
	}
	if !ds.spawn("webhook spool drain", ds.runSpoolDrain) {
		sp.draining.Store(false)
	}
}

// runSpoolDrain is the drain task: passes over the spool, oldest first,
// until a pass finds nothing new. A request it cannot record stops the pass
// there, so the requests after it wait rather than overtake it, and the
// task waits and starts again from the oldest. A canceled context
// (shutdown) leaves what remains for the next process.
func (ds *dataset) runSpoolDrain(ctx context.Context) {
	sp := &ds.spool
	wait := webhookSpoolRetryMin
	for {
		for sp.again.Swap(false) {
			if ds.drainSpoolPass(ctx) {
				wait = webhookSpoolRetryMin
				continue
			}
			if ctx.Err() != nil {
				sp.draining.Store(false)
				return
			}
			sp.again.Store(true)
			select {
			case <-ctx.Done():
				sp.draining.Store(false)
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, webhookSpoolRetryMax)
		}
		sp.draining.Store(false)
		// A kick between the last Swap and the Store found the task still
		// running and started none: take it here.
		if !sp.again.Load() || !sp.draining.CompareAndSwap(false, true) {
			return
		}
	}
}

// drainSpoolPass drains every spooled request in order and reports whether
// it reached the end. Each recorded request's fire is handed to the
// background as the door's own used to be.
func (ds *dataset) drainSpoolPass(ctx context.Context) bool {
	names, err := ds.spooledNames()
	if err != nil {
		ds.spoolDrainFailed("", err)
		return false
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return false
		}
		// The sender has its answer, but the request is still owed to the
		// ledger, so its admission goes ahead of the repository's background
		// writers as the door's did.
		got, err := ds.drainSpooled(withForegroundWrites(ctx), name)
		if errors.Is(err, errSpoolUnreadable) {
			ds.spoolDrainFailed(name, err)
			if aside := changelogfile.SetSpoolFileAside(ds.dir, name); aside != nil {
				ds.svc.log.Error("substrate: an unreadable spooled webhook request could not be set aside",
					"repository", ds.Repository().ID, "file", name, "error", aside)
				return false
			}
			ds.forgetSpooled(name)
			continue
		}
		if err != nil {
			ds.spoolDrainFailed(name, err)
			if got.recorded {
				ds.fireDrained(got)
			}
			return false
		}
		if got.recorded {
			ds.fireDrained(got)
		}
	}
	return true
}

// spoolDrainFailed counts and logs one failed drain step. The line names the
// file and classifies the error to a fixed word (webhookFireOutcome), as a
// fire's does: an admission error can quote the request (a part's name), and
// the request is the sealed file's to hold, never the log's.
func (ds *dataset) spoolDrainFailed(name string, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	metrics.WebhookSpoolDrainFailures.WithLabelValues(ds.Repository().ID).Inc()
	outcome := webhookFireOutcome(err)
	if errors.Is(err, errSpoolUnreadable) {
		ds.svc.log.Error("substrate: a spooled webhook request does not open; it is set aside as "+changelogfile.UnreadableSpoolSuffix,
			"repository", ds.Repository().ID, "file", name)
		return
	}
	ds.svc.log.Warn("substrate: a spooled webhook request could not be recorded; the drain retries",
		"repository", ds.Repository().ID, "file", name, "outcome", outcome)
}

// fireDrained hands a drained request's fire to the background when its
// trigger runs now. A trigger that does not (disabled, deleted, its callable
// gone) leaves the row pending for the dispatcher's resume (resumeWebhooks),
// as an accepted request always has.
func (ds *dataset) fireDrained(got drainedWebhook) {
	tr, _, err := ds.triggerByID(context.Background(), got.trigger)
	if err != nil || !tr.Webhook || !tr.Enabled || !tr.runnable() {
		return
	}
	row := got.row
	if !ds.spawn("webhook fire", func(ctx context.Context) { ds.fireWebhook(ctx, tr, row) }) {
		ds.svc.log.Info("substrate: webhook fire left pending, the service is shutting down",
			"repository", ds.Repository().ID, "trigger", logSafeID(tr.ID), "fire", logSafeID(row.FireID))
	}
}

// spoolFilesForCopy reads every spooled file's bytes, for a copy of the
// directory (the export under the writer mutex, the stopped snapshot). A
// file deleted between the listing and the read was drained: its entry is
// in the changelog the copy holds.
func spoolFilesForCopy(dir string) ([]spoolCopy, error) {
	files, err := changelogfile.SpoolFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]spoolCopy, 0, len(files))
	for _, f := range files {
		data, err := changelogfile.ReadSpoolFile(dir, f.Name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, spoolCopy{name: f.Name, data: data})
	}
	return out, nil
}

// spoolCopy is one spooled file as a copy carries it.
type spoolCopy struct {
	name string
	data []byte
}
