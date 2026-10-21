package engine

// THE REPOSITORY'S UNIT GRAPH (decision 0186): the unit and conversion
// records of one repository, read into an immutable vocabulary.UnitGraph per
// graph revision.
//
// The revision is a counter in `unit_graph` that every fold of a unit or
// conversion row moves, in the write's transaction (bumpUnitRevision). A
// reader takes it inside its own snapshot and asks the service's cache for
// the graph built at that revision, building it from the same snapshot on a
// miss, so a read that began before a conversion committed converts with the
// graph it began under. A transaction that folds a unit or conversion row
// builds a graph of its own from what it has written, again at its next read
// after each such fold, and the cache receives it only after the commit: a
// rollback publishes nothing.
//
// The transaction's end (checkUnitGraph, from inTx) refuses a graph that
// names a problem the graph before it did not, and refuses deleting a unit a
// stored value names or leaving two used units apart that were one linear
// component. What a value names is `unit_uses`, a table the fold keeps from
// each row's properties (syncUnitUses), and the refs index for a reference
// to a unit record.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// isUnitGraphKind reports whether rows of the kind are read into the graph.
func isUnitGraphKind(kind string) bool {
	return kind == vocabulary.KindUnit || kind == vocabulary.KindConversion
}

// unitGraphAt is one repository's graph at one revision, with the problems
// BuildUnitGraph and the record readers named for it.
type unitGraphAt struct {
	revision int64
	graph    *vocabulary.UnitGraph
	problems []vocabulary.UnitProblem
}

// problemSet is the problems' texts, the identity a transaction's end
// compares two graphs' problems by.
func (at *unitGraphAt) problemSet() map[string]bool {
	out := make(map[string]bool, len(at.problems))
	for _, p := range at.problems {
		out[p.Error()] = true
	}
	return out
}

// The cache's bounds: a repository keeps its two newest revisions, which is
// what a read that began before a commit and one that began after it need
// at once, and the process keeps at most unitGraphsPerProcess graphs, the
// least recently used one going first.
const (
	unitGraphsPerRepository = 2
	unitGraphsPerProcess    = 256
)

// unitGraphCache holds built graphs by repository and revision. The zero
// value is ready.
type unitGraphCache struct {
	mu    sync.Mutex
	repos map[string][]*unitGraphEntry
	total int
	tick  uint64
}

type unitGraphEntry struct {
	at   *unitGraphAt
	used uint64
}

// get answers the graph built for the repository at the revision, or nil.
func (c *unitGraphCache) get(repo string, revision int64) *unitGraphAt {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.repos[repo] {
		if e.at.revision == revision {
			c.tick++
			e.used = c.tick
			return e.at
		}
	}
	return nil
}

// put keeps a graph under its revision. A graph for a revision already held
// leaves the held one in place, and one older than both held revisions is
// dropped, so a build that finishes late never replaces a newer graph.
func (c *unitGraphCache) put(repo string, at *unitGraphAt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.repos == nil {
		c.repos = map[string][]*unitGraphEntry{}
	}
	list := c.repos[repo]
	for _, e := range list {
		if e.at.revision == at.revision {
			return
		}
	}
	c.tick++
	list = append(list, &unitGraphEntry{at: at, used: c.tick})
	sort.Slice(list, func(i, j int) bool { return list[i].at.revision > list[j].at.revision })
	c.total++
	if len(list) > unitGraphsPerRepository {
		// The dropped slots are cleared, or the backing array keeps their
		// graphs reachable past what total counts.
		clear(list[unitGraphsPerRepository:])
		c.total -= len(list) - unitGraphsPerRepository
		list = list[:unitGraphsPerRepository]
	}
	c.repos[repo] = list
	for c.total > unitGraphsPerProcess {
		c.evictOne()
	}
}

