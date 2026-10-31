package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// changeBatch is one Changes() page while draining.
const changeBatch = 500

// heartbeatInterval keeps idle ndjson streams (and the proxies in front of
// them) alive. A variable only so a test can wait less than the interval.
var heartbeatInterval = 30 * time.Second

// streamWriteDeadline is how long one frame of a stream, or one write of the
// export, may take to reach its client. The server sets no WriteTimeout,
// which would cut every stream at a fixed age, so this is what frees a
// handler whose client stopped reading: the stalled write fails and the
// stream ends. Twice the heartbeat, so an idle stream never comes near it. A
// variable only so a test can wait less than a minute for a stall.
var streamWriteDeadline = 60 * time.Second

// isDraining reports whether the server has started to shut down
// (Config.Draining).
func (h *handler) isDraining() bool {
	select {
	case <-h.draining:
		return true
	default:
		return false
	}
}

// retentionHorizon is the oldest seq still resumable: a `from=` below it is a
// `compacted` error. Retention is full today, so the horizon is 0
// and nothing has been compacted away — but it is POLICY, not a wire
// guarantee, and clients MUST handle the compacted signal for when a future
// deployment prunes. It is surfaced in GET /.well-known/substrate/server.json
// discovery.
func retentionHorizon() int64 { return 0 }

// ndjson control-frame rule: a line WITHOUT `seq` is a control
// frame identified by its key. `bookmark` opens a stream (with the history
// `generation` beside it), `{}` is an idle heartbeat, and the substrate.ErrorEnvelope
// (`{"error":{…}}`) is the reserved TERMINAL error frame: a mid-stream
// failure travels as one problem object rather than a silent EOF.
//
// setStreamHeaders opens a live ndjson stream. Every line is flushed as it is
// written, and the two proxy headers keep whatever sits in front of the server
// from undoing that: nginx and its ingress buffer a response unless
// `X-Accel-Buffering: no` says otherwise, and `no-transform` refuses the
// compression an intermediary would hold lines back for.
func setStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
}

// writeWatchError encodes that terminal frame. A client-gone encode error is
// swallowed: there is no one left to tell.
func writeWatchError(enc *json.Encoder, flusher http.Flusher, err error) {
	_, p := problemFor(err)
	if encErr := enc.Encode(substrate.ErrorEnvelope{Error: p}); encErr != nil {
		return
	}
	flusher.Flush()
}

