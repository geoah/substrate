package engine

import (
	"context"
	"fmt"
	"sync"
)

// The measured-value checks a write may run beyond folding its own rows: the
// unit graph and the quantity declarations it publishes, the metric rules,
// the observed index's settle, and the observed index's probe on a record
// that leaves the fold. The feature is optional and experimental, so each
// check is skipped where nothing it judges was written: a write to a kind
// with no measured values runs none. The probe still runs for a record of a
// kind that does not bind `observed` but holds a segment, written while it
// did (observedKinds). measured is the seam a test counts them through; it
// does nothing in the server.
const (
	measureUnitGraph     = "unit graph"
	measureDeclaredUnits = "declared units"
	measureMetricValues  = "metric values"
	measureObserved      = "observed settle"
	measureObservedProbe = "observed probe"
	measureUnitUses      = "unit uses"
)

// measured reports a measured-value check that is about to do work.
func (t *txn) measured(check string) {
	if h := t.ds.svc.testMeasureHook; h != nil {
		h(t.actor, check)
	}
}

// observedKinds is the kinds of a repository that hold an observed segment,
// or did since the dataset opened: a record of a kind outside it leaves the
// fold without asking observed_segments. It is read once, at open
// (loadObservedKinds), and the observed settle adds a kind before it writes
// the kind's first segment (writeSegmentBatch), so it never misses a kind a
// segment was written for; a kind whose segments all went stays in it, which
// costs only the probe it would have saved. Until it is read, every kind may
// hold one.
type observedKinds struct {
	mu     sync.Mutex
	loaded bool
	kinds  map[string]bool
}

// mayHold reports whether a record of kind may own a segment.
func (o *observedKinds) mayHold(kind string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.loaded || o.kinds[kind]
}

// note records that kind holds a segment.
func (o *observedKinds) note(kind string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.kinds == nil {
		o.kinds = map[string]bool{}
	}
	o.kinds[kind] = true
}

// observedKindsQuery lists the kinds holding a segment, one index probe per
// kind over the table's primary key rather than a scan of its rows.
const observedKindsQuery = `
	WITH RECURSIVE k(kind) AS (
		SELECT min(kind) FROM observed_segments
		UNION ALL
		SELECT (SELECT min(s.kind) FROM observed_segments s WHERE s.kind > k.kind) FROM k WHERE k.kind IS NOT NULL
	)
	SELECT kind FROM k WHERE kind IS NOT NULL`

// loadObservedKinds is the open ladder's step that reads observedKinds, so
// no write asks the table which kinds hold a segment.
func (ds *dataset) loadObservedKinds(ctx context.Context) error {
	rows, err := ds.db.QueryContext(ctx, observedKindsQuery)
	if err != nil {
		return fmt.Errorf("substrate/engine: repository %s: read the kinds holding observed segments: %w", ds.info.ID, err)
	}
	defer func() { _ = rows.Close() }()
	kinds := map[string]bool{}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return err
		}
		kinds[kind] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	ds.observedKinds.mu.Lock()
	defer ds.observedKinds.mu.Unlock()
	for kind := range ds.observedKinds.kinds {
		kinds[kind] = true
	}
	ds.observedKinds.kinds, ds.observedKinds.loaded = kinds, true
	return nil
}
