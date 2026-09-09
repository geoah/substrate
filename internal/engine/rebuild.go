package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// rebuild-repository: clear the fold and replay the changelog
// through the SAME fold the live write path uses (fold.go). It is the
// containment test made runnable — a repository is what this plus its side
// stores reproduces — and it is a required, tested path rather than a
// diagnostic, because a fold nobody can rebuild is a fold nobody can trust.
//
// WHAT IS CLEARED AND WHY. The fold tables are the ones the changelog's entries
// write and therefore the ones the changelog can say again:
//
//	records            the fold itself, and with it `fts`, the search index
//	                   the fold derives over each row from the kind's
//	                   declaration (fold.go foldFTS, reprojectFTS)
//	refs               the reverse projection of the records' reference values,
//	                   re-derived by the same record effect that produces them
//	annotations        written by the same entries
//	property_managers  ditto — who last had a write accepted, per property
//	former_ids         ditto — merge's trail
//	trigger_cursors, trigger_schedule, trigger_failures, paged_cursors
//	                   the delivery ledger (delivery.go): every motion is an
//	                   effect on a `delivery` entry, so a trigger comes back at
//	                   the position it last acknowledged, with its parked
//	                   failures and its drain's resume row. The one motion
//	                   outside the ledger is the scan position past rows that
//	                   matched nothing (functions.go advanceCursor): a replay
//	                   leaves the cursor at the last acknowledged delivery and
//	                   the next pass re-reads rows that deliver nothing.
//
// property_offers is neither replayed nor kept: it is recompute's projection
// of what each live source offers each target (mapping.go syncOffers), the
// changelog never carried it, and a table left standing would hold whatever
// the last live recompute left, or nothing after an import into an empty
// database. The rebuild clears it and derives it again from the fold it just
// replayed (rederiveOffers); a row's updated_at is its source record's, so the
// derived table is the live one exactly.
//
// Everything else survives the rebuild, and each for a stated reason:
//
//   - sealed, A SIDE STORE: its payloads were never in the changelog and
//     cannot be regenerated from it; the changelog only re-links the references.
//     This is why the repository directory holds them beside the segments
//     (repodir.go), as it holds the blob bytes.
//   - embeddings, embed_queue — DERIVED FROM THE RECORDS, not from the changelog,
//     and expensive: the vectors of a reproduced row are still that row's, so
//     they are kept rather than re-bought from the provider.
//   - oauth_flows, RUNTIME STATE: a consent flow in flight is a nonce and a
//     PKCE verifier with an expiry, which has no meaning in the changelog; an
//     interrupted flow is started again.
//   - vocabulary_dialect, the STORE SHAPE's stamp, about the tables rather
//     than about their contents.
//   - changelog_dialect — what dialect the entries being replayed are written
//     in (changelogdialect.go). A replay does not rewrite an entry, so it
//     cannot change the answer.
//   - import_progress: the boot import's own marker (repodir.go). A rebuild
//     never runs while it is set, because no dataset opens over an
//     incomplete import.
//   - idempotency_keys: the `Idempotency-Key` store (idempotency.go), request
//     bookkeeping the changelog never carried. A rebuild replays the effects
//     the keys answer for, so the keys stay valid; only a fresh database
//     forgets them, which docs/api.md states.
//   - repositories — the control plane, one row per repository.

// RebuildReport is what one rebuild did.
type RebuildReport struct {
	Repository string        `json:"repository"`
	Entries    int64         `json:"entries"`
	Head       int64         `json:"head"`
	Records    int64         `json:"records"`
	Took       time.Duration `json:"took"`
}

// foldTables are cleared and replayed, in this order: the record fold, then
// the delivery ledger's four (delivery.go). Nothing here is referenced by
// anything else.
var foldTables = append([]string{
	"refs", "former_ids", "annotations", "property_managers", "records",
}, deliveryTables...)

// rebuildBatch bounds one page of the replay: a transaction cannot iterate a
// cursor while it writes, so the changelog is read a page at a time by seq.
const rebuildBatch = 500