func (h *handler) getChanges(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	// The feed's filter keys are PLURAL (`kinds`, `ops`, `actors`), and a
	// plausible singular guess dropped in silence would answer with the WHOLE
	// feed looking like a filtered one. An unsupported parameter is a
	// bad_request naming the key.
	if bad := unsupportedParam(r, changeParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	f, err := parseChangeFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	q := r.URL.Query()
	if scoped, ok := ds.(*scopedDataset); ok {
		h.watchScoped(w, r, scoped, f)
		return
	}
	if q.Has("runs") {
		// Run summaries are a history read: they walk backward from `before`
		// and have no forward or streaming form.
		switch {
		case q.Get("runs") != "1":
			writeError(w, http.StatusBadRequest, codeBadRequest, "runs: the only value is 1")
		case q.Has("watch") || q.Has("from"):
			writeError(w, http.StatusBadRequest, codeBadRequest, "runs: a history read, paged with before; it takes neither watch nor from")
		default:
			h.getChangeRuns(w, r, ds, f)
		}
		return
	}
	if q.Get("watch") == "1" {
		h.streamChanges(w, r, ds, f, true)
		return
	}
	// `before`/`first` select the newest-first history page; the parameterless
	// forward read below predates it and external consumers page it by `from`.
	if q.Has("before") || q.Has("first") {
		h.getChangesPage(w, r, ds, f)
		return
	}
	rs, ok := resumeCursor(w, r, ds)
	if !ok {
		return
	}
	changes, err := ds.Changes(r.Context(), rs.from, f, changeBatch)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	rows, err := annotateChanges(r.Context(), ds, changes)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	enc := json.NewEncoder(w)
	_ = enc.Encode(bookmarkFrame{Bookmark: rs.from, Generation: rs.head.Generation})
	for i := range rows {
		if err := enc.Encode(rows[i]); err != nil {
			return
		}
	}
}

// watchScoped is the change feed for an app's token (decision 0195): the
// live tail alone, held to the records the grant reads, each row carrying
// names alone. History pages, run summaries and the forward read stay the
// owner's; so do `q` and `values=1`, which read across kinds and into
// payloads. Everything is refused before the stream opens.
func (h *handler) watchScoped(w http.ResponseWriter, r *http.Request, ds *scopedDataset, f substrate.ChangeFilter) {
	q := r.URL.Query()
	if q.Get("watch") != "1" || q.Has("runs") || q.Has("before") || q.Has("first") {
		writeSubstrateError(w, r, errScope("the change feed's history: its live tail is watch=1"))
		return
	}
	if _, _, err := ds.changeFilter(r.Context(), f); err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	h.streamChanges(w, r, ds, f, false)
}

// streamChanges writes the ndjson watch stream: a bookmark line, then every
// matching change from that cursor on, then heartbeats while idle. annotate
// attaches per-function delivery states to each row — the cross-collection
// feed's contract; per-collection watches stay plain rows. Every row the
// filter matches is emitted for a full-access token, which reads its whole
// repository; an app's token gets the rows its grant reads (readChanges).
//
// The stream ends cleanly, after a flush, when the server starts to shut
// down (Config.Draining): every row written is a row the client holds, so it
// resumes from the last seq it read. Each frame has streamWriteDeadline to
// reach the client.
func (h *handler) streamChanges(w http.ResponseWriter, r *http.Request, ds substrate.Dataset, f substrate.ChangeFilter, annotate bool) {
	ctx := r.Context()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal, "streaming unsupported")
		return
	}
	rc := http.NewResponseController(w)
	// frame gives the next write its own deadline. A writer that cannot take
	// one (a test recorder) writes without.
	frame := func() { _ = rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline)) }

	// Subscribe before reading the head so nothing committed in between is
	// missed. The cursor check runs before any 200/stream bytes, so a
	// refused resume is a clean 410, not a terminal frame.
	signals := ds.WatchSignal(ctx)
	rs, ok := resumeCursor(w, r, ds)
	if !ok {
		return
	}
	from := rs.from
	if !rs.hasFrom {
		from = rs.head.Seq
	}

	setStreamHeaders(w)
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	frame()
	if err := enc.Encode(bookmarkFrame{Bookmark: from, Generation: rs.head.Generation}); err != nil {
		return
	}
	flusher.Flush()

	cursor := from
	// drain returns the substrate error that ended the stream (for the
	// terminal error frame), errClientGone when the client's socket died (no
	// one to frame to), or nil when the batch is exhausted cleanly.
	drain := func() error {
		for {
			changes, scanned, last, err := readChanges(ctx, ds, cursor, f, changeBatch)
			if err != nil {
				return err
			}
			rows := make([]substrate.ChangeRow, len(changes))
			for i := range changes {
				rows[i].Change = changes[i]
			}
			if annotate {
				if rows, err = annotateChanges(ctx, ds, changes); err != nil {
					return err
				}
			}
			for i := range rows {
				cursor = rows[i].Seq
				frame()
				if err := enc.Encode(rows[i]); err != nil {
					return errClientGone
				}
			}
			flusher.Flush()
			cursor = last
			// A backlog stops between batches once the server is
			// shutting down; the loop below then ends the stream.
			if scanned < changeBatch || h.isDraining() {
				return nil
			}
		}
	}
	if err := drain(); err != nil {
		if !errors.Is(err, errClientGone) {
			frame()
			writeWatchError(enc, flusher, err)
		}
		return
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.draining:
			flusher.Flush()
			return
		case _, open := <-signals:
			if !open {
				return
			}
			if err := drain(); err != nil {
				if !errors.Is(err, errClientGone) {
					frame()
					writeWatchError(enc, flusher, err)
				}
				return
			}
		case <-ticker.C:
			frame()
			if _, err := w.Write([]byte("{}\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// readChanges is one batch of the tail: the rows to write, how many rows the
// batch read and the highest seq among them. An app's batch writes fewer rows
// than it reads (heldChanges), and the cursor moves past the ones it held
// back, or every signal would read them again.
func readChanges(ctx context.Context, ds substrate.Dataset, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, int, int64, error) {
	if scoped, ok := ds.(*scopedDataset); ok {
		return scoped.heldChanges(ctx, after, f, limit)
	}
	changes, err := ds.Changes(ctx, after, f, limit)
	if err != nil || len(changes) == 0 {
		return changes, 0, after, err
	}
	return changes, len(changes), changes[len(changes)-1].Seq, nil
}

// bookmarkFrame opens every stream and every forward page: the seq the rows
// that follow are read after, and the history generation that seq belongs to.
// A client saves the pair and resumes with `from={bookmark}&generation={…}`.
type bookmarkFrame struct {
	Bookmark   int64  `json:"bookmark"`
	Generation string `json:"generation"`
}

// resume is a checked `from` cursor: the head it was held to, the seq to read
// after, and whether the request supplied one at all.
type resume struct {
	head    substrate.ChangelogHead
	from    int64
	hasFrom bool
}

// resumeCursor reads the `from` cursor and holds it to the changelog before a
// byte of the response is written. A cursor is a seq under a history
// generation, and it resumes only while the generation is the repository's
// and the seq is at or below the head: an imported directory recreates a
// repository under the same authority with seqs that restart below whatever
// cursors clients saved, and a bare seq cannot tell that history from this one
// (decision 0056). Seq 0 is the start of every history and names no entry, so
// it needs no generation. Anything else answers 410 `compacted` naming the
// head to re-list from; a parse failure is the 400 it always was. ok is false
// once the response has been written.
func resumeCursor(w http.ResponseWriter, r *http.Request, ds substrate.Dataset) (resume, bool) {
	from, hasFrom, err := parseFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return resume{}, false
	}
	head, err := ds.Head(r.Context())
	if err != nil {
		writeSubstrateError(w, r, err)
		return resume{}, false
	}
	generation := r.URL.Query().Get("generation")
	refuse := func(msg string) (resume, bool) {
		writeCompacted(w, head, msg+"; re-list and resume from the head")
		return resume{}, false
	}
	switch {
	case hasFrom && from < retentionHorizon():
		return refuse(fmt.Sprintf("seq %d is below the retention horizon %d", from, retentionHorizon()))
	case generation != "" && generation != head.Generation:
		return refuse(fmt.Sprintf("generation %q is not this changelog's %q: the history was replaced since the cursor was saved", generation, head.Generation))
	case hasFrom && from > 0 && generation == "":
		return refuse(fmt.Sprintf("from=%d names an entry and needs the generation it was read under; the head is %d under generation %q", from, head.Seq, head.Generation))
	case hasFrom && from > head.Seq:
		return refuse(fmt.Sprintf("seq %d is above the head %d: this changelog never reached the cursor", from, head.Seq))
	}
	return resume{head: head, from: from, hasFrom: hasFrom}, true
}

func parseFrom(r *http.Request) (int64, bool, error) {
	raw := r.URL.Query().Get("from")
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, errFrom
	}
	return n, true, nil
}

var errFrom = &parseError{"from: not a sequence number"}

// errClientGone signals a failed ndjson encode (the client's socket died):
// there is no terminal error frame to write, so the stream just ends.
var errClientGone = &parseError{"watch client gone"}

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }

