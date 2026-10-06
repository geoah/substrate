package engine

// THE BACKGROUND REPROJECTION.
//
// Two of a row's columns are derived from the row and its kind's declaration
// and from nothing else: its refs rows (refs.go deriveRefs) and its `fts`
// (validate.go ftsBands). A declaration that moves a reference site or an
// `fts` flag moves what the kind's stored rows derive to, so the indexes
// must be re-derived under the new declaration, or a rebuild, which folds
// under it, ends at a different store. Every request on a repository waits
// for its open, so the open may do nothing per stored row: the boot upgrade
// re-derives only the declaration rows in its transaction and records every
// other reshaped kind in `index_reprojections` for the pass this file runs
// once the open has published the dataset.
//
// Both derivations are PER TOP-LEVEL PROPERTY: deriveRefs walks each
// declared property's value on its own and ftsBands reads each declared
// property's value on its own, so a row that carries none of the properties
// whose declaration moved derives the same refs rows and the same bands
// under either declaration. The request names those properties, index by
// index, and the pass reads only the rows that carry one, in pages, each
// its own transaction under the shared registry-dependency lock, as the
// open-time search reindex does (searchindex.go). A kind declared on one
// side only names no property, and every row is re-derived.
//
// The request rows are written in the upgrade's own transaction and each
// page records the id it ended at, so a shutdown leaves the rest for the
// next open to find and resume. A write that lands on a row meanwhile
// re-derives that row under the published declaration itself, and a
// rebuild re-derives every row, so neither is undone by the pass: a page
// derives exactly what the write path derives, from the row as it locks it.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// reprojectionBatch bounds one transaction of the background pass: the rows
// of one kind it locks, re-derives and writes before it commits. A write that
// touches one of those rows waits for that commit and no longer.
const reprojectionBatch = 500

// reprojectionPass names the pass in its retry line (retryStep) and in the
// line a stop that outlives the drain budget logs.
const reprojectionPass = "background index re-derivation"

// indexFilter is one index's half of a request: whether the index moved
// for the kind and, if so, which rows re-derive. every means each row, and
// properties is then empty; otherwise the rows carrying one of properties.
type indexFilter struct {
	moved      bool
	every      bool
	properties []string
}

// wants reports whether a row with these properties re-derives this index.
func (f indexFilter) wants(props map[string]any) bool {
	if !f.moved {
		return false
	}
	if f.every {
		return true
	}
	for _, name := range f.properties {
		if _, ok := props[name]; ok {
			return true
		}
	}
	return false
}

// merge widens f by another request for the same index.
func (f indexFilter) merge(o indexFilter) indexFilter {
	switch {
	case !o.moved:
		return f
	case !f.moved:
		return o
	}
	out := indexFilter{moved: true, every: f.every || o.every}
	if !out.every {
		out.properties = unionStrings(f.properties, o.properties)
	}
	return out
}

// stored is the filter as the request row carries it: NULL properties for
// every row, and nothing for an index that did not move.
func (f indexFilter) stored() any {
	if !f.moved || f.every {
		return nil
	}
	return f.properties
}

// indexReprojection is one kind whose derived indexes must be re-derived
// under the published declarations: a filter per index, and how far the
// pass got.
type indexReprojection struct {
	kind      string
	refs, fts indexFilter
	// after is the id the last committed page ended at, empty before the
	// first; the pass resumes after it.
	after string
	// requestedAt is the stored row's, read by the pass so that a page or
	// the finish writes the request it read and not one widened since.
	requestedAt time.Time
}

// every reports whether either index re-derives every row of the kind.
func (r indexReprojection) every() bool {
	return (r.refs.moved && r.refs.every) || (r.fts.moved && r.fts.every)
}

// properties is every property either index reads, for the page filter.
func (r indexReprojection) properties() []string {
	return unionStrings(r.refs.properties, r.fts.properties)
}

// reprojectionSet accumulates the kinds a declaration change reshapes, by
// identity, merging what each comparison finds about the same kind.
type reprojectionSet map[string]*indexReprojection

// add records that a kind's indexes moved, as the two filters say.
func (s reprojectionSet) add(kind string, refs, fts indexFilter) {
	r := s[kind]
	if r == nil {
		r = &indexReprojection{kind: kind}
		s[kind] = r
	}
	r.refs = r.refs.merge(refs)
	r.fts = r.fts.merge(fts)
}