// evictOne drops the least recently used graph of the whole process.
func (c *unitGraphCache) evictOne() {
	var oldRepo string
	oldIdx := -1
	var oldUsed uint64
	for repo, list := range c.repos {
		for i, e := range list {
			if oldIdx < 0 || e.used < oldUsed {
				oldRepo, oldIdx, oldUsed = repo, i, e.used
			}
		}
	}
	if oldIdx < 0 {
		c.total = 0
		return
	}
	// slices.Delete clears the vacated tail slot, so the backing array keeps
	// no pointer at the evicted graph.
	list := slices.Delete(c.repos[oldRepo], oldIdx, oldIdx+1)
	if len(list) == 0 {
		delete(c.repos, oldRepo)
	} else {
		c.repos[oldRepo] = list
	}
	c.total--
}

// forget drops every graph of a repository: one whose rows were replayed or
// erased, where a revision may come back holding other records.
func (c *unitGraphCache) forget(repo string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total -= len(c.repos[repo])
	delete(c.repos, repo)
}

// readUnitRevision is the repository's graph revision as q sees it; a
// repository whose fold never wrote a unit or conversion row is at 0.
func readUnitRevision(ctx context.Context, q dbx) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx, `SELECT revision FROM unit_graph`).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("substrate/engine: read the unit graph revision: %w", err)
	}
	return rev, nil
}

