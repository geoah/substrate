package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

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
//	edges              written by the same entries
//	annotations        ditto
//	property_managers  ditto — who last had a write accepted, per property
//	former_ids         ditto — merge's trail
//
// Everything else survives the rebuild, and each for a stated reason:
//
//   - blobs, sealed — SIDE STORES. Their bytes were never in the changelog and
//     cannot be regenerated from it; the changelog only re-links the references.
//     This is why a backup is changelog + blobs + sealed as ONE unit.
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
//   - repositories — the control plane, one row per user.

// RebuildReport is what one rebuild did.
type RebuildReport struct {
	Repository string        `json:"repository"`
	Username   string        `json:"username"`
	Entries    int64         `json:"entries"`
	Head       int64         `json:"head"`
	Records    int64         `json:"records"`
	Took       time.Duration `json:"took"`
	// Unverified marks a fold built from history the chain refused
	// (RebuildRepositoryUnverified): installed on explicit demand, and the
	// report says so rather than letting it read as clean.
	Unverified bool `json:"unverified,omitempty"`
}

// foldTables are cleared and replayed, in this order: a purge effect walks the
// same list, and nothing here is referenced by anything else.
var foldTables = []string{
	"edges", "former_ids", "annotations", "property_managers", "records",
}

// rebuildBatch bounds one page of the replay: a transaction cannot iterate a
// cursor while it writes, so the changelog is read a page at a time by seq.
const rebuildBatch = 500

// RebuildRepository clears one repository's fold and replays its whole changelog
// into it. The repository's own advisory lock is held for the duration, so no
// write can interleave, and the whole rebuild is ONE transaction: a rebuild
// either replaces the fold or leaves it exactly as it was.
//
// THE CHAIN IS VERIFIED FIRST, before anything is cleared: tampered history
// refuses to become the live fold. The explicit escape hatch is
// RebuildRepositoryUnverified, a distinct method on purpose.
func (s *service) RebuildRepository(ctx context.Context, username string) (RebuildReport, error) {
	return s.rebuildRepository(ctx, username, true)
}

// RebuildRepositoryUnverified rebuilds from history the chain may refuse.
// What it installs is exactly as trustworthy as the bytes in the table, which
// is the thing the caller is choosing to accept; the report carries the mark.
func (s *service) RebuildRepositoryUnverified(ctx context.Context, username string) (RebuildReport, error) {
	report, err := s.rebuildRepository(ctx, username, false)
	report.Unverified = true
	return report, err
}

func (s *service) rebuildRepository(ctx context.Context, username string, verify bool) (RebuildReport, error) {
	started := time.Now()
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return RebuildReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return RebuildReport{}, err
	}
	report := RebuildReport{Repository: repo.ID, Username: repo.Username}
	// Not inTx: a rebuild is not a write with an actor and must append no
	// entry of its own. It replays what is already there.
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
		now: nowUTC(), internal: true,
	}
	if err := t.rebuild(&report, verify); err != nil {
		_ = tx.Rollback()
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	report.Took = time.Since(started)
	return report, nil
}

func (t *txn) rebuild(report *RebuildReport, verify bool) error {
	// The write path's own serialization: holding the changelog lock for the whole
	// rebuild means no writer can append while the fold is missing.
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	// The chain check runs INSIDE this transaction, under this lock, over the
	// exact bytes the replay below will fold (adversarial review, both
	// passes: a verify on a separate connection blesses a moment, not the
	// bytes installed).
	if verify {
		signing := t.ds.signing()
		var finding string
		if _, err := verifyChainCore(t.ctx, t.tx, t.ds.scope.Repository, signing.signedFrom, signing.public,
			func(f string) {
				if finding == "" {
					finding = f
				}
			}); err != nil {
			return err
		}
		if finding != "" {
			return fmt.Errorf("substrate/engine: rebuild refuses: the chain does not verify (%s); run `repository verify`, and only rebuild --force-unverified once you understand what you would be installing", finding)
		}
		// Signing is mandatory; a repository that never activated has hashes
		// but no signature guarantee at all. The insecure switch is the one
		// door that accepts folding it anyway, complaining (#175).
		if signing.signedFrom == 0 {
			if !t.ds.svc.insecureInvalidSigs {
				return fmt.Errorf("substrate/engine: rebuild refuses: changelog signing has never activated on this repository, so no signature vouches for the history this would install; open it under a credential key first, or rebuild under SUBSTRATE_INSECURE_ALLOW_INVALID_SIGNATURES (local testing only)")
			}
			t.ds.svc.log.Warn("substrate: INSECURE — rebuilding from a chain with no signature guarantee (signing never activated)",
				"repository", t.ds.scope.Repository)
		}
	}
	for _, table := range foldTables {
		if _, err := t.exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("substrate/engine: rebuild: clear %s: %w", table, err)
		}
	}
	var after int64
	for {
		entries, err := t.changelogPage(after)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			break
		}
		for _, ch := range entries {
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

// changelogPage reads one page of the changelog in seq order.
func (t *txn) changelogPage(after int64) ([]substrate.Change, error) {
	rows, err := t.query(`
		SELECT seq, ts, actor, op, record_id, kind, payload, hash FROM changelog
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, rebuildBatch)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.Change
	for rows.Next() {
		ch, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
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
		"edges": `SELECT to_jsonb(e) FROM (
				SELECT rel, src_kind, src, dst_kind, dst, props, subject, created_at
				FROM edges ORDER BY rel, src_kind, src, dst_kind, dst) e`,
		"annotations": `SELECT to_jsonb(a) FROM (
				SELECT record_kind, record_id, key, value, updated_at
				FROM annotations ORDER BY record_kind, record_id, key) a`,
		"property_managers": `SELECT to_jsonb(m) FROM (
				SELECT record_kind, record_id, property, actor, tier, updated_at
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
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
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