// addMoved compares each named kind between two registries and records
// which of its indexes moved and which properties decide the rows. The
// refs comparison ignores the order properties are declared in, because
// deriveRefs sorts its rows; the fts comparison does not, because ftsBands
// joins the bands in declaration order.
func (s reprojectionSet) addMoved(before, after kindLookup, kinds map[string]bool) {
	for _, ident := range sortedKeys(kinds) {
		refs := filterOf(movedProperties(before, after, ident, referenceShape, propertyReferenceShape, false))
		fts := filterOf(movedProperties(before, after, ident, ftsShape, propertyFTSShape, true))
		if refs.moved || fts.moved {
			s.add(ident, refs, fts)
		}
	}
}

// filterOf is movedProperties' answer as a filter.
func filterOf(props []string, every, moved bool) indexFilter {
	return indexFilter{moved: moved, every: every, properties: props}
}

// kinds lists the set's kinds, sorted, for a log line.
func (s reprojectionSet) kinds() []string { return sortedKeys(s) }

// movedProperties reports whether one kind's shape (kindShape, the
// comparison the apply door classifies with) differs between two registries
// and, if so, which of its top-level properties differ under propShape, or
// that every row must be re-derived: the kind is declared on one side only,
// the body flag moved, or, when ordered, the properties the shape still
// reads on both sides come in a different order, which moves what every
// row derives whether or not it carries a changed property. A kind-level
// difference no property accounts for is no move at all when the order
// does not matter to the derivation.
func movedProperties(before, after kindLookup, ident string,
	kindShape func(kindLookup, string) string, propShape func(string, *vocabulary.Property) string, ordered bool,
) (props []string, every, moved bool) {
	if kindShape(before, ident) == kindShape(after, ident) {
		return nil, false, false
	}
	a, okA := before.ByIdentity(ident)
	b, okB := after.ByIdentity(ident)
	if !okA || !okB {
		return nil, true, true
	}
	names := make(map[string]bool, len(a.Props)+len(b.Props))
	for n := range a.Props {
		names[n] = true
	}
	for n := range b.Props {
		names[n] = true
	}
	changed := map[string]bool{}
	for _, n := range sortedKeys(names) {
		if propertyShapeOf(a, n, propShape) == propertyShapeOf(b, n, propShape) {
			continue
		}
		// `body` lives in its own column, never in `props`, so no property
		// filter can name the rows that carry it.
		if n == substrate.PropBody {
			return nil, true, true
		}
		changed[n] = true
		props = append(props, n)
	}
	if ordered && !slices.Equal(shapedOrder(a, changed, propShape), shapedOrder(b, changed, propShape)) {
		return nil, true, true
	}
	if len(props) == 0 {
		return nil, false, false
	}
	return props, false, true
}

// shapedOrder lists, in declaration order, the properties the shape reads
// whose own shape did not change: the sequence the derivation joins
// unchanged values in.
func shapedOrder(ty *vocabulary.Kind, changed map[string]bool, shape func(string, *vocabulary.Property) string) []string {
	var out []string
	for _, n := range ty.PropOrder {
		if !changed[n] && shape(n, ty.Props[n]) != "" {
			out = append(out, n)
		}
	}
	return out
}

// propertyShapeOf is one property's shape, or the empty string where the
// kind does not declare it.
func propertyShapeOf(ty *vocabulary.Kind, name string, shape func(string, *vocabulary.Property) string) string {
	p, ok := ty.Props[name]
	if !ok {
		return ""
	}
	return shape(name, p)
}

// propertyReferenceShape is the part of one property's declaration that
// deriveRefs reads (appendReferenceShape); referenceShape is these joined in
// declaration order.
func propertyReferenceShape(name string, p *vocabulary.Property) string {
	var b strings.Builder
	appendReferenceShape(&b, name, p)
	return b.String()
}

// propertyFTSShape is the part of one property's declaration that ftsBands
// reads: whether it indexes, and into which band; ftsShape is these joined
// in declaration order, with the body flag after them.
func propertyFTSShape(name string, p *vocabulary.Property) string {
	if !p.FTS || p.Sensitive() {
		return ""
	}
	return fmt.Sprintf("%s|%v;", name, vocabulary.IsLongText(p.Datatype))
}

// kindsOfPackages is every kind the named packages declare on either side.
func kindsOfPackages(a, b *vocabulary.Registry, packages map[string]bool) map[string]bool {
	out := map[string]bool{}
	for aname := range packages {
		for _, reg := range []*vocabulary.Registry{a, b} {
			g, ok := reg.PackageByName(aname)
			if !ok || g == nil {
				continue
			}
			for _, tn := range g.KindOrder {
				out[g.Kinds[tn].Identity] = true
			}
		}
	}
	return out
}

