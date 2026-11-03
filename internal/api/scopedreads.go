package api

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The reads decision 0219 opens to an app's token beside the list: the
// window and aggregate reads over the kinds it reads, held exactly as a list
// is, and a blob's bytes where a record it reads names the blob.

// holdQuery holds a records query to the grant: its kinds narrowed to the
// ones the grant reads (an empty list becomes every readable kind, a named
// kind it cannot read is unknown), then by `implements` and `purposes`
// against those alone; a condition or an order on a trigger's source, a
// reference condition into a kind it cannot read, or a referencing target of
// one, refused. ok is false when no kind is left, which the caller answers
// with an empty result rather than passing on, because no kinds means every
// kind below here.
func (s *scopedDataset) holdQuery(ctx context.Context, q substrate.Query) (substrate.Query, bool, error) {
	kinds, any, err := s.narrow(ctx, q.Filter.Kinds)
	if err != nil {
		return q, false, err
	}
	if any {
		kinds, any, err = s.narrowByRegistry(ctx, kinds, q.Filter.Implements, q.Filter.Purposes)
		if err != nil {
			return q, false, err
		}
	}
	if !any {
		return q, false, nil
	}
	q.Filter.Kinds = kinds
	if err := checkTriggerSource(kinds, q); err != nil {
		return q, false, err
	}
	if err := s.checkReferenceFilters(ctx, kinds, q.Filter.Properties); err != nil {
		return q, false, err
	}
	if ref := q.Filter.Referencing; ref != nil {
		for _, target := range append([]string{ref.Ref}, ref.Refs...) {
			if target == "" {
				continue
			}
			if err := s.requireRead(ctx, kindOfPath(target)); err != nil {
				return q, false, err
			}
		}
	}
	return q, true, nil
}

// refuseRowGated refuses a read that names a kind the agent verbs reach row
// by row: those reads narrow a list record by record, and a window, an
// aggregate or a search has no row to narrow by.
func (s *scopedDataset) refuseRowGated(ctx context.Context, kinds []string) error {
	for _, k := range kinds {
		if gated, err := s.rowGated(ctx, k); err != nil {
			return err
		} else if gated {
			return fmt.Errorf("%w: an app's token reads %s through a list, which its agent scopes narrow record by record",
				substrate.ErrForbidden, k)
		}
	}
	return nil
}

// Window is the window read held to the grant: the rows, the series and the
// overrides of the kinds it reads. Every record handed back is cut as a
// list's are, an override of a kind the grant cannot read included: the
// window then reads as if that override were never written, which is how
// every hidden record reads. The head is left out as on a list; the
// generation stays, since the cursor is held to it and the tail's bookmark
// discloses it anyway (decision 0195).
func (s *scopedDataset) Window(ctx context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error) {
	if err := s.refuseRowGated(ctx, q.Filter.Kinds); err != nil {
		return nil, err
	}
	held, any, err := s.holdQuery(ctx, substrate.Query{Filter: q.Filter})
	if err != nil {
		return nil, err
	}
	if !any {
		return &substrate.WindowPage{Rows: []*substrate.Record{}}, nil
	}
	q.Filter = held.Filter
	wp, err := s.inner.Window(ctx, q)
	if err != nil {
		return nil, err
	}
	out := *wp
	out.Head = 0
	if out.Rows, err = s.cutRecords(ctx, wp.Rows); err != nil {
		return nil, err
	}
	if out.Series, err = s.cutRecords(ctx, wp.Series); err != nil {
		return nil, err
	}
	if out.Overrides, err = s.cutRecords(ctx, wp.Overrides); err != nil {
		return nil, err
	}
	if out.Included, err = s.cutIncluded(ctx, wp.Included); err != nil {
		return nil, err
	}
	return &out, nil
}

// kindMetric is the metric record an aggregate's metric reference names.
const kindMetric = vocabulary.PackageObservations + "/metric"

// Aggregate is the aggregate read held to the grant: the filtered kinds
// narrowed as a list's are, a metric read over every readable kind cut to
// the ones the observed index holds, and each metric's own record readable,
// since the answer carries its unit. The sources an answer names are record
// paths, so they are cut to the kinds the grant reads, and the head is left
// out as on a list.
func (s *scopedDataset) Aggregate(ctx context.Context, q substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	if err := s.refuseRowGated(ctx, q.Filter.Kinds); err != nil {
		return nil, err
	}
	a := q.Aggregate
	if a != nil {
		var refs []string
		if a.Metric != nil {
			refs = append(refs, a.Metric.Ref)
		}
		for _, m := range a.Metrics {
			refs = append(refs, m.Ref)
		}
		for _, ref := range refs {
			kind := kindOfPath(ref)
			if kind == "" {
				kind = kindMetric
			}
			if err := s.requireRead(ctx, kind); err != nil {
				return nil, err
			}
		}
	}
	named := len(q.Filter.Kinds) > 0
	held, any, err := s.holdQuery(ctx, substrate.Query{Filter: q.Filter})
	if err != nil {
		return nil, err
	}
	empty := &substrate.AggregateResult{Rows: []substrate.AggregateRow{}, Exact: true}
	if !any {
		return empty, nil
	}
	q.Filter = held.Filter
	if a != nil && a.Property == nil && !named {
		// The engine refuses a metric read naming a kind the observed index
		// does not hold, and an app that named none asked for every kind it
		// reads: the ones the index holds are what it meant.
		observed, err := s.implementers(ctx, vocabulary.TraitObservedCore)
		if err != nil {
			return nil, err
		}
		q.Filter.Kinds = slices.DeleteFunc(slices.Clone(q.Filter.Kinds), func(k string) bool { return !slices.Contains(observed, k) })
		if len(q.Filter.Kinds) == 0 {
			return empty, nil
		}
	}
	res, err := s.inner.Aggregate(ctx, q)
	if err != nil {
		return nil, err
	}
	out := *res
	out.Head = substrate.AggregateHead{}
	if out.Sources, err = s.readablePaths(ctx, res.Sources); err != nil {
		return nil, err
	}
	out.Rows = slices.Clone(res.Rows)
	for i := range out.Rows {
		if out.Rows[i].Sources, err = s.readablePaths(ctx, res.Rows[i].Sources); err != nil {
			return nil, err
		}
		if g := res.Rows[i].Group; g != nil && g.Source != "" {
			if ok, err := s.reads(ctx, kindOfPath(g.Source)); err != nil {
				return nil, err
			} else if !ok {
				cut := *g
				cut.Source = ""
				out.Rows[i].Group = &cut
			}
		}
	}
	return &out, nil
}