// loadUnitGraph builds the graph from the live unit and conversion rows q
// sees. A row the record readers refuse is a problem of the graph and stays
// out of it; the write path refuses such a row, so only a row an older
// binary admitted can be one.
func loadUnitGraph(ctx context.Context, q dbx, revision int64) (*unitGraphAt, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT kind, id, props FROM records
		WHERE kind = ANY($1::text[]) AND deleted_at IS NULL
		ORDER BY kind, id`, []string{vocabulary.KindUnit, vocabulary.KindConversion})
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the unit graph: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var units []vocabulary.UnitRecord
	var convs []vocabulary.ConversionRecord
	var problems []vocabulary.UnitProblem
	for rows.Next() {
		var kind, id string
		var raw []byte
		if err := rows.Scan(&kind, &id, &raw); err != nil {
			return nil, err
		}
		props, err := decodeNumberPreserving(raw)
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: read %s %s: %w", kind, id, err)
		}
		u, c, err := readUnitGraphRecord(kind, id, props)
		switch {
		case err != nil:
			problems = append(problems, vocabulary.UnitProblem{Kind: kind, ID: id, Message: err.Error()})
		case u != nil:
			units = append(units, *u)
		case c != nil:
			convs = append(convs, *c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	g, built := vocabulary.BuildUnitGraph(units, convs)
	return &unitGraphAt{revision: revision, graph: g, problems: append(problems, built...)}, nil
}

// readUnitGraphRecord reads one row of either kind.
func readUnitGraphRecord(kind, id string, props map[string]any) (*vocabulary.UnitRecord, *vocabulary.ConversionRecord, error) {
	switch kind {
	case vocabulary.KindUnit:
		u, err := vocabulary.CheckUnitRecord(id, props)
		if err != nil {
			return nil, nil, err
		}
		return &u, nil, nil
	case vocabulary.KindConversion:
		c, err := vocabulary.CheckConversionRecord(id, props)
		if err != nil {
			return nil, nil, err
		}
		return nil, &c, nil
	}
	return nil, nil, nil
}

// unitGraph is the repository's graph as q sees it: the revision and, on a
// cache miss, the rows, both read through q. q must hold one snapshot for
// both reads (a REPEATABLE READ transaction, or a write transaction holding
// the changelog lock, which no other writer commits under).
func (ds *dataset) unitGraph(ctx context.Context, q dbx) (*unitGraphAt, error) {
	rev, err := readUnitRevision(ctx, q)
	if err != nil {
		return nil, err
	}
	if at := ds.svc.unitGraphs.get(ds.scope.Repository, rev); at != nil {
		return at, nil
	}
	if hook := ds.svc.unitGraphLoadHook; hook != nil {
		hook()
	}
	at, err := loadUnitGraph(ctx, q, rev)
	if err != nil {
		return nil, err
	}
	ds.svc.unitGraphs.put(ds.scope.Repository, at)
	return at, nil
}

// unitGraph is the graph this transaction reads: the committed one while it
// has folded no unit or conversion row, else one built from its own writes,
// again after each such fold.
func (t *txn) unitGraph() (*unitGraphAt, error) {
	if !t.unitTouched {
		return t.ds.unitGraph(t.ctx, t.tx)
	}
	if t.unitLocal != nil {
		return t.unitLocal, nil
	}
	rev, err := readUnitRevision(t.ctx, t.tx)
	if err != nil {
		return nil, err
	}
	at, err := loadUnitGraph(t.ctx, t.tx, rev)
	if err != nil {
		return nil, err
	}
	t.unitLocal = at
	return at, nil
}

// beforeUnitFold runs ahead of a fold that changes a unit or conversion row:
// the first one takes the graph as the transaction found it, which the end
// of the transaction judges its own against. A replay judges nothing.
func (t *txn) beforeUnitFold(kind string) error {
	if !isUnitGraphKind(kind) || t.replaying || t.unitBefore != nil {
		return nil
	}
	at, err := t.ds.unitGraph(t.ctx, t.tx)
	if err != nil {
		return err
	}
	t.unitBefore = at
	return nil
}

// afterUnitFold runs once a fold changed a unit or conversion row: the
// revision moves and the transaction's own graph is stale.
func (t *txn) afterUnitFold(kind string) error {
	if !isUnitGraphKind(kind) {
		return nil
	}
	t.unitTouched = true
	t.unitLocal = nil
	if _, err := t.exec(`
		INSERT INTO unit_graph (revision) VALUES (1)
		ON CONFLICT (repository) DO UPDATE SET revision = unit_graph.revision + 1`); err != nil {
		return fmt.Errorf("substrate/engine: move the unit graph revision: %w", err)
	}
	return nil
}

// --- what a row uses ------------------------------------------------------------

// rowUnitUses is every unit spelling a row's properties store, sorted and
// once each: the unit of every quantity-shaped value at any depth (an object
// with a string `value` and a non-empty string `unit`, which is the quantity
// datatype's stored shape, nested fields, lists and link properties
// included), and the top-level `unit` of a metric or a packed observed
// record. It reads the values and not the declaration, so a row derives the
// same spellings under any declaration of its kind; only the two top-level
// cases read the kind's bindings.
//
// Each spelling is kept as its vocabulary.UnitSpellingKey, not as stored: a
// metric keeps the `µg` its writer sent, and the guard probes a unit's
// spellings by the same key, so `µg`, `μg` and `ug` are one use of ug.
func rowUnitUses(ty *vocabulary.Kind, props map[string]any) []string {
	seen := map[string]bool{}
	for _, v := range props {
		collectQuantityUnits(v, seen)
	}
	if readsTopLevelUnit(ty) {
		if u, ok := props[vocabulary.QuantityUnit].(string); ok {
			addUnitUse(seen, u)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// readsTopLevelUnit reports whether rowUnitUses reads a row's top-level
// `unit` under the kind: one that binds packed `observed`, whose `unit` is
// every point's. A metric's `unit` is a reference to a unit record since the
// switch (decision 0186), which the guard reads from `refs`. It is the one
// part of the declaration the derivation reads, so a declaration that moves
// it re-derives the kind's rows (reprojection.go unitUsesShape).
func readsTopLevelUnit(ty *vocabulary.Kind) bool {
	if ty == nil {
		return false
	}
	b, observed := observedBinding(ty)
	return observed && b.Variant == vocabulary.ObservedPacked
}

// unitUsesShape is readsTopLevelUnit for a kind of a registry; a kind it
// does not declare reads none.
func unitUsesShape(reg kindLookup, ident string) bool {
	ty, ok := reg.ByIdentity(ident)
	return ok && readsTopLevelUnit(ty)
}

func collectQuantityUnits(v any, seen map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t[vocabulary.QuantityValue].(string); ok {
			if u, ok := t[vocabulary.QuantityUnit].(string); ok {
				addUnitUse(seen, u)
			}
		}
		for _, inner := range t {
			collectQuantityUnits(inner, seen)
		}
	case []any:
		for _, inner := range t {
			collectQuantityUnits(inner, seen)
		}
	}
}

// addUnitUse keeps one stored spelling under its key; a blank one names no
// unit.
func addUnitUse(seen map[string]bool, spelling string) {
	if key := vocabulary.UnitSpellingKey(spelling); key != "" {
		seen[key] = true
	}
}

// syncUnitUses replaces a row's unit_uses rows when what it uses moved. A
// row the fold just created has none to delete.
func (t *txn) syncUnitUses(ref eref, before, after []string, created bool) error {
	if slices.Equal(before, after) {
		return nil
	}
	return t.replaceUnitUses(ref, after, !created)
}

// replaceUnitUses writes a row's unit_uses rows as uses, deleting what it
// held first unless the row is new.
func (t *txn) replaceUnitUses(ref eref, uses []string, held bool) error {
	if held {
		if _, err := t.exec(`DELETE FROM unit_uses WHERE kind = $1 AND record_id = $2`, ref.Kind, ref.ID); err != nil {
			return fmt.Errorf("substrate/engine: unit uses of %s %s: %w", ref.Kind, ref.ID, err)
		}
	}
	if len(uses) == 0 {
		return nil
	}
	if _, err := t.exec(`
		INSERT INTO unit_uses (kind, record_id, unit)
		SELECT $1, $2, u FROM unnest($3::text[]) u`, ref.Kind, ref.ID, uses); err != nil {
		return fmt.Errorf("substrate/engine: unit uses of %s %s: %w", ref.Kind, ref.ID, err)
	}
	return nil
}

// unitUsesVersion is the rule rowUnitUses derives by. A repository whose
// unit_uses_index row is behind it, its rows written before the table
// existed or under an older rule, has every row's uses derived again behind
// the open (checkUnitUses); a change to rowUnitUses moves it. Version 2
// keys each spelling by vocabulary.UnitSpellingKey; version 3 stops reading a
// metric's top-level `unit`, a reference since step 4 of decision 0186.
const unitUsesVersion = 3

// checkUnitUses is the open ladder's step: a repository whose unit uses are
// behind this binary's rule asks the pass behind the open to derive every
// row's again, and records the version in the same transaction, as
// checkObservedIndex does for the observed index. Until the pass finishes,
// the use guard refuses what it cannot judge (guardUnitUses).
func (ds *dataset) checkUnitUses(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	var have int
	err := ds.db.QueryRowContext(ctx, `SELECT version FROM unit_uses_index`).Scan(&have)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		have = 0
	case err != nil:
		return fmt.Errorf("substrate/engine: repository %s: read the unit uses version: %w", ds.info.ID, err)
	}
	if have >= unitUsesVersion {
		return nil
	}
	var kinds []string
	err = ds.inRawTx(ctx, func(t *txn) error {
		rows, err := t.query(`SELECT DISTINCT kind FROM records ORDER BY kind`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var kind string
			if err := rows.Scan(&kind); err != nil {
				_ = rows.Close()
				return err
			}
			kinds = append(kinds, kind)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		owed := make([]indexReprojection, len(kinds))
		for i, kind := range kinds {
			owed[i] = indexReprojection{kind: kind, units: indexFilter{moved: true, every: true}}
		}
		if err := t.requestReprojections(owed); err != nil {
			return err
		}
		return t.markUnitUsesDerived()
	})
	if err != nil {
		return fmt.Errorf("substrate/engine: repository %s: request the unit uses derivation: %w", ds.info.ID, err)
	}
	if len(kinds) > 0 {
		ds.svc.log.Info("substrate: the unit uses were derived under older rules; deriving them behind the open",
			"repository", logSafeID(ds.info.ID), "from", have, "to", unitUsesVersion, "kinds", len(kinds))
	}
	return nil
}

// markUnitUsesDerived records that every row's unit uses follow this
// binary's rule: a creation, whose rows the fold derived, a replay, which
// derives every row's, and the request above, which leaves the rest to the
// pass.
func (t *txn) markUnitUsesDerived() error {
	_, err := t.exec(`
		INSERT INTO unit_uses_index (version) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET version = EXCLUDED.version`, unitUsesVersion)
	return err
}

// unitUsesPending reports whether the pass behind the open still owes some
// row its unit uses.
func (t *txn) unitUsesPending() (bool, error) {
	var pending bool
	err := t.row(`SELECT EXISTS (SELECT 1 FROM index_reprojections WHERE units)`).Scan(&pending)
	return pending, err
}

// --- the transaction's end ----------------------------------------------------

// checkUnitRow holds one written unit or conversion record to its shape, the
// way the trait rules hold an observed record.
func checkUnitRow(kind string, row *erow) error {
	if !isUnitGraphKind(kind) || row.DeletedAt != nil {
		return nil
	}
	_, _, err := readUnitGraphRecord(kind, row.ID, row.Props)
	if err == nil {
		return nil
	}
	msg := err.Error()
	if key, _, ok := strings.Cut(msg, ":"); ok && !strings.ContainsAny(key, " \"") {
		msg = "props." + msg
	}
	return &substrate.ValidationError{Problems: []string{msg}}
}

// checkUnitGraph is the end of a transaction that folded a unit or
// conversion row. It refuses a graph that names a problem the graph the
// transaction found did not, then runs the use guard, and hands the graph to
// the cache once the transaction commits.
func (t *txn) checkUnitGraph() error {
	if !t.unitTouched || t.migrating {
		return nil
	}
	after, err := t.unitGraph()
	if err != nil {
		return err
	}
	before := t.unitBefore
	if before == nil {
		before = &unitGraphAt{graph: emptyUnitGraph()}
	}
	known := before.problemSet()
	var problems []string
	for _, p := range after.problems {
		if !known[p.Error()] {
			known[p.Error()] = true
			problems = append(problems, p.Error())
		}
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	if err := t.guardUnitUses(before.graph, after.graph); err != nil {
		return err
	}
	if err := t.queueCircularRederivation(before.graph, after.graph); err != nil {
		return err
	}
	repo := t.ds.scope.Repository
	cache := &t.ds.svc.unitGraphs
	t.afterCommit = append(t.afterCommit, func() { cache.put(repo, after) })
	return nil
}

func emptyUnitGraph() *vocabulary.UnitGraph {
	g, _ := vocabulary.BuildUnitGraph(nil, nil)
	return g
}

// guardUnitUses refuses, against the graph the transaction found (was) and
// the one it leaves (now), a deleted unit a stored value names, a spelling
// that stops naming the unit a stored value meant by it, and two used units
// of one linear component that end apart. A changed factor passes: every
// later read converts through it, and no stored value moves.
//
// A unit is used when a stored value names it, tombstones included because a
// restore brings the value back: a row's quantities (unit_uses), a reference
// to the unit record from anything but a conversion (refs), and a
// declaration's listed `units`, bounds and default. Before the units switch
// (decision 0186, step 4) a stored value holds a table code, so each stored
// spelling is read through the graph the transaction found.
func (t *txn) guardUnitUses(was, now *vocabulary.UnitGraph) error {
	affected := affectedUnits(was, now)
	if len(affected) == 0 {
		return nil
	}
	// A repository whose rows predate unit_uses has them derived behind the
	// open; until that finishes a use may be missing, so nothing a use could
	// refuse is admitted.
	if pending, err := t.unitUsesPending(); err != nil {
		return err
	} else if pending {
		return fmt.Errorf("%w: the units stored values use are still being derived behind the open; retry the write once that finishes", substrate.ErrConflict)
	}
	used, err := t.usedUnitSpellings(was, affected)
	if err != nil {
		return err
	}
	var problems []string
	// unit id (as was reads it) -> one place a value names it
	where := map[string]string{}
	for _, s := range sortedKeys(used) {
		id, ok := was.Resolve(s)
		if !ok || !affected[id] {
			continue
		}
		into, still := now.Resolve(s)
		switch {
		case !still:
			problems = append(problems, fmt.Sprintf("%s/%s: %s names it, so it cannot be deleted; delete or purge those values first",
				vocabulary.KindUnit, id, describeUse(s, id, used[s])))
		case into != id:
			problems = append(problems, fmt.Sprintf("%s/%s: %s names it, and %q would name %s instead",
				vocabulary.KindUnit, id, describeUse(s, id, used[s]), s, into))
		default:
			if _, held := where[id]; !held {
				where[id] = describeUse(s, id, used[s])
			}
		}
	}
	// Two used units one linear component held must still share one.
	byRoot := map[string][]string{}
	for _, id := range sortedKeys(where) {
		root, _ := was.Root(id)
		byRoot[root] = append(byRoot[root], id)
	}
	for _, root := range sortedKeys(byRoot) {
		ids := byRoot[root]
		first := ids[0]
		firstRoot, _ := now.Root(first)
		for _, id := range ids[1:] {
			if r, _ := now.Root(id); r != firstRoot {
				problems = append(problems, fmt.Sprintf("%s/%s: the transaction leaves %s and %s apart, and stored values use both (%s; %s); a value in one would no longer convert to the other",
					vocabulary.KindUnit, id, first, id, where[first], where[id]))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", substrate.ErrConflict, strings.Join(problems, "; "))
}

func describeUse(spelling, id, place string) string {
	if spelling == id {
		return place
	}
	return place + " (as " + spelling + ")"
}

// affectedUnits are the units of was the transaction may have taken from a
// stored value: each one now lacks, each whose spellings now resolve to
// another unit or to none, and every member of a linear component now
// splits.
func affectedUnits(was, now *vocabulary.UnitGraph) map[string]bool {
	out := map[string]bool{}
	byRoot := map[string][]string{}
	for _, u := range was.Units() {
		root, _ := was.Root(u.ID)
		byRoot[root] = append(byRoot[root], u.ID)
		if _, ok := now.Unit(u.ID); !ok {
			out[u.ID] = true
			continue
		}
		for _, s := range unitSpellings(u) {
			if id, ok := now.Resolve(s); !ok || id != u.ID {
				out[u.ID] = true
				break
			}
		}
	}
	for _, members := range byRoot {
		roots := map[string]bool{}
		for _, id := range members {
			if r, ok := now.Root(id); ok {
				roots[r] = true
			}
		}
		if len(roots) > 1 {
			for _, id := range members {
				out[id] = true
			}
		}
	}
	return out
}

// unitSpellings are the spellings a unit record answers as written.
func unitSpellings(u vocabulary.UnitRecord) []string {
	out := []string{u.ID}
	if u.Code != "" {
		out = append(out, u.Code)
	}
	return append(out, u.Aliases...)
}

// usedUnitSpellings finds which spellings of the affected units a stored
// value or a declaration names, each with one place that names it. A
// spelling is probed and answered as its vocabulary.UnitSpellingKey, the key
// unit_uses holds, so a value stored as `µg` is found by the unit whose code
// is `ug`.
func (t *txn) usedUnitSpellings(was *vocabulary.UnitGraph, affected map[string]bool) (map[string]string, error) {
	keys := map[string]bool{}
	for _, id := range sortedKeys(affected) {
		u, _ := was.Unit(id)
		for _, s := range unitSpellings(u) {
			if key := vocabulary.UnitSpellingKey(s); key != "" {
				keys[key] = true
			}
		}
	}
	spellings := sortedKeys(keys)
	used := map[string]string{}
	rows, err := t.query(`
		SELECT s, k FROM unnest($1::text[]) s,
			LATERAL (SELECT kind AS k FROM unit_uses WHERE unit = s LIMIT 1) u`, spellings)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read unit uses: %w", err)
	}
	for rows.Next() {
		var s, kind string
		if err := rows.Scan(&s, &kind); err != nil {
			_ = rows.Close()
			return nil, err
		}
		used[s] = "a stored value of " + kind
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	refRows, err := t.query(`
		SELECT DISTINCT ON (dst) dst, src_kind FROM refs
		WHERE dst_kind = $1 AND src_kind <> $2 AND dst = ANY($3::text[])
		ORDER BY dst, src_kind`, vocabulary.KindUnit, vocabulary.KindConversion, sortedKeys(affected))
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read unit references: %w", err)
	}
	for refRows.Next() {
		var dst, kind string
		if err := refRows.Scan(&dst, &kind); err != nil {
			_ = refRows.Close()
			return nil, err
		}
		if _, held := used[dst]; !held {
			used[dst] = "a reference from " + kind
		}
	}
	if err := errors.Join(refRows.Err(), refRows.Close()); err != nil {
		return nil, err
	}
	for s, place := range declaredUnitSpellings(t.declarations()) {
		if _, held := used[s]; !held {
			used[s] = place
		}
	}
	return used, nil
}

// declaredUnitSpellings is every unit spelling a declaration names, by its
// vocabulary.UnitSpellingKey: a quantity property's `unit`, its listed
// `units`, its bounds and its default, at any depth, each with the property
// that names it.
func declaredUnitSpellings(reg *vocabulary.Registry) map[string]string {
	out := map[string]string{}
	if reg == nil {
		return out
	}
	var walk func(kind, path string, p *vocabulary.Property)
	walk = func(kind, path string, p *vocabulary.Property) {
		if p == nil {
			return
		}
		place := "the declaration of " + kind + " " + path
		add := func(s string) {
			key := vocabulary.UnitSpellingKey(s)
			if _, held := out[key]; !held && key != "" {
				out[key] = place
			}
		}
		if q := p.Quantity; q != nil {
			add(q.Unit)
			for _, u := range q.Units {
				add(u)
			}
			if q.Min != nil {
				add(q.Min.Unit)
			}
			if q.Max != nil {
				add(q.Max.Unit)
			}
		}
		if p.Datatype == vocabulary.DatatypeQuantity && p.Default != nil {
			seen := map[string]bool{}
			collectQuantityUnits(p.Default, seen)
			for _, s := range sortedKeys(seen) {
				add(s)
			}
		}
		for _, name := range sortedKeys(p.Fields) {
			walk(kind, path+"."+name, p.Fields[name])
		}
		for _, name := range sortedKeys(p.Properties) {
			walk(kind, path+"."+name, p.Properties[name])
		}
	}
	for _, k := range reg.Kinds() {
		for _, name := range sortedKeys(k.Props) {
			walk(k.Identity, name, k.Props[name])
		}
	}
	return out
}

// --- the repository's answer --------------------------------------------------

// Units answers the repository's units and conversions at one revision, read
// in one snapshot: each unit with its linear component's root and its exact
// map to it, each conversion as written, and the shipped records the last
// seed skipped (unitseed.go).
func (ds *dataset) Units(ctx context.Context) (substrate.UnitTable, error) {
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return substrate.UnitTable{}, err
	}
	defer func() { _ = tx.Rollback() }()
	at, err := ds.unitGraph(ctx, tx)
	if err != nil {
		return substrate.UnitTable{}, err
	}
	return ds.unitTable(at), nil
}

func (ds *dataset) unitTable(at *unitGraphAt) substrate.UnitTable {
	out := graphTable(at.graph, at.revision)
	held := map[string]bool{}
	for _, u := range out.Units {
		held[vocabulary.KindUnit+"/"+u.ID] = true
	}
	for _, c := range out.Conversions {
		held[vocabulary.KindConversion+"/"+c.ID] = true
	}
	for _, s := range ds.unseededUnits() {
		if !s.absent || !held[s.rec.Kind+"/"+s.rec.ID] {
			out.Unseeded = append(out.Unseeded, s.rec)
		}
	}
	return out
}

// graphTable is one graph as GET /api/v1/units answers it, with no unseeded
// record: every unit with its root and exact map to it, and every
// conversion.
func graphTable(g *vocabulary.UnitGraph, revision int64) substrate.UnitTable {
	out := substrate.UnitTable{
		Units:       []substrate.RepositoryUnit{},
		Conversions: []substrate.RepositoryConversion{},
		Revision:    revision,
		Unseeded:    []substrate.UnseededRecord{},
	}
	for _, u := range g.Units() {
		ru := substrate.RepositoryUnit{
			ID: u.ID, Name: u.Name, Symbol: u.Symbol, Code: u.Code,
			Aliases: append([]string{}, u.Aliases...), Scale: string(u.Scale),
		}
		if turn := g.Turn(u.ID); turn != nil {
			ru.Turn = vocabulary.FormatRational(turn)
		}
		ru.Root, _ = g.Root(u.ID)
		if f, o, ok := g.ToRoot(u.ID); ok {
			ru.Factor = vocabulary.FormatRational(f)
			if o.Sign() != 0 {
				ru.Offset = vocabulary.FormatRational(o)
			}
		}
		out.Units = append(out.Units, ru)
	}
	for _, c := range g.Conversions() {
		rc := substrate.RepositoryConversion{ID: c.ID, From: c.From, To: c.To, Expr: c.Expr, Inverse: c.Inverse}
		if c.Factor != nil {
			rc.Factor = vocabulary.FormatRational(c.Factor)
		}
		if c.Offset != nil {
			rc.Offset = vocabulary.FormatRational(c.Offset)
		}
		out.Conversions = append(out.Conversions, rc)
	}
	return out
}

// ConvertUnit converts one value through the repository's graph at one
// revision: exact inside a linear component, through the one expression
// route between two components otherwise. A unit the graph does not hold, or
// two units nothing joins, is a validation error naming why.
func (ds *dataset) ConvertUnit(ctx context.Context, in substrate.UnitConvert) (substrate.UnitConversion, error) {
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return substrate.UnitConversion{}, err
	}
	defer func() { _ = tx.Rollback() }()
	at, err := ds.unitGraph(ctx, tx)
	if err != nil {
		return substrate.UnitConversion{}, err
	}
	c, err := at.graph.Convert(ctx, in.Value, in.From, in.To, in.Delta)
	if err != nil {
		return substrate.UnitConversion{}, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	return substrate.UnitConversion{Value: c.Value, Unit: c.Unit, Exact: c.Exact}, nil
}

// queueCircularRederivation asks the pass behind the open to re-derive the observed kinds that hold circular rows
// when the transaction moved a turn: a circular point's angle is its value
// over its unit's turn, and the rollups keep sums of the angles' sines and
// cosines, so a changed turn changes stored sums. An aggregate read over such
// a kind is refused until the pass finishes (checkObservedReady).
func (t *txn) queueCircularRederivation(was, now *vocabulary.UnitGraph) error {
	if !turnsMoved(was, now) {
		return nil
	}
	kinds := observedKindsOf(t.declarations())
	if len(kinds) == 0 {
		return nil
	}
	rows, err := t.query(`SELECT k FROM unnest($1::text[]) k
		WHERE EXISTS (SELECT 1 FROM records WHERE kind = k AND (props->>'circular') = 'true')
		ORDER BY k`, kinds)
	if err != nil {
		return fmt.Errorf("substrate/engine: find the circular observed kinds: %w", err)
	}
	var owed []indexReprojection
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			_ = rows.Close()
			return err
		}
		owed = append(owed, indexReprojection{kind: kind, observed: indexFilter{moved: true, every: true}})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if len(owed) == 0 {
		return nil
	}
	if err := t.requestReprojections(owed); err != nil {
		return err
	}
	ds := t.ds
	t.afterCommit = append(t.afterCommit, ds.kickIndexReprojection)
	return nil
}

// turnsMoved reports whether any unit's turn, its own or its circular
// component's, differs between two graphs.
func turnsMoved(was, now *vocabulary.UnitGraph) bool {
	ids := map[string]bool{}
	for _, u := range was.Units() {
		ids[u.ID] = true
	}
	for _, u := range now.Units() {
		ids[u.ID] = true
	}
	for id := range ids {
		a, b := was.Turn(id), now.Turn(id)
		switch {
		case (a == nil) != (b == nil):
			return true
		case a != nil && a.Cmp(b) != 0:
			return true
		}
	}
	return false
}
