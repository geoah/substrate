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
//	records            the fold itself
//	refs               the reverse projection of the records' reference values,
//	                   re-derived by the same record effect that produces them
//	annotations        written by the same entries
//	property_managers  ditto — who last had a write accepted, per property
//	former_ids         ditto — merge's trail
//
// Everything else survives the rebuild, and each for a stated reason:
//
//   - blobs, sealed — SIDE STORES. Their bytes were never in the changelog and
//     cannot be regenerated from it; the changelog only re-links the references.
//     This is why the repository directory holds all three (repodir.go).
//   - embeddings, embed_queue — DERIVED FROM THE RECORDS, not from the changelog,
//     and expensive: the vectors of a reproduced row are still that row's, so
//     they are kept rather than re-bought from the provider.
//   - property_offers — recompute's projection of what each live source would
//     write. It is rebuilt and pruned by mapping recompute, from the records;
//     the changelog never carried it.
//   - trigger_cursors, trigger_failures, trigger_schedule, oauth_flows,
//     paged_cursors — RUNTIME STATE. A cursor is a consumer's position in the
//     changelog, not a fold of it: clearing them would redeliver history, and a
//     half-finished oauth flow or drain has no meaning in the changelog at all.
//   - vocabulary_dialect, vocabulary_promotions — the STORE SHAPE's ledger, about the
//     tables rather than about their contents.
//   - changelog_dialect — what dialect the entries being replayed are written
//     in (changelogdialect.go). A replay does not rewrite an entry, so it
//     cannot change the answer.
//   - repositories — the control plane, one row per user.

// RebuildReport is what one rebuild did.
type RebuildReport struct {
	Repository string        `json:"repository"`
	Username   string        `json:"username"`
	Entries    int64         `json:"entries"`
	Head       int64         `json:"head"`
	Records    int64         `json:"records"`
	Took       time.Duration `json:"took"`
}

// foldTables are cleared and replayed, in this order: a purge effect walks the
// same list, and nothing here is referenced by anything else.
var foldTables = []string{
	"refs", "former_ids", "annotations", "property_managers", "records",
}

// rebuildBatch bounds one page of the replay: a transaction cannot iterate a
// cursor while it writes, so the changelog is read a page at a time by seq.
const rebuildBatch = 500

// Rebuilder is the operator hat's rebuild seam, off substrate.Service like
// Resetter (auth.go) and asserted here for the same reason.
type Rebuilder interface {
	RebuildRepository(ctx context.Context, username string) (RebuildReport, error)
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
func (s *service) RebuildRepository(ctx context.Context, username string) (RebuildReport, error) {
	started := time.Now()
	if s.readOnly {
		return RebuildReport{}, ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return RebuildReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return RebuildReport{}, err
	}
	report := RebuildReport{Repository: repo.ID, Username: repo.Username}
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
	return t.row(`SELECT count(*) FROM records`).Scan(&report.Records)
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
// a rebuild is compared against. `fts` is included: the search index is derived
// from the folded row, so a rebuild that reproduced the rows but not their
// index would not have reproduced the store.
func foldSnapshot(ctx context.Context, db *sql.DB) (map[string]any, error) {
	out := map[string]any{}
	queries := map[string]string{
		"records": `SELECT to_jsonb(r) - 'repository' FROM (
				SELECT kind, id, title, body, states, at, ends_at, due_at, props, labels,
					version, created_at, updated_at, deleted_at, finalizers, fts::text
				FROM records ORDER BY kind, id) r`,
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