// requestReprojections writes the pass's work for each kind, in the
// transaction that moves the declarations: a request for a kind one is
// already owed for widens it, index by index, and starts it over from the
// first id, so the pass never loses what an earlier change left.
func (t *txn) requestReprojections(rs []indexReprojection) error {
	for _, r := range rs {
		if _, err := t.exec(`
			INSERT INTO index_reprojections (kind, refs, refs_properties, fts, fts_properties)
			VALUES ($1, $2, $3::text[], $4, $5::text[])
			ON CONFLICT (repository, kind) DO UPDATE SET
				refs = index_reprojections.refs OR EXCLUDED.refs,
				refs_properties = `+mergedProperties("refs")+`,
				fts = index_reprojections.fts OR EXCLUDED.fts,
				fts_properties = `+mergedProperties("fts")+`,
				after_id = '',
				requested_at = now()`,
			r.kind, r.refs.moved, r.refs.stored(), r.fts.moved, r.fts.stored()); err != nil {
			return fmt.Errorf("substrate/engine: request the index re-derivation of %s: %w", r.kind, err)
		}
	}
	return nil
}

// mergedProperties is the SQL that widens one index's stored property list
// by a new request's: the new one where the index had not moved, the stored
// one where it does not move now, NULL (every row) where either is, and the
// union otherwise. `index` is one of this file's two column stems, never
// caller input.
func mergedProperties(index string) string {
	col := "index_reprojections." + index + "_properties"
	new := "EXCLUDED." + index + "_properties"
	return `CASE
		WHEN NOT index_reprojections.` + index + ` THEN ` + new + `
		WHEN NOT EXCLUDED.` + index + ` THEN ` + col + `
		WHEN ` + col + ` IS NULL OR ` + new + ` IS NULL THEN NULL
		ELSE (SELECT array_agg(DISTINCT p ORDER BY p) FROM unnest(` + col + ` || ` + new + `) AS p)
	END`
}

// pendingReprojections reads the kinds a pass is owed, sorted.
func (ds *dataset) pendingReprojections(ctx context.Context) ([]indexReprojection, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT kind, refs, refs_properties IS NULL, COALESCE(to_jsonb(refs_properties), '[]'::jsonb),
		       fts, fts_properties IS NULL, COALESCE(to_jsonb(fts_properties), '[]'::jsonb),
		       after_id, requested_at
		FROM index_reprojections ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []indexReprojection
	for rows.Next() {
		var r indexReprojection
		var refsProps, ftsProps []byte
		if err := rows.Scan(&r.kind, &r.refs.moved, &r.refs.every, &refsProps,
			&r.fts.moved, &r.fts.every, &ftsProps, &r.after, &r.requestedAt); err != nil {
			return nil, err
		}
		r.refs.properties, r.fts.properties = jsonStrings(refsProps), jsonStrings(ftsProps)
		out = append(out, r)
	}
	return out, rows.Err()
}