// Rebuilder is the operator hat's rebuild seam, off substrate.Service like
// Resetter (auth.go) and asserted here for the same reason.
type Rebuilder interface {
	RebuildRepository(ctx context.Context, repository string) (RebuildReport, error)
}

var _ Rebuilder = (*service)(nil)

// RebuildRepository clears one repository's fold and replays its whole
// changelog into it FROM THE SEGMENT FILES under the data root, so the
// directory alone is proven to reproduce the fold. The repository's own
// advisory lock is held for the duration, so no write can interleave, and the
// whole rebuild is ONE transaction: a rebuild either replaces the fold or
// leaves it exactly as it was. Before the replay the files are held to the
// table (repodir.go): the heads must be equal and the common tail must agree,
// or the rebuild refuses rather than fold a history the table does not index.
func (s *service) RebuildRepository(ctx context.Context, repository string) (RebuildReport, error) {
	started := time.Now()
	if s.readOnly {
		return RebuildReport{}, ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return RebuildReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return RebuildReport{}, err
	}
	report := RebuildReport{Repository: repo.ID}
	if err := ds.directoryErr(); err != nil {
		return report, err
	}
	// Not inTx: a rebuild is not a write with an actor and must append no
	// entry of its own. It replays what is already there.
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback() }()
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
		now: nowUTC(), internal: true,
	}
	// The changelog lock first, then the writer mutex: the order inTx takes
	// them. With both held no committed transaction is still on its way to
	// the file, so the file is at the table's head or something is wrong.
	if err := t.lockKey(changelogLockKey); err != nil {
		return report, err
	}
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	log, err := ds.replayLog(ctx, t.tx)
	if err != nil {
		return report, err
	}
	if err := t.rebuild(log, &report); err != nil {
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	report.Took = time.Since(started)
	return report, nil
}

// replayLog opens the repository's changelog files for a replay, read-only
// because the dataset's writer has the active segment open, and holds them
// to the table: equal heads and an agreeing tail, or a named refusal.
func (ds *dataset) replayLog(ctx context.Context, q dbx) (*changelogfile.Log, error) {
	tableHead, err := tableChangelogHead(ctx, q)
	if err != nil {
		return nil, err
	}
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
	}
	switch {
	case log.Head() > tableHead:
		return nil, fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileAhead, log.Head(), tableHead)
	case log.Head() < tableHead:
		return nil, fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileBehind, log.Head(), tableHead)
	}
	if err := compareTails(ctx, q, log, tableHead); err != nil {
		return nil, err
	}
	return log, nil
}

// rebuild clears the fold tables and replays every entry of log through the
// fold, in seq order and in pages.
func (t *txn) rebuild(log *changelogfile.Log, report *RebuildReport) error {
	// The write path's own serialization: holding the changelog lock for the whole
	// rebuild means no writer can append while the fold is missing.
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	// Under that lock, the changelog dialect is read AGAIN rather than trusted
	// from the open (changelogdialect.go): a replay is exactly the operation
	// that must not run on a stale claim, and it refuses here, before the fold
	// tables are cleared.
	if err := t.refuseNewerChangelogDialect(); err != nil {
		return err
	}
	for _, table := range foldTables {
		if _, err := t.exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("substrate/engine: rebuild: clear %s: %w", table, err)
		}
	}
	var after int64
	for {
		entries, err := log.Read(after, rebuildBatch)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, e := range entries {
			ch, err := changeOfEntry(e)
			if err != nil {
				return err
			}
			if foldRefuses(ch) {
				return fmt.Errorf("substrate/engine: rebuild refuses seq %d: %s cannot be replayed yet — the fold would not be the changelog's",
					ch.Seq, ch.Op)
			}
			if err := t.foldEntry(ch); err != nil {
				return err
			}
			report.Entries++
			report.Head = ch.Seq
			after = ch.Seq
		}
	}
	if err := t.rederiveOffers(); err != nil {
		return err
	}
	return t.row(`SELECT count(*) FROM records`).Scan(&report.Records)
}