func parseChangeFilter(r *http.Request) (substrate.ChangeFilter, error) {
	v := r.URL.Query()
	// `recordId` is a bare id and an id is NOT unique — two kinds may share
	// one. Scoping the feed to a single record therefore takes BOTH `recordId`
	// and `recordKind`; the id alone would conflate distinct records' feeds.
	// Require the companion rather than silently over-matching.
	recordID := v.Get("recordId")
	recordKind := v.Get("recordKind")
	if recordID != "" && recordKind == "" {
		return substrate.ChangeFilter{}, &parseError{"recordId requires recordKind — an id alone does not name one record"}
	}
	if recordKind != "" && recordID == "" {
		return substrate.ChangeFilter{}, &parseError{"recordKind requires recordId — the pair scopes the feed to one record"}
	}
	f := substrate.ChangeFilter{RecordID: recordID, Q: v.Get("q")}
	// `values=1` asks every row for its records' before and after values
	// (decision 0135). Any other spelling is refused rather than read as
	// "no", so a client that meant to ask never silently gets names alone.
	switch raw := v.Get("values"); raw {
	case "", "0":
	case "1":
		f.Values = true
	default:
		return substrate.ChangeFilter{}, &parseError{"values: 1 asks for before and after values, 0 or absent does not; got " + strconv.Quote(raw)}
	}
	if recordKind != "" {
		f.Kinds = append(f.Kinds, recordKind)
	}
	f.Kinds = append(f.Kinds, splitList(v["kinds"])...)
	f.ExcludeKinds = append(f.ExcludeKinds, splitList(v["excludeKinds"])...)
	for _, a := range splitList(v["actors"]) {
		f.Actors = append(f.Actors, substrate.Actor(a))
	}
	for _, a := range splitList(v["excludeActors"]) {
		f.ExcludeActors = append(f.ExcludeActors, substrate.Actor(a))
	}
	for _, o := range splitList(v["ops"]) {
		f.Ops = append(f.Ops, substrate.Op(o))
	}
	for _, o := range splitList(v["excludeOps"]) {
		f.ExcludeOps = append(f.ExcludeOps, substrate.Op(o))
	}
	return f, nil
}

// splitList accepts both spellings clients use: a repeated query parameter
// and a comma-separated list (and any mix of the two).
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