// checkIndexReprojections is the open ladder's step: it reads whether a pass
// is owed, so startIndexReprojection runs one once the open completes. The
// pass does not run here: on a kind of a million rows it takes minutes, and
// the open is what every request on the repository waits for. A read-only
// process writes nothing and serves the indexes it finds.
func (ds *dataset) checkIndexReprojections(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var pending bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM index_reprojections)`).Scan(&pending); err != nil {
		return fmt.Errorf("substrate/engine: repository %s: read the index re-derivations owed: %w", ds.info.ID, err)
	}
	ds.reprojectPending = pending
	return nil
}

// startIndexReprojection starts the pass checkIndexReprojections found owed
// (backgroundPass). The open calls it as it publishes the dataset, and a
// rebuild that failed calls it again.
func (ds *dataset) startIndexReprojection() {
	if !ds.reprojectPending {
		return
	}
	ds.reproject.start(ds, "re-derive the indexes", ds.reprojectIndexes)
}

// stopIndexReprojection cancels the running pass and waits for it to
// return; final is the dataset closing.
func (ds *dataset) stopIndexReprojection(final bool) {
	ds.reproject.stop(ds, final, "the "+reprojectionPass)
}

// reprojectIndexes runs the pass: every kind owed, each in pages of
// reprojectionBatch rows by id, each page its own transaction, until nothing
// is owed. It moves nothing but the refs rows and `fts` of the rows it
// re-derives: no version, no updated_at, no changelog entry, because no
// record changed, only the indexes over it. Reads and writes are served
// throughout, and a row keeps its old index rows until its page commits.
//
// A step that fails is tried again after a pause (retryStep), so a passing
// database error costs a pause and not the rest of the pass. Each page
// records the id it ended at on the request, and the request is deleted
// after the kind's last row, so a pass the close stops is resumed by the
// next open where it was. A row redone twice lands at the same rows.
//
// A page is PLANNED on the pool, outside any lock: its filter walks the
// kind's rows by id and reads each one's properties, and a kind where few
// rows carry the moved property is a long walk that must not hold the
// registry-dependency lock against a vocabulary apply waiting for the
// exclusive side. The page then takes the shared lock, as every data write
// does, so the registry it reads is the published one a write derives
// under, locks its rows FOR UPDATE SKIP LOCKED and derives from the rows as
// locked, so no write lands between its read and its statements; and it
// never waits on a row a write holds: those are redone after the kind's
// pages, one transaction each (searchindex.go has the deadlock this
// avoids). A row that is gone by the time the page locks it is nothing to
// derive.
func (ds *dataset) reprojectIndexes(ctx context.Context) {
	started := time.Now()
	repo := logSafeID(ds.scope.Repository)
	var done int64
	kinds := 0
	stopped := func() {
		ds.svc.log.Info("substrate: the index re-derivation stopped before it finished; the next open resumes it",
			"repository", repo, "kinds", kinds, "rows", done, "elapsed", time.Since(started).Round(time.Millisecond))
	}
	prog := ds.svc.progress("substrate: re-deriving the indexes of a reshaped kind", "repository", repo)
	announced := false
	for {
		var pending []indexReprojection
		if ds.retryStep(ctx, reprojectionPass, "plan", func() error {
			var err error
			pending, err = ds.pendingReprojections(ctx)
			return err
		}) != nil {
			stopped()
			return
		}
		if len(pending) == 0 {
			break
		}
		if !announced {
			announced = true
			names := make([]string, 0, len(pending))
			for _, p := range pending {
				names = append(names, p.kind)
			}
			ds.svc.log.Info("substrate: re-deriving the indexes of the kinds a declaration change reshaped",
				"repository", repo, "kinds", names)
		}
		for _, p := range pending {
			n, err := ds.reprojectKind(ctx, p, func(rows int) {
				done += int64(rows)
				prog.report("kind", p.kind, "rows", done, "elapsed", time.Since(started).Round(time.Millisecond))
			})
			if err != nil {
				stopped()
				return
			}
			if ds.retryStep(ctx, reprojectionPass, "finish "+p.kind, func() error {
				return ds.inRawTx(ctx, func(t *txn) error { return t.finishReprojection(p) })
			}) != nil {
				stopped()
				return
			}
			kinds++
			ds.svc.log.Info("substrate: re-derived the indexes of one kind",
				"repository", repo, "kind", p.kind, "refs", p.refs.moved, "fts", p.fts.moved,
				"properties", p.properties(), "everyRow", p.every(), "rows", n,
				"elapsed", time.Since(started).Round(time.Millisecond))
		}
	}
	if announced {
		ds.svc.log.Info("substrate: re-derived the indexes of the kinds a declaration change reshaped",
			"repository", repo, "kinds", kinds, "rows", done, "took", time.Since(started).Round(time.Millisecond))
	}
}

// reprojectKind re-derives one kind's owed rows from where the request says
// the last page ended: its pages first, then the rows a write held when
// their page came, and reports the rows it wrote to advanced as it goes. It
// returns an error only when ctx ends.
func (ds *dataset) reprojectKind(ctx context.Context, p indexReprojection, advanced func(rows int)) (int, error) {
	redone := 0
	var held []string
	after := p.after
	for {
		var page, missed []string
		var n int
		if err := ds.retryStep(ctx, reprojectionPass, "page of "+p.kind, func() error {
			if hook := ds.svc.testReprojectionHook; hook != nil {
				if err := hook(ctx, p.kind); err != nil {
					return err
				}
			}
			var err error
			if page, err = ds.reprojectionPage(ctx, p, after, reprojectionBatch); err != nil || len(page) == 0 {
				return err
			}
			return ds.inRawTx(ctx, func(t *txn) error {
				if err := t.lockRegistryDepShared(); err != nil {
					return err
				}
				var err error
				if n, missed, err = t.reprojectRows(p, page, true); err != nil {
					return err
				}
				return t.advanceReprojection(p, page[len(page)-1])
			})
		}); err != nil {
			return redone, err
		}
		if len(page) == 0 {
			break
		}
		redone += n
		held = append(held, missed...)
		after = page[len(page)-1]
		advanced(n)
		if len(page) < reprojectionBatch {
			break
		}
	}
	for _, id := range held {
		var n int
		if err := ds.retryStep(ctx, reprojectionPass, "held row of "+p.kind, func() error {
			return ds.inRawTx(ctx, func(t *txn) error {
				if err := t.lockRegistryDepShared(); err != nil {
					return err
				}
				var err error
				n, _, err = t.reprojectRows(p, []string{id}, false)
				return err
			})
		}); err != nil {
			return redone, err
		}
		redone += n
		advanced(n)
	}
	return redone, nil
}

// reprojectionPage plans one page on the pool: the ids of the next batch
// rows of the kind after the id the last page ended at, live and tombstoned,
// that carry one of the moved properties, or any row when every row is
// owed. Locked or not, so a row a write holds is still named and
// reprojectRows can report it.
func (ds *dataset) reprojectionPage(ctx context.Context, p indexReprojection, after string, batch int) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM records
		WHERE kind = $1 AND id > $2 AND ($3::boolean OR props ?| $4::text[])
		ORDER BY id LIMIT $5`, p.kind, after, p.every(), p.properties(), batch)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// reprojectRows locks the rows of the kind named by ids and re-derives the
