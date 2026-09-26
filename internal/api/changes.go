package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/geoah/substrate/internal/substrate"
)

// annotateChanges attaches each enabled trigger's stance on every row.
func annotateChanges(ctx context.Context, ds substrate.Dataset, changes []substrate.Change) ([]substrate.ChangeRow, error) {
	rows := make([]substrate.ChangeRow, len(changes))
	for i := range changes {
		rows[i].Change = changes[i]
	}
	states, err := ds.ChangeTriggers(ctx, changes)
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
	before, head, first, ok := historyStart(w, r, ds)
	if !ok {
		return
	}
	// One storage read per page: every row the filter matches is readable, so
	// the page the query returns is the page the client gets.
	kept := []substrate.ChangeRow{}
	cur := before
	exhausted := false
	changes, err := ds.ChangesBefore(r.Context(), cur, f, first)
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
	// `head` and `generation` are the watch handoff, as on a list envelope:
	// `watch?from={head}&generation={generation}` tails what this page did not
	// hold.
	body := substrate.ChangePage{Changes: kept, Head: head.Seq, Generation: head.Generation}
	switch {
	case len(kept) > first:
		// Overshoot: return the first `first` readable rows and set the cursor to
		// the LAST returned row's seq, so the walk resumes strictly below it —
		// the trimmed readable rows are re-fetched next page, never skipped.
		body.Changes = kept[:first]
		body.Cursor = kept[first-1].Seq
	case !exhausted:
		// A full page with more rows below: resume under the oldest seq
		// consumed.
		body.Cursor = cur
	}
	writeJSON(w, http.StatusOK, body)
}

// getChangeRuns serves the history as run summaries (`runs=1`): `first`
// counts runs instead of rows, and each run is read to its end, so the oldest
// one on the page carries its whole count even when it reaches far below the
// others. The walk reads the filtered feed in batches and stops at the row
// that would start run first+1, which is also how it knows more lie below.
// Its cost is linear in the rows the page's runs hold.
func (h *handler) getChangeRuns(w http.ResponseWriter, r *http.Request, ds substrate.Dataset, f substrate.ChangeFilter) {
	before, head, first, ok := historyStart(w, r, ds)
	if !ok {
		return
	}
	runs := []substrate.ChangeRun{}
	var records map[string]struct{}
	more := false
	cur := before
walk:
	for {
		changes, err := ds.ChangesBefore(r.Context(), cur, f, changeBatch)
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		for _, c := range changes {
			verb := substrate.RunVerb(c)
			if n := len(runs); n > 0 {
				last := &runs[n-1]
				if last.Actor == c.Actor && last.Kind == c.Kind && last.Verb == verb {
					last.Count++
					last.OldestSeq, last.OldestTS = c.Seq, c.TS
					if _, seen := records[c.RecordID]; !seen {
						records[c.RecordID] = struct{}{}
						last.Records++
						last.RecordID = ""
					}
					continue
				}
				if n == first {
					more = true
					break walk
				}
			}
			records = map[string]struct{}{c.RecordID: {}}
			runs = append(runs, substrate.ChangeRun{
				Actor: c.Actor, Kind: c.Kind, Verb: verb, Count: 1, Records: 1, RecordID: c.RecordID,
				NewestSeq: c.Seq, OldestSeq: c.Seq, NewestTS: c.TS, OldestTS: c.TS,
			})
		}
		if len(changes) < changeBatch {
			break
		}
		cur = changes[len(changes)-1].Seq
	}
	body := substrate.ChangeRunPage{Runs: runs, Head: head.Seq, Generation: head.Generation}
	if more {
		body.Cursor = runs[len(runs)-1].OldestSeq
	}
	writeJSON(w, http.StatusOK, body)
}

// historyStart reads and checks what every backward history read opens with:
// the `before` cursor held to the retention horizon, the head and the
// generation, and the `first` page size. It writes the refusal itself and
// answers false when the request cannot be served.
func historyStart(w http.ResponseWriter, r *http.Request, ds substrate.Dataset) (int64, substrate.ChangelogHead, int, bool) {
	before, err := parseSeqParam(r, "before")
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return 0, substrate.ChangelogHead{}, 0, false
	}
	// The head is read BEFORE the page: a row committed between the two reads
	// then lands above the head the body reports, so a client resuming a
	// watch at `head` sees it again rather than never. The other order would
	// hide it under a head the page never saw.
	head, err := ds.Head(r.Context())
	if err != nil {
		writeSubstrateError(w, err)
		return 0, substrate.ChangelogHead{}, 0, false
	}
	// The horizon binds both ends of the cursor contract, not just the forward
	// one: walking back below it is exactly the "you can no longer address
	// this" case `compacted` exists for, and answering with an empty 200 would
	// let a client mistake a pruned range for the start of the changelog. The
	// generation binds both ends too: a `before` continuation names an entry
	// of ONE history, and a client that fetched a page, then had an older
	// directory restored under it, would otherwise walk on through the
	// replacement's rows as if they were the ones it had been reading.
	// `before=0` is the head of whatever history is there and names nothing.
	generation := r.URL.Query().Get("generation")
	switch {
	case before < 0 || (before > 0 && before < retentionHorizon()):
		writeCompacted(w, head, fmt.Sprintf("seq %d is below the retention horizon %d; re-list and resume from the head", before, retentionHorizon()))
		return 0, substrate.ChangelogHead{}, 0, false
	case before > head.Seq:
		writeCompacted(w, head, fmt.Sprintf("seq %d is above the head %d: this changelog never reached the cursor; re-list and resume from the head", before, head.Seq))
		return 0, substrate.ChangelogHead{}, 0, false
	case generation != "" && generation != head.Generation:
		writeCompacted(w, head, fmt.Sprintf("generation %q is not this changelog's %q: the history was replaced since the cursor was saved; re-list and resume from the head", generation, head.Generation))
		return 0, substrate.ChangelogHead{}, 0, false
	case before > 0 && generation == "":
		writeCompacted(w, head, fmt.Sprintf("before=%d names an entry and needs the generation it was read under; the head is %d under generation %q; re-list and resume from it", before, head.Seq, head.Generation))
		return 0, substrate.ChangelogHead{}, 0, false
	}
	first, err := parseFirstParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return 0, substrate.ChangelogHead{}, 0, false
	}
	return before, head, first, true
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
