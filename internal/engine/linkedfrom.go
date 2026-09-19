package engine

import (
	"context"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The inbound half of a mapping-owned link, on the single-record read.
//
// A recordmapping NAMES its subject reference into being on its SOURCE kind
// (record 0085), so a person is pointed AT by a Slack user, a GitHub user and
// a Google contact, and nothing on the person says so: the slot lives on the
// mirrors. `filter.referencing` answers "who points at this" in general and pages it;
// this is the narrow question the manifest needs — which MIRRORS converged on
// this subject — answered inline, on the read that already opened the record
// (record 0088).
//
// It is the same predicate over the same index the reverse read stands on:
// `referencingWhere` renders the target half, former ids included, so a
// pointer written before a merge counts here exactly as it does there.

// maxLinkedFrom caps how many inbound links one read carries. It is the page
// cap, because that is this repository's bound on how much one read may hand
// back; `filter.referencing` is the complete, paged answer past it.
const maxLinkedFrom = maxPageSize

// linkedFrom reads the source records whose mapping-owned subject slot points
// at this record. It answers nil for a kind no mapping targets — absence is
// the answer "nothing maps onto this kind", which an empty list would not
// say — and an empty non-nil slice never occurs, so the wire key is absent in
// both the no-mapping and the nothing-linked-yet cases.
//
// The record must be canonical: Get resolves the former-id trail before this
// runs, and the trail is re-read here to match pointers written under a
// discarded id.
func (ds *dataset) linkedFrom(ctx context.Context, x dbx, e *substrate.Record) ([]substrate.LinkedRecord, error) {
	mappings := ds.registry().MappingsTo(e.Kind)
	if len(mappings) == 0 {
		return nil, nil
	}
	// One mapping per (source kind, subject property) is the mapping set's own
	// key (record 0049), so the pair identifies the mapping that owns a ref
	// row. A property name is a camel word and a kind reference has no space,
	// so the joined key cannot collide.
	owner := make(map[string]*vocabulary.Mapping, len(mappings))
	pairs := make([]string, 0, len(mappings))
	for _, m := range mappings {
		key := m.From + " " + m.Property
		if _, seen := owner[key]; seen {
			continue
		}
		owner[key] = m
		pairs = append(pairs, key)
	}
	ids, err := ds.idsOf(ctx, x, eref{Kind: e.Kind, ID: e.ID})
	if err != nil {
		return nil, err
	}
	b := &builder{}
	where := referencingWhere(b, eref{Kind: e.Kind, ID: e.ID}, ids, "")
	rows, err := x.QueryContext(ctx,
		`SELECT r.src_kind, r.src, r.property, s.title FROM refs r `+
			`JOIN records s ON s.kind = r.src_kind AND s.id = r.src AND s.deleted_at IS NULL `+
			`WHERE `+where+
			` AND (r.src_kind || ' ' || r.property) IN `+b.jsonArray(pairs)+
			` ORDER BY r.src_kind, r.src, r.property LIMIT `+b.arg(maxLinkedFrom),
		b.args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.LinkedRecord
	for rows.Next() {
		var srcKind, src, property, title string
		if err := rows.Scan(&srcKind, &src, &property, &title); err != nil {
			return nil, err
		}
		m := owner[srcKind+" "+property]
		if m == nil {
			continue
		}
		link := substrate.LinkedRecord{
			Ref:      vocabulary.RecordPath(srcKind, src),
			Kind:     srcKind,
			Title:    title,
			Property: property,
			Mapping:  m.Identity(),
		}
		// A source holding the target under one slot is one link however many
		// ref rows the index kept for it.
		if len(out) > 0 && out[len(out)-1] == link {
			continue
		}
		out = append(out, link)
	}
	return out, rows.Err()
}