// owed indexes under the published registry, with the parked kinds behind it
// as the fold reads them (fold.go foldView), and reports how many it wrote
// and which ids it did not lock: a row a write holds, under skipLocked, or
// one that is gone. Each index re-derives the rows its own filter names: a
// row carrying only a property the other index reads derives the same rows
// here, and is not written. A kind the registry no longer declares derives
// no refs rows, so its stale ones go, and its `fts` lands at the
// unknown-kind bands, which is what a rebuild folds for it. The caller holds
// the shared registry-dependency lock.
func (t *txn) reprojectRows(p indexReprojection, ids []string, skipLocked bool) (int, []string, error) {
	lock := ` FOR UPDATE`
	if skipLocked {
		lock += ` SKIP LOCKED`
	}
	rows, err := t.ds.scanRows(t.ctx, t.tx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = ANY($2::text[]) ORDER BY id`+lock,
		[]any{p.kind, ids})
	if err != nil {
		return 0, nil, err
	}
	view := t.liveFoldView()
	ty, _ := view.ByIdentity(p.kind)
	var rederive, reindex []*erow
	for _, row := range rows {
		if p.refs.wants(row.Props) {
			rederive = append(rederive, row)
		}
		if p.fts.wants(row.Props) {
			reindex = append(reindex, row)
		}
	}
	if err := t.syncRefsOfRows(p.kind, ty, rederive); err != nil {
		return 0, nil, err
	}
	if err := t.rederiveFTS(view, p.kind, reindex); err != nil {
		return 0, nil, err
	}
	var missed []string
	if len(rows) < len(ids) {
		locked := make(map[string]bool, len(rows))
		for _, row := range rows {
			locked[row.ID] = true
		}
		for _, id := range ids {
			if !locked[id] {
				missed = append(missed, id)
			}
		}
	}
	return len(rows), missed, nil
}

// advanceReprojection records, in the page's own transaction, the id the
// page ended at, on the request the pass read; a request widened since
// (requested_at moved) starts over from the first id and is left alone.
func (t *txn) advanceReprojection(p indexReprojection, after string) error {
	_, err := t.exec(`UPDATE index_reprojections SET after_id = $3 WHERE kind = $1 AND requested_at = $2`,
		p.kind, p.requestedAt, after)
	return err
}

// finishReprojection deletes the request the pass read for a kind. A request
// written since (requested_at moved) stays, and the next plan finds it.
func (t *txn) finishReprojection(p indexReprojection) error {
	_, err := t.exec(`DELETE FROM index_reprojections WHERE kind = $1 AND requested_at = $2`, p.kind, p.requestedAt)
	return err
}

// retryStep runs one step of a background pass until it succeeds or ctx
// ends, pausing between tries from a second up to searchReindexMaxPause,
// and returns an error only for ctx. Every step is safe to run again: a page
// that failed rolled back, and one redone lands at the same rows. A step
// that can never succeed logs its error at every try, which is how an
// operator hears of it.
func (ds *dataset) retryStep(ctx context.Context, pass, step string, fn func() error) error {
	pause := time.Second
	for {
		err := fn()
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		ds.svc.log.Error("substrate: a "+pass+" step failed; trying it again",
			"repository", logSafeID(ds.scope.Repository), "step", step, "retry_in", pause, "error", err)
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		pause = min(2*pause, searchReindexMaxPause)
	}
}
