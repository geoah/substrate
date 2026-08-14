package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const (
	// heartbeatInterval keeps idle ndjson streams (and the proxies in front
	// of them) alive.
	heartbeatInterval = 30 * time.Second
	// changeBatch is one Changes() page while draining.
	changeBatch = 500
)

// retentionHorizon is the oldest seq still resumable: a `from=` below it is a
// `compacted` error. Retention is full today, so the horizon is 0
// and nothing has been compacted away — but it is POLICY, not a wire
// guarantee, and clients MUST handle the compacted signal for when a future
// deployment prunes. It is surfaced in GET /.well-known/substrate/server.json
// discovery.
func retentionHorizon() int64 { return 0 }

// ndjson control-frame rule: a line WITHOUT `seq` is a control
// frame identified by its single key. `bookmark` opens a stream, `{}` is an
// idle heartbeat, and the errorEnvelope (`{"error":{…}}`) is the reserved
// TERMINAL error frame — a mid-stream failure travels as one problem object
// rather than a silent EOF.
//
// writeWatchError encodes that terminal frame. A client-gone encode error is
// swallowed: there is no one left to tell.
func writeWatchError(enc *json.Encoder, flusher http.Flusher, err error) {
	_, p := problemFor(err)
	if encErr := enc.Encode(errorEnvelope{Error: p}); encErr != nil {
		return
	}
	flusher.Flush()
}

func (h *handler) getChanges(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	// The feed's filter keys are PLURAL (`kinds`, `ops`, `actors`), so the
	// plausible singular guess used to be dropped in silence and answer with the
	// WHOLE feed looking like a filtered one. Ruling A8: an unsupported
	// parameter is a bad_request naming the key.
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
	from, hasFrom, err := parseFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if !hasFrom {
		from = 0
	}
	if hasFrom && from < retentionHorizon() {
		writeCompacted(w, retentionHorizon())
		return
	}
	changes, err := ds.Changes(r.Context(), from, f, changeBatch)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	rows, err := annotateChanges(r.Context(), ds, changes)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]int64{"bookmark": from})
	for i := range rows {
		if err := enc.Encode(rows[i]); err != nil {
			return
		}
	}
}

// streamChanges writes the ndjson watch stream: a bookmark line, then every
// matching change from that cursor on, then heartbeats while idle. annotate
// attaches per-function delivery states to each row — the cross-collection
// feed's contract; per-collection watches stay plain rows. Every row the
// filter matches is emitted: a token reads its whole repository, so there is
// no per-row gate left to apply.
func (h *handler) streamChanges(w http.ResponseWriter, r *http.Request, ds substrate.Dataset, f substrate.ChangeFilter, annotate bool) {
	ctx := r.Context()
	from, hasFrom, err := parseFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// The horizon check happens before any 200/stream bytes, so a compacted
	// resume is a clean 410, not a terminal frame.
	if hasFrom && from < retentionHorizon() {
		writeCompacted(w, retentionHorizon())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal, "streaming unsupported")
		return
	}

	// Subscribe before establishing the head so nothing committed in
	// between is missed.
	signals := ds.WatchSignal(ctx)

	if !hasFrom {
		head, err := headSeq(ctx, ds)
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		from = head
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	if err := enc.Encode(map[string]int64{"bookmark": from}); err != nil {
		return
	}
	flusher.Flush()

	cursor := from
	// drain returns the substrate error that ended the stream (for the
	// terminal error frame), errClientGone when the client's socket died (no
	// one to frame to), or nil when the batch is exhausted cleanly.
	drain := func() error {
		for {
			changes, err := ds.Changes(ctx, cursor, f, changeBatch)
			if err != nil {
				return err
			}
			rows := make([]changeRow, len(changes))
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
				if err := enc.Encode(rows[i]); err != nil {
					return errClientGone
				}
			}
			flusher.Flush()
			if len(changes) < changeBatch {
				return nil
			}
		}
	}
	if err := drain(); err != nil {
		if !errors.Is(err, errClientGone) {
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
		case _, open := <-signals:
			if !open {
				return
			}
			if err := drain(); err != nil {
				if !errors.Is(err, errClientGone) {
					writeWatchError(enc, flusher, err)
				}
				return
			}
		case <-ticker.C:
			if _, err := w.Write([]byte("{}\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// headSeq is the changelog's highest committed seq. The Dataset contract
// exposes no direct "latest seq" read and only pages forward, so the head
// is found by an exponential then binary probe: O(changelog head) single-row
// reads instead of a walk whose length is the changelog's.
func headSeq(ctx context.Context, ds substrate.Dataset) (int64, error) {
	more := func(after int64) (bool, error) {
		changes, err := ds.Changes(ctx, after, substrate.ChangeFilter{}, 1)
		if err != nil {
			return false, err
		}
		return len(changes) > 0, nil
	}
	// Invariant: more(lo) is true, more(hi) is false, so head is in (lo,hi].
	if ok, err := more(0); err != nil || !ok {
		return 0, err
	}
	lo, hi := int64(0), int64(1)
	for {
		ok, err := more(hi)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		lo = hi
		if hi > math.MaxInt64/2 {
			hi = math.MaxInt64
			break
		}
		hi *= 2
	}
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		ok, err := more(mid)
		if err != nil {
			return 0, err
		}
		if ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi, nil
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