// rederiveOffers clears property_offers and derives it again from the fold:
// every live record of a kind some mapping targets gets the rows its live
// sources offer (mapping.go syncOffersOf). It is the offers half of recompute
// alone, so no accepted value moves and nothing appends, which is what lets a
// rebuild and an import, both forbidden to append, run it. The registry names
// the target kinds, so the import's first pass, folding under an empty
// registry, clears the table and derives nothing; the second pass derives it
// all.
func (t *txn) rederiveOffers() error {
	if _, err := t.exec(`DELETE FROM property_offers`); err != nil {
		return fmt.Errorf("substrate/engine: rebuild: clear property_offers: %w", err)
	}
	targets := map[string]bool{}
	for _, m := range t.declarations().Mappings() {
		targets[m.To] = true
	}
	return t.deriveOffersOf(sortedKeys(targets))
}

// recomputeMappingTargets is the vocabulary apply's half of recompute. For
// every target kind whose mapping set the batch changed: the properties the
// LIVE mappings supplied and the candidate's no longer do are released on
// every live record where the machine tier holds them (value and manager row,
// a required property kept), the kind's offers go, and each record recomputes
// against the candidate for whatever still maps. Offers AND values: the values
// a removed or narrowed mapping's sources projected are changelog entries a
// rebuild keeps, so only a recompute in this transaction leaves nothing for a
// rebuild under the published closure to disagree with. A kind losing its last
// mapping is the same computation against an empty candidate set, so a value
// the machine wrote for no mapping (a system write, a default) is not touched.
func (t *txn) recomputeMappingTargets(live, cand *vocabulary.Registry) error {
	for _, kind := range changedMappingTargets(live, cand) {
		removed := mappedProperties(live.MappingsTo(kind))
		for name := range mappedProperties(cand.MappingsTo(kind)) {
			delete(removed, name)
		}
		if _, err := t.exec(`DELETE FROM property_offers WHERE record_kind = $1`, kind); err != nil {
			return fmt.Errorf("substrate/engine: clear the offers of %s: %w", kind, err)
		}
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return err
		}
		mapped := len(cand.MappingsTo(kind)) > 0
		for _, id := range ids {
			ref := eref{Kind: kind, ID: id}
			if err := t.releaseMachineManaged(ref, sortedKeys(removed)); err != nil {
				return fmt.Errorf("substrate/engine: release %s %s after its mappings changed: %w", kind, id, err)
			}
			if !mapped {
				continue
			}
			if err := t.recompute(ref); err != nil {
				return fmt.Errorf("substrate/engine: recompute %s %s after its mappings changed: %w", kind, id, err)
			}
		}
	}
	return nil
}

// mappedProperties is the set of target properties a mapping set writes: the
// union of every mapping's map keys. A match rule reads a target property to
// find the subject and writes nothing, so it is not one.
func mappedProperties(ms []*vocabulary.Mapping) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		for _, name := range m.MapOrder {
			out[name] = true
		}
	}
	return out
}

// deriveOffersOf derives the offers of every live record of the given target
// kinds from its live sources, against the transaction's declarations. The
// caller has deleted what it wants gone; this writes what is live.
func (t *txn) deriveOffersOf(kinds []string) error {
	for _, kind := range kinds {
		ids, err := t.liveIDsOf(kind)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := t.syncOffersOf(eref{Kind: kind, ID: id}); err != nil {
				return fmt.Errorf("substrate/engine: derive the offers of %s %s: %w", kind, id, err)
			}
		}
	}
	return nil
}

