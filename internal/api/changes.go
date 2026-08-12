package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/geoah/substrate/internal/substrate"
)

// changeFeedOps is the engine's cross-collection feed seam, asserted at
// runtime like automationOps: substrate.Dataset stays frozen, a dataset
// without the seam serves the forward reads and plain rows only.
type changeFeedOps interface {
	ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error)
	ChangeTriggers(ctx context.Context, changes []substrate.Change) (map[int64][]substrate.ChangeTrigger, error)
}

// changeRow is the /changes wire row: the change plus each enabled trigger's
// stance on it. Triggers is omitted when no trigger matches — absence means
// "nothing listens", never "unknown".
type changeRow struct {
	substrate.Change
	Triggers []substrate.ChangeTrigger `json:"triggers,omitempty"`
}

// annotateChanges attaches per-trigger delivery states through the feed
// seam; a dataset without it serves plain rows rather than failing the read.
func annotateChanges(ctx context.Context, ds substrate.Dataset, changes []substrate.Change) ([]changeRow, error) {
	rows := make([]changeRow, len(changes))
	for i := range changes {
		rows[i].Change = changes[i]
	}
	ops, ok := ds.(changeFeedOps)
	if !ok {
		return rows, nil
	}
	states, err := ops.ChangeTriggers(ctx, changes)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Triggers = states[rows[i].Seq]
	}
	return rows, nil
}

// getChangesPage serves the feed's history: seq < before, newest first, one
// JSON body. History is a page and the live tail is the watch — the two
// address the same rows from opposite ends, so a client walks backward with
// `before` and resumes forward with `from`.
func (h *handler) getChangesPage(w http.ResponseWriter, r *http.Request, ds substrate.Dataset, f substrate.ChangeFilter) {
	ops, ok := ds.(changeFeedOps)
	if !ok {
		writeUnsupported(w, "this substrate serves no change feed")
		return
	}
	before, err := parseSeqParam(r, "before")
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// The horizon binds both ends of the cursor contract, not just the forward
	// one: walking back below it is exactly the "you can no longer address
	// this" case `compacted` exists for, and answering with an empty 200 would
	// let a client mistake a pruned range for the start of the changelog.
	if before > 0 && before < retentionHorizon() {
		writeCompacted(w, retentionHorizon())
		return
	}
	first, err := parseFirstParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// One storage read per page: every row the filter matches is readable, so
	// the page the query returns is the page the client gets.
	kept := []changeRow{}
	cur := before
	exhausted := false
	changes, err := ops.ChangesBefore(r.Context(), cur, f, first)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	if len(changes) < first {
		// Fewer than a full batch: no more matching rows lie below, so the
		// walk has reached the bottom.
		exhausted = true
	}
	if len(changes) > 0 {
		cur = changes[len(changes)-1].Seq
		rows, err := annotateChanges(r.Context(), ds, changes)
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		kept = append(kept, rows...)
	}
	// The history walks backward, newest-first. `cursor` is the continuation
	//: the seq the client passes as the next `before`. It is omitted
	// only when the walk reached the bottom with room to spare (absence = done).
	body := map[string]any{"changes": kept}
	switch {
	case len(kept) > first:
		// Overshoot: return the first `first` readable rows and set the cursor to
		// the LAST returned row's seq, so the walk resumes strictly below it —
		// the trimmed readable rows are re-fetched next page, never skipped.
		kept = kept[:first]
		body["changes"] = kept
		body["cursor"] = kept[first-1].Seq
	case !exhausted:
		// A full page with more rows below: resume under the oldest seq
		// consumed.
		body["cursor"] = cur
	}
	writeJSON(w, http.StatusOK, body)
}

// parseSeqParam reads one optional sequence-number parameter; absent is 0.
func parseSeqParam(r *http.Request, name string) (int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, &parseError{name + ": not a sequence number"}
	}
	return n, nil
}

// parseFirstParam reads the history page size: default 50, capped at the
// same batch bound the watch drains by.
func parseFirstParam(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("first")
	if raw == "" {
		return 50, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, &parseError{"first: not a positive number"}
	}
	return min(n, changeBatch), nil
}