// readablePaths keeps the record paths of kinds the grant reads; nil stays
// nil, so an answer that named no source still names none.
func (s *scopedDataset) readablePaths(ctx context.Context, paths []string) ([]string, error) {
	if paths == nil {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		ok, err := s.reads(ctx, kindOfPath(p))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// cutRecords cuts every record the grant reads and drops the rest.
func (s *scopedDataset) cutRecords(ctx context.Context, recs []*substrate.Record) ([]*substrate.Record, error) {
	out := make([]*substrate.Record, 0, len(recs))
	for _, rec := range recs {
		if ok, err := s.visible(ctx, rec); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		c, err := s.cut(ctx, rec)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// cutIncluded is cutRecords over a page's expanded referents, keyed by path.
func (s *scopedDataset) cutIncluded(ctx context.Context, included map[string]*substrate.Record) (map[string]*substrate.Record, error) {
	if included == nil {
		return nil, nil
	}
	out := make(map[string]*substrate.Record, len(included))
	for path, rec := range included {
		if rec == nil {
			continue
		}
		if ok, err := s.visible(ctx, rec); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		c, err := s.cut(ctx, rec)
		if err != nil {
			return nil, err
		}
		out[path] = c
	}
	return out, nil
}

// --- blobs ---

// blobSite is one blob-ref property of a kind the grant reads.
type blobSite struct {
	kind, property string
	repeated       bool
}

// blobRefProperties lists a kind's blob-ref properties. A blob-ref is never
// a field of an object, so the top level is all of them.
func blobRefProperties(k substrate.KindInfo) []blobSite {
	props, _ := k.Definition["properties"].(map[string]any)
	var out []blobSite
	for name, raw := range props {
		p, _ := raw.(map[string]any)
		if t, _ := p["type"].(string); t == string(vocabulary.DatatypeBlobRef) {
			repeated, _ := p["repeated"].(bool)
			out = append(out, blobSite{kind: k.Identity, property: name, repeated: repeated})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].property < out[j].property })
	return out
}

// declaresBlobRef reports whether a kind declares a blob-ref property.
func declaresBlobRef(k substrate.KindInfo) bool { return len(blobRefProperties(k)) > 0 }

// requireBlob is the blob read rule (decision 0219): a live record of a kind
// the grant reads names the blob through a blob-ref property. A grant that
// reads no such kind reaches no blob at all; one that does is answered, for
// a blob no record it reads names, exactly what the engine answers for a
// blob nobody stored, so the read says nothing about the blobs it cannot
// reach. The engine filters each kind before it reads, so a record of a kind
// the grant cannot read is never looked at.
func (s *scopedDataset) requireBlob(ctx context.Context, digest string) error {
	reg, err := s.kinds(ctx)
	if err != nil {
		return err
	}
	var sites []blobSite
	for id, k := range reg {
		if s.grant.Reads(id, internalKind(k)) {
			sites = append(sites, blobRefProperties(k)...)
		}
	}
	if len(sites) == 0 {
		return errScope("blobs")
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].kind != sites[j].kind {
			return sites[i].kind < sites[j].kind
		}
		return sites[i].property < sites[j].property
	})
	for _, site := range sites {
		cond := substrate.Cond{Eq: digest}
		if site.repeated {
			cond = substrate.Cond{Contains: digest}
		}
		page, err := s.inner.List(ctx, substrate.Query{
			Filter: substrate.Filter{Kinds: []string{site.kind}, Properties: map[string]substrate.Cond{site.property: cond}},
			First:  1,
		})
		if err != nil {
			return err
		}
		if len(page.Records) > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: blob %s", substrate.ErrNotFound, digest)
}

// cutBlobInfo is a blob's manifest as the grant may see it: the name and
// media type are whatever its first upload said, which a token that does
// not read the blob manifests never learns (decision 0202), as a blob
// reference it reads carries neither.
func (s *scopedDataset) cutBlobInfo(info *substrate.BlobInfo) *substrate.BlobInfo {
	if info == nil || s.grant.Reads(kindBlob, true) {
		return info
	}
	cut := *info
	cut.Name, cut.MediaType = "", ""
	return &cut
}

// GetBlob streams a blob a record the grant reads names (requireBlob).
func (s *scopedDataset) GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	if err := s.requireBlob(ctx, digest); err != nil {
		return nil, nil, err
	}
	info, data, err := s.inner.GetBlob(ctx, digest)
	if err != nil {
		return nil, nil, err
	}
	return s.cutBlobInfo(info), data, nil
}

// StatBlob is GetBlob's manifest without the bytes, under the same rule.
func (s *scopedDataset) StatBlob(ctx context.Context, digest string) (*substrate.BlobInfo, error) {
	if err := s.requireBlob(ctx, digest); err != nil {
		return nil, err
	}
	info, err := s.inner.StatBlob(ctx, digest)
	if err != nil {
		return nil, err
	}
	return s.cutBlobInfo(info), nil
}