// liveIDsOf lists one kind's live record ids, read to the end before the
// caller writes: a transaction cannot iterate a cursor while it writes.
func (t *txn) liveIDsOf(kind string) ([]string, error) {
	rows, err := t.query(`SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kind)
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

// changeOfEntry is a file entry in the shape the fold replays. The payload is
// decoded number-preserving for the same reason scanChange does: float64
// would round an integer past 2^53 into a value the changelog never held.
func changeOfEntry(e changelogfile.Entry) (substrate.Change, error) {
	ch := substrate.Change{
		Seq: e.Seq, TS: e.TS.UTC(), Actor: substrate.Actor(e.Actor), Op: substrate.Op(e.Op),
		RecordID: e.RecordID, Kind: e.Kind,
	}
	if len(e.Payload) > 0 {
		payload, err := decodeNumberPreserving(e.Payload)
		if err != nil {
			return ch, fmt.Errorf("substrate/engine: seq %d carries an unreadable payload: %w", e.Seq, err)
		}
		ch.Payload = payload
	}
	return ch, nil
}

// foldSnapshot reads every folded table in one deterministic order — the shape
// a rebuild is compared against. `fts` is its own section, not a column of
// `records`: the search index is derived from the folded row AND the kind's
// declaration (fold.go foldFTS), so a rebuild that reproduced the rows but not
// their index has not reproduced the store, and a mismatch in the index is a
// different finding from a mismatch in the rows, which the section names.
func foldSnapshot(ctx context.Context, db *sql.DB) (map[string]any, error) {
	out := map[string]any{}
	queries := map[string]string{
		"records": `SELECT to_jsonb(r) - 'repository' FROM (
				SELECT kind, id, title, body, states, at, ends_at, due_at, props, labels,
					version, kind_version, created_at, updated_at, deleted_at, finalizers
				FROM records ORDER BY kind, id) r`,
		"fts": `SELECT to_jsonb(f) FROM (
				SELECT kind, id, fts::text FROM records ORDER BY kind, id) f`,
		"refs": `SELECT to_jsonb(r) FROM (
				SELECT src_kind, src, property, path, ord, dst_kind, dst, props
				FROM refs ORDER BY src_kind, src, property, path, ord) r`,
		"annotations": `SELECT to_jsonb(a) FROM (
				SELECT record_kind, record_id, key, value, updated_at
				FROM annotations ORDER BY record_kind, record_id, key) a`,
		"property_managers": `SELECT to_jsonb(m) FROM (
				SELECT record_kind, record_id, property, actor, tier, principal, updated_at
				FROM property_managers ORDER BY record_kind, record_id, property) m`,
		"former_ids": `SELECT to_jsonb(f) FROM (
				SELECT record_kind, former_id, record_id, created_at
				FROM former_ids ORDER BY record_kind, former_id) f`,
		// Whole, updated_at included: a row's stamp is its source record's
		// (mapping.go syncOffers), so a rebuild derives it too.
		"property_offers": `SELECT to_jsonb(o) FROM (
				SELECT record_kind, record_id, property, actor, value, updated_at
				FROM property_offers ORDER BY record_kind, record_id, property, actor) o`,
		// The delivery ledger's three replayable tables. trigger_cursors is
		// left out: its scan position is written outside the ledger
		// (functions.go advanceCursor), so a rebuild reproduces the
		// acknowledged position and not the row.
		"trigger_schedule": `SELECT to_jsonb(s) - 'repository' FROM (
				SELECT trigger_id, fired_at, updated_at FROM trigger_schedule ORDER BY trigger_id) s`,
		"trigger_failures": `SELECT to_jsonb(f) - 'repository' FROM (
				SELECT id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at, payload
				FROM trigger_failures ORDER BY id) f`,
		"paged_cursors": `SELECT to_jsonb(p) - 'repository' FROM (
				SELECT chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at
				FROM paged_cursors ORDER BY chain) p`,
	}
	for name, q := range queries {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		var list []any
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				_ = rows.Close()
				return nil, err
			}
			// UseNumber: the snapshot is the containment instrument, and a
			// decode through float64 would round an integer past 2^53 in BOTH
			// snapshots, so a rebuild that rounded the stored value would
			// compare equal to the fold it failed to reproduce.
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			var v any
			if err := dec.Decode(&v); err != nil {
				_ = rows.Close()
				return nil, err
			}
			list = append(list, v)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		out[name] = list
	}
	return out, nil
}

// FoldSnapshot renders a repository's whole fold as one ordered JSON document:
// what `rebuild-repository` must reproduce, byte for byte. It is the
// containment test's instrument — operator tooling and the rebuild test both
// read the fold through it rather than through a hand-written query each.
func (ds *dataset) FoldSnapshot(ctx context.Context) ([]byte, error) {
	snap, err := foldSnapshot(ctx, ds.db)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(snap, "", "  ")
}
