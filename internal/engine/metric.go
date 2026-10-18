package engine

// THE METRIC AND SOURCE CONTRACTS ON THE READ PATH (decision 0154).
//
// An observed value names its metric and its source by reference, and the
// engine reads those records through core's `metric` and `source` traits
// alone: a record whose kind binds neither contributes nothing, whatever
// properties it happens to carry. metricContract is the one reading of a
// metric (its measure, default unit, bucket place and rollup tiers) every
// caller takes; the write path holds both records to their traits' rules
// (checkObserved).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// metricContract reads the live metric record at a record path through core's
// `metric` trait: false where the path names no live record or a record of a
// kind that does not bind the trait, whose properties mean nothing to a read.
func metricContract(ctx context.Context, q rowQuerier, reg *vocabulary.Registry, path string) (vocabulary.MetricContract, bool, error) {
	kind, id, ok := vocabulary.SplitRecordPath(path)
	if !ok {
		return vocabulary.MetricContract{}, false, nil
	}
	if k, ok := reg.ByIdentity(kind); !ok || !k.Implements(vocabulary.TraitMetricCore) {
		return vocabulary.MetricContract{}, false, nil
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT props FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		kind, id).Scan(&raw)
	switch {
	case err == sql.ErrNoRows:
		return vocabulary.MetricContract{}, false, nil
	case err != nil:
		return vocabulary.MetricContract{}, false, fmt.Errorf("substrate/engine: metric %s: %w", path, err)
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return vocabulary.MetricContract{}, false, fmt.Errorf("substrate/engine: metric %s: %w", path, err)
	}
	return vocabulary.MetricContractOf(props), true, nil
}

// bindsSource reports whether a kind binds core's `source`, whose records'
// `priorities` the aggregate read ranks sources by.
func bindsSource(reg *vocabulary.Registry, kind string) bool {
	k, ok := reg.ByIdentity(kind)
	return ok && k.Implements(vocabulary.TraitSourceCore)
}
