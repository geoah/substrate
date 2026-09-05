package engine

// THE REPOSITORY DIRECTORY.
//
// Every repository has one directory under the data root,
// <root>/repositories/<id>, holding its manifest, its changelog segments,
// its blob bytes and one file per sealed row (internal/changelogfile,
// decision 0051). The Postgres tables stay the live index and the commit
// point; the directory is what a backup copies and what a boot reads back.
//
// The two are kept equal in three places, and nowhere else:
//
//   - inTx (dataset.go) appends the committed entries and mirrors the sealed
//     rows AFTER tx.Commit(), under the dataset's writer mutex. A crash between
//     the commit and the append leaves the directory one transaction behind.
//   - reconcileRepositories runs at boot over every row and every directory
//     and applies the five cases below, so the gap a crash leaves is closed
//     before anything is served.
//   - openNew and RebuildRepository re-run the head comparison (never the
//     import) so a dataset never appends onto a file that is not at the
//     table's head.
//
// The five boot cases, from docs/plans/filesystem-changelog.md:
//
//  1. Row and directory, heads equal, the common tail's checksums agree: ok.
//  2. Table ahead of the file: append the missing rows to the file.
//  3. File ahead of the table, or a directory with no row: import. The row is
//     created from the manifest when missing, sealed/ is loaded into the
//     table, the missing entries are inserted with their checksums, and the
//     fold is rebuilt from the files (the same replay `repository rebuild`
//     runs).
//  4. A seq in both with different checksums, a line whose sum does not
//     verify, or a finished segment whose sidecar does not match: the boot
//     refuses, naming the repository and the seq or file. Nothing is repaired.
//     Refusing the WHOLE boot rather than quarantining one repository is a v1
//     simplification: a divergent repository must not be served, and one
//     refusal an operator reads is simpler than a per-repository half-open
//     state that every code path would have to know about.
//  5. Row with no directory: write the directory out from the tables. This is
//     the one-time migration from a store that predates the data root.
//
// Sealed files follow the changelog's direction in every case: an import
// reads them into the table; everything else writes the table out, so a
// mirror a crash skipped (a TOTP step consume appends no entry, so heads
// alone would not notice) is rewritten at the next boot.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ErrChangelogFileBehind is the refusal every write meets after an append or
// a sealed mirror failed AFTER its transaction committed: the tables hold a
// write the directory does not, and the dataset stops taking writes until
// the process restarts and the boot check catches the directory up. Nothing
// repairs inline, because a repair racing the next append is how two writers
// interleave lines.
var ErrChangelogFileBehind = errors.New("substrate/engine: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up")

// ErrChangelogDiverged is the boot check's refusal (case 4): the file and the
// table hold different entries under one seq, or the file does not verify.
var ErrChangelogDiverged = errors.New("substrate/engine: the changelog file and the changelog table disagree")

// ErrChangelogFileAhead is the refusal a dataset open or a rebuild gives when
// the file holds entries the table does not: an import runs at boot and
// nowhere else, so the answer is a restart.
var ErrChangelogFileAhead = errors.New("substrate/engine: the changelog file is ahead of the table; restart the server so the boot check imports it")

// tailCompareEntries is how many entries of the common tail the head
// comparison checks entry by entry. A divergence anywhere below the tail is
// what `repository verify` walks for; the tail is what a crash can touch.
const tailCompareEntries = 64

// The reconcile actions, as the boot log prints them.
const (
	reconcileOK       = "ok"
	reconcileCaughtUp = "caught up"
	reconcileImported = "imported"
	reconcileWroteDir = "wrote directory"
)

// reconcileOutcome is what the boot check did for one repository.
type reconcileOutcome struct {
	Repository string
	Username   string
	Action     string
	// Entries is how many entries moved: appended to the file, or imported
	// into the table.
	Entries int64
	// TruncatedBytes is the torn tail Open cut from the active segment.
	TruncatedBytes int64
}

// repositoryDir is the repository's directory under the data root, created
// with its three subdirectories when missing.
func (s *service) repositoryDir(id string) (string, error) {
	return changelogfile.EnsureRepoDir(s.dataRoot, id)
}

// writerOptions is the one WriterOptions every writer in this process uses.
func (s *service) writerOptions() changelogfile.WriterOptions {
	return changelogfile.WriterOptions{SegmentBytes: s.segmentBytes}
}

// reconcileRepositories is the boot check: every `repositories` row against
// its directory, then every directory with no row. It refuses the boot on the
// first repository that cannot be reconciled.
func (s *service) reconcileRepositories(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: boot check: list repositories: %w", err)
	}
	dirs, err := changelogfile.ListRepositoryDirs(s.dataRoot)
	if err != nil {
		return fmt.Errorf("substrate/engine: boot check: list repository directories: %w", err)
	}
	hasRow := make(map[string]bool, len(repos))
	for _, repo := range repos {
		hasRow[repo.ID] = true
		out, err := s.reconcileRow(ctx, repo, true)
		if err != nil {
			return fmt.Errorf("substrate/engine: boot check: repository %s (%s): %w", repo.ID, repo.Username, err)
		}
		s.logReconcile(out)
	}
	for _, id := range dirs {
		if hasRow[id] {
			continue
		}
		out, err := s.importRepositoryDir(ctx, id)
		if err != nil {
			return fmt.Errorf("substrate/engine: boot check: repository directory %s: %w", id, err)
		}
		s.logReconcile(out)
	}
	return nil
}

func (s *service) logReconcile(out reconcileOutcome) {
	attrs := []any{"repository", out.Repository, "username", out.Username, "action", out.Action}
	if out.Entries > 0 {
		attrs = append(attrs, "entries", out.Entries)
	}
	if out.TruncatedBytes > 0 {
		attrs = append(attrs, "truncatedBytes", out.TruncatedBytes)
	}
	s.log.Info("substrate: repository directory checked", attrs...)
}

// reconcileRow reconciles one repository that HAS a control-plane row with its
// directory: cases 1, 2, 4 and 5, and case 3's file-ahead half when
// allowImport is set. It opens its own scoped pool and its own writer; no
// dataset may be open on the repository while it runs, which is true at boot
// and at creation.
func (s *service) reconcileRow(ctx context.Context, repo Repository, allowImport bool) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: repo.ID, Username: repo.Username}
	dir, err := changelogfile.RepoDir(s.dataRoot, repo.ID)
	if err != nil {
		return out, err
	}
	// A directory that is not there yet is case 5; one with no manifest is a
	// creation or a case-5 write that crashed part way, which the same path
	// finishes.
	_, manifestErr := changelogfile.ReadManifest(dir)
	fresh := errors.Is(manifestErr, os.ErrNotExist)
	if manifestErr != nil && !fresh {
		return out, manifestErr
	}
	if _, err := s.repositoryDir(repo.ID); err != nil {
		return out, err
	}
	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
	if err != nil {
		return out, err
	}
	ds := s.bareDataset(repo, db, dir)
	defer ds.close()
	if err := ds.reconcileDir(ctx, &out, allowImport); err != nil {
		return out, err
	}
	if fresh && out.Action == reconcileCaughtUp {
		out.Action = reconcileWroteDir
	}
	if err := s.ensureManifest(ctx, dir, repo, ds.db); err != nil {
		return out, err
	}
	return out, nil
}

// bareDataset is a dataset with no registry, no writer and no open ladder: the
// shape the reconcile and the import fold through. It never serves a request
// and is closed by its caller.
func (s *service) bareDataset(repo Repository, db *sql.DB, dir string) *dataset {
	return &dataset{
		svc: s, db: db, scope: repo.scope(), dir: dir,
		reg: vocabulary.NewRegistry(), watch: newBroadcaster(), info: repo.info(),
	}
}

// reconcileDir is the head comparison and its consequences over the dataset's
// directory: it opens the changelog (repairing a torn tail), compares heads and
// the common tail, appends what the table has and the file lacks, imports what
// the file has and the table lacks when allowed, and then mirrors the sealed
// store in the same direction. The dataset's writer, when it has one, is used
// for the append; otherwise a writer is opened over the log and closed.
func (ds *dataset) reconcileDir(ctx context.Context, out *reconcileOutcome, allowImport bool) error {
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	log, err := changelogfile.Open(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
	}
	out.TruncatedBytes = log.TruncatedBytes
	fileHead := log.Head()
	if err := compareTails(ctx, ds.db, log, min(tableHead, fileHead)); err != nil {
		return err
	}
	out.Action = reconcileOK
	switch {
	case tableHead > fileHead:
		w := ds.writer
		if w == nil {
			if w, err = log.Writer(ds.svc.writerOptions()); err != nil {
				return err
			}
			defer func() { _ = w.Close() }()
		}
		n, err := appendFromTable(ctx, ds.db, w, fileHead)
		if err != nil {
			return err
		}
		out.Action, out.Entries = reconcileCaughtUp, n
	case fileHead > tableHead:
		if !allowImport {
			return fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileAhead, fileHead, tableHead)
		}
		if err := loadSealedFiles(ctx, ds.db, ds.dir); err != nil {
			return err
		}
		n, err := ds.importEntries(ctx, log, tableHead)
		if err != nil {
			return err
		}
		out.Action, out.Entries = reconcileImported, n
		return nil
	}
	return mirrorSealedFromTable(ctx, ds.db, ds.dir)
}

// tableChangelogHead is the table's head, 0 for an empty changelog.
func tableChangelogHead(ctx context.Context, q dbx) (int64, error) {
	var head int64
	if err := q.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return 0, fmt.Errorf("substrate/engine: read the changelog head: %w", err)
	}
	return head, nil
}

// compareTails checks the last tailCompareEntries entries at or below head in
// the file against the checksums the table stamped for the same seqs. Any
// seq the two disagree on, or that one side lacks, is case 4.
func compareTails(ctx context.Context, q dbx, log *changelogfile.Log, head int64) error {
	if head <= 0 {
		return nil
	}
	n := min(head, int64(tailCompareEntries))
	after := head - n
	entries, err := log.Read(after, int(n))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
	}
	rows, err := q.QueryContext(ctx,
		`SELECT seq, hash FROM changelog WHERE seq > $1 AND seq <= $2 ORDER BY seq`, after, head)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	stamped := make(map[int64][]byte, n)
	for rows.Next() {
		var seq int64
		var hash []byte
		if err := rows.Scan(&seq, &hash); err != nil {
			return err
		}
		stamped[seq] = hash
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if int64(len(entries)) != n {
		return fmt.Errorf("%w: the file holds %d of the %d entries below seq %d", ErrChangelogDiverged, len(entries), n, head)
	}
	for _, e := range entries {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			return fmt.Errorf("%w: seq %d: %w", ErrChangelogDiverged, e.Seq, err)
		}
		hash, ok := stamped[e.Seq]
		if !ok {
			return fmt.Errorf("%w: seq %d is in the file and not in the table", ErrChangelogDiverged, e.Seq)
		}
		if !bytes.Equal(hash, sum[:]) {
			return fmt.Errorf("%w: seq %d: the table's checksum is not the file's", ErrChangelogDiverged, e.Seq)
		}
	}
	return nil
}

// appendFromTable appends every table row above the writer's head to the
// file, in pages, and returns how many it wrote.
//
// A row whose stamped `hash` is not the checksum of what it holds is
// RE-STAMPED as it is written out. That is the one-time migration from the
// store this replaced, whose `hash` was a chain hash no line can carry
// (decision 0050): the file has nothing to compare such a row against, so the
// row as it stands is what the file records, and the table's stamp is made to
// agree with it. A row this binary wrote is already equal and is not touched.
func appendFromTable(ctx context.Context, q dbx, w *changelogfile.Writer, after int64) (int64, error) {
	var n int64
	for {
		page, err := scanChecksumPage(ctx, q, after, rebuildBatch)
		if err != nil {
			return n, err
		}
		if len(page) == 0 {
			return n, nil
		}
		entries := make([]changelogfile.Entry, 0, len(page))
		for _, row := range page {
			e := row.entry.fileEntry()
			_, sum, err := changelogfile.Encode(e)
			if err != nil {
				return n, fmt.Errorf("substrate/engine: seq %d does not encode as a changelog line: %w", e.Seq, err)
			}
			if !bytes.Equal(row.hash, sum[:]) {
				if _, err := q.ExecContext(ctx, `UPDATE changelog SET hash = $2 WHERE seq = $1`, e.Seq, sum[:]); err != nil {
					return n, fmt.Errorf("substrate/engine: re-stamp the checksum of seq %d: %w", e.Seq, err)
				}
			}
			entries = append(entries, e)
			after = e.Seq
		}
		if err := w.Append(entries); err != nil {
			return n, fmt.Errorf("substrate/engine: append seq %d..%d to the changelog file: %w", entries[0].Seq, after, err)
		}
		n += int64(len(entries))
	}
}

// importEntries inserts every file entry above the table's head into the
// table, in batches of one transaction each under the changelog lock, then
// rebuilds the fold from the files. It is idempotent by construction: a crash
// between batches leaves the file still ahead, and the next boot resumes from
// the new table head.
func (ds *dataset) importEntries(ctx context.Context, log *changelogfile.Log, tableHead int64) (int64, error) {
	var n int64
	after := tableHead
	for {
		entries, err := log.Read(after, rebuildBatch)
		if err != nil {
			return n, fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			break
		}
		if err := ds.insertEntries(ctx, entries); err != nil {
			return n, err
		}
		n += int64(len(entries))
		after = entries[len(entries)-1].Seq
	}
	if err := ds.refoldFromFiles(ctx, log); err != nil {
		return n, err
	}
	return n, nil
}

// insertEntries writes one batch of file entries as changelog rows, each with
// the checksum its line carries, in one transaction holding the changelog
// lock.
func (ds *dataset) insertEntries(ctx context.Context, entries []changelogfile.Entry) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	t := &txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true}
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	for _, e := range entries {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			return fmt.Errorf("%w: seq %d: %w", ErrChangelogDiverged, e.Seq, err)
		}
		var causedBy sql.NullInt64
		if e.CausedByOK {
			causedBy = sql.NullInt64{Int64: e.CausedBy, Valid: true}
		}
		if _, err := t.exec(`
			INSERT INTO changelog (seq, ts, actor, principal, op, record_id, kind, payload, caused_by, hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
			e.Seq, e.TS, e.Actor, e.Principal, e.Op, e.RecordID, e.Kind, []byte(e.Payload), causedBy, sum[:]); err != nil {
			return fmt.Errorf("substrate/engine: import seq %d: %w", e.Seq, err)
		}
	}
	return tx.Commit()
}

// refoldFromFiles rebuilds the fold from the files in TWO passes. The fold
// consults the registry for one thing, the weighted search bands
// (fold.go foldFTS), and the reference sites (refs.go syncRefs), and the
// registry is built from declaration RECORDS that do not exist until the
// first pass has folded them. So the first pass folds under an empty registry
// to bring the declaration rows into being, the registry is loaded from
// them without writing anything, and the second pass folds every row the way
// the live write did. Nothing appends here: an import runs before any dataset
// is open and must leave the heads equal.
func (ds *dataset) refoldFromFiles(ctx context.Context, log *changelogfile.Log) error {
	replay := func() error {
		tx, err := ds.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		t := &txn{
			ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem, tier: substrate.TierMachine,
			now: nowUTC(), internal: true,
		}
		var report RebuildReport
		if err := t.rebuild(log, &report); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := replay(); err != nil {
		return fmt.Errorf("substrate/engine: import: first fold: %w", err)
	}
	if err := ds.loadDeclarationsForReplay(ctx); err != nil {
		return err
	}
	if err := replay(); err != nil {
		return fmt.Errorf("substrate/engine: import: second fold: %w", err)
	}
	return nil
}

// loadDeclarationsForReplay fills the dataset's registry from its stored
// declaration rows and WRITES NOTHING: loadStoredVocabulary clears quarantine
// markers through a patch, which appends an entry, and an import may not
// append. A closure that does not admit is left out, which is what the open
// ladder does too.
func (ds *dataset) loadDeclarationsForReplay(ctx context.Context) error {
	built, _, err := ds.storedPackages(ctx, nil)
	if err != nil {
		return err
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if err := ds.reg.InstallAll(built); err != nil {
		ds.admissibleSubset(built)
	}
	return nil
}

// importRepositoryDir is case 3 for a directory with no row: the manifest
// becomes the row, then the ordinary row reconcile imports the rest. The
// row is inserted first and the import is idempotent, so a crash anywhere in
// between leaves a row whose file is ahead of its table, which the next boot
// finishes.
func (s *service) importRepositoryDir(ctx context.Context, id string) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: id}
	dir, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return out, err
	}
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		return out, fmt.Errorf("a directory with no `repositories` row must carry a manifest to import: %w", err)
	}
	out.Username = m.Username
	if m.ChangelogDialect > maxChangelogDialect {
		return out, newerChangelogDialect(m.Username, m.ChangelogDialect)
	}
	if other, err := s.repositoryByUsername(ctx, m.Username); err == nil {
		return out, fmt.Errorf("the manifest names username %q, which repository %s already holds", m.Username, other.ID)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return out, err
	}
	if other, err := s.repositoryByAuthority(ctx, m.Authority); err == nil {
		return out, fmt.Errorf("the manifest names authority %q, which repository %s already holds", m.Authority, other.ID)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return out, err
	}
	repo := Repository{ID: m.ID, Username: m.Username, Authority: m.Authority, CreatedAt: m.CreatedAt, DEK: m.DEK}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = nowUTC()
	}
	if _, err := s.maint.ExecContext(ctx, `
		INSERT INTO repositories (id, username, authority, created_at, dek)
		VALUES ($1, $2, $3, $4, $5)`, repo.ID, repo.Username, repo.Authority, repo.CreatedAt, repo.DEK); err != nil {
		return out, fmt.Errorf("create the row from the manifest: %w", err)
	}
	if m.ChangelogDialect > 0 {
		db, err := openScoped(s.dsn, repo.scope(), s.appRole)
		if err != nil {
			return out, err
		}
		_, err = db.ExecContext(ctx, changelogDialectStamp, m.ChangelogDialect)
		_ = db.Close()
		if err != nil {
			return out, fmt.Errorf("stamp changelog dialect %d from the manifest: %w", m.ChangelogDialect, err)
		}
	}
	return s.reconcileRow(ctx, repo, true)
}

// manifestOf renders the row as its manifest, with the changelog dialect
// read from the repository's own stamp.
func (s *service) manifestOf(ctx context.Context, repo Repository, q dbx) (changelogfile.Manifest, error) {
	dialect, err := readChangelogDialect(ctx, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	return changelogfile.Manifest{
		Format: changelogfile.ManifestFormat, ID: repo.ID, Username: repo.Username,
		Authority: repo.Authority, CreatedAt: repo.CreatedAt, ChangelogDialect: dialect, DEK: repo.DEK,
	}, nil
}

// ensureManifest writes the repository's manifest when it is missing or when
// what the row says has moved (a DEK adopted, a dialect stamped). The row is
// the truth for a repository that has one.
func (s *service) ensureManifest(ctx context.Context, dir string, repo Repository, q dbx) error {
	want, err := s.manifestOf(ctx, repo, q)
	if err != nil {
		return err
	}
	have, err := changelogfile.ReadManifest(dir)
	if err == nil && manifestsEqual(have, want) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return changelogfile.WriteManifest(dir, want)
}

func manifestsEqual(a, b changelogfile.Manifest) bool {
	return a.Format == b.Format && a.ID == b.ID && a.Username == b.Username && a.Authority == b.Authority &&
		a.CreatedAt.Equal(b.CreatedAt) && a.ChangelogDialect == b.ChangelogDialect && bytes.Equal(a.DEK, b.DEK)
}

// --- the sealed mirror ------------------------------------------------------

// sealedMirrorOp is one sealed-table change waiting for its commit: a write
// of the row as it now stands, or the deletion of a ref.
type sealedMirrorOp struct {
	rec    changelogfile.SealedRecord
	delete bool
}

// mirrorSealedWrite records that this transaction wrote a sealed row, for the
// file write that follows the commit.
func (t *txn) mirrorSealedWrite(rec changelogfile.SealedRecord) {
	t.sealedMirror = append(t.sealedMirror, sealedMirrorOp{rec: rec})
}

// mirrorSealedDelete records that this transaction deleted a sealed row.
func (t *txn) mirrorSealedDelete(ref string) {
	t.sealedMirror = append(t.sealedMirror, sealedMirrorOp{rec: changelogfile.SealedRecord{Ref: ref}, delete: true})
}

// sealedRecordOf renders one row's columns as its file record.
func sealedRecordOf(ref, recordKind, recordID string, payload []byte, expiresAt sql.NullTime, updatedAt time.Time) changelogfile.SealedRecord {
	rec := changelogfile.SealedRecord{
		Ref: ref, RecordKind: recordKind, RecordID: recordID, Payload: payload, UpdatedAt: updatedAt.UTC(),
	}
	if expiresAt.Valid {
		exp := expiresAt.Time.UTC()
		rec.ExpiresAt = &exp
	}
	return rec
}

// applySealedMirror runs the collected sealed operations against the
// directory.
func applySealedMirror(dir string, ops []sealedMirrorOp) error {
	for _, op := range ops {
		var err error
		if op.delete {
			err = changelogfile.DeleteSealed(dir, op.rec.Ref)
		} else {
			err = changelogfile.WriteSealed(dir, op.rec)
		}
		if err != nil {
			return fmt.Errorf("substrate/engine: mirror sealed %s: %w", op.rec.Ref, err)
		}
	}
	return nil
}

// readSealedTable reads every sealed row of the repository as file records,
// keyed by ref.
func readSealedTable(ctx context.Context, q dbx) (map[string]changelogfile.SealedRecord, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT ref, record_kind, record_id, payload, expires_at, updated_at FROM sealed ORDER BY ref`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]changelogfile.SealedRecord{}
	for rows.Next() {
		var ref, kind, id string
		var payload []byte
		var expires sql.NullTime
		var updated time.Time
		if err := rows.Scan(&ref, &kind, &id, &payload, &expires, &updated); err != nil {
			return nil, err
		}
		out[ref] = sealedRecordOf(ref, kind, id, payload, expires, updated)
	}
	return out, rows.Err()
}

func sealedRecordsEqual(a, b changelogfile.SealedRecord) bool {
	if a.Ref != b.Ref || a.RecordKind != b.RecordKind || a.RecordID != b.RecordID || !bytes.Equal(a.Payload, b.Payload) ||
		!a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	switch {
	case a.ExpiresAt == nil && b.ExpiresAt == nil:
		return true
	case a.ExpiresAt == nil || b.ExpiresAt == nil:
		return false
	}
	return a.ExpiresAt.Equal(*b.ExpiresAt)
}

// mirrorSealedFromTable makes sealed/ hold exactly the table's rows: a file
// that differs is rewritten, one with no row is removed, one that matches is
// left alone.
func mirrorSealedFromTable(ctx context.Context, q dbx, dir string) error {
	want, err := readSealedTable(ctx, q)
	if err != nil {
		return err
	}
	have, err := changelogfile.ReadSealed(dir)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(have))
	var ops []sealedMirrorOp
	for _, rec := range have {
		seen[rec.Ref] = true
		w, ok := want[rec.Ref]
		switch {
		case !ok:
			ops = append(ops, sealedMirrorOp{rec: rec, delete: true})
		case !sealedRecordsEqual(rec, w):
			ops = append(ops, sealedMirrorOp{rec: w})
		}
	}
	for _, ref := range sortedKeys(want) {
		if !seen[ref] {
			ops = append(ops, sealedMirrorOp{rec: want[ref]})
		}
	}
	return applySealedMirror(dir, ops)
}

// loadSealedFiles upserts every file under sealed/ into the table: the import
// direction. A row the files do not name is left, because an import adds to
// a table it found; the boot check that runs on the next open then writes the
// table back out and the two agree.
func loadSealedFiles(ctx context.Context, q dbx, dir string) error {
	recs, err := changelogfile.ReadSealed(dir)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		var expires any
		if rec.ExpiresAt != nil {
			expires = rec.ExpiresAt.UTC()
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO sealed (ref, record_kind, record_id, payload, expires_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (repository, ref) DO UPDATE
			    SET record_kind = EXCLUDED.record_kind, record_id = EXCLUDED.record_id, payload = EXCLUDED.payload,
			        expires_at = EXCLUDED.expires_at, updated_at = EXCLUDED.updated_at`,
			rec.Ref, rec.RecordKind, rec.RecordID, rec.Payload, expires, rec.UpdatedAt.UTC()); err != nil {
			return fmt.Errorf("substrate/engine: import sealed %s: %w", rec.Ref, err)
		}
	}
	return nil
}

// --- the dataset's side ------------------------------------------------------

// openDirectory runs the head comparison for a dataset that is about to
// serve: the directory must be at the table's head (a creation's directory
// write racing a first open is the one way it can be behind, and it is caught
// up here), never ahead, and the common tail must agree. It opens the
// dataset's writer over the same scan.
func (ds *dataset) openDirectory(ctx context.Context) error {
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	log, err := changelogfile.Open(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
	}
	if log.Head() > tableHead {
		return fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileAhead, log.Head(), tableHead)
	}
	if err := compareTails(ctx, ds.db, log, log.Head()); err != nil {
		return err
	}
	w, err := log.Writer(ds.svc.writerOptions())
	if err != nil {
		return err
	}
	ds.writer = w
	if tableHead > log.Head() {
		n, err := appendFromTable(ctx, ds.db, w, log.Head())
		if err != nil {
			return err
		}
		ds.svc.log.Warn("substrate: the changelog file was behind the table at open and was caught up",
			"repository", ds.scope.Repository, "entries", n)
	}
	return mirrorSealedFromTable(ctx, ds.db, ds.dir)
}

// directoryErr is the standing refusal after a failed post-commit mirror.
func (ds *dataset) directoryErr() error {
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	return ds.fileErr
}

// mirrorAfterCommit appends the transaction's entries and applies its sealed
// operations. It runs with writerMu held, right after tx.Commit(), so file
// order is commit order. A failure is logged and latched: the commit already
// happened, so the caller's write is durable in the tables, and every later
// write is refused until a restart lets the boot check catch the directory
// up.
func (ds *dataset) mirrorAfterCommit(t *txn) {
	if len(t.pending) > 0 {
		entries := make([]changelogfile.Entry, 0, len(t.pending))
		for _, e := range t.pending {
			entries = append(entries, e.fileEntry())
		}
		if err := ds.writer.Append(entries); err != nil {
			ds.latchDirectoryErr(fmt.Errorf("append seq %d..%d: %w", entries[0].Seq, entries[len(entries)-1].Seq, err))
			return
		}
	}
	if err := applySealedMirror(ds.dir, t.sealedMirror); err != nil {
		ds.latchDirectoryErr(err)
	}
}

// mirrorSealedNow applies sealed operations for a write that ran outside
// inTx (a refreshed token, a teardown's deletes), under the same mutex and
// the same latch.
func (ds *dataset) mirrorSealedNow(ops []sealedMirrorOp) {
	if ds.writer == nil || len(ops) == 0 {
		return
	}
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	if ds.fileErr != nil {
		return
	}
	if err := applySealedMirror(ds.dir, ops); err != nil {
		ds.latchDirectoryErr(err)
	}
}

// latchDirectoryErr records the first post-commit failure. Called with
// writerMu held.
func (ds *dataset) latchDirectoryErr(cause error) {
	if ds.fileErr != nil {
		return
	}
	ds.fileErr = fmt.Errorf("%w: repository %s: %w", ErrChangelogFileBehind, ds.info.Name, cause)
	ds.svc.log.Error("substrate: the repository directory fell behind the tables; refusing writes until restart",
		"repository", ds.scope.Repository, "username", ds.info.Name, "error", cause)
}

// mirrorSealedByRef writes one sealed row's file for a service-level write
// that ran on the maintenance pool with no dataset in hand (the TOTP step
// consume). It is best effort in the same sense as the dataset's mirror: the
// row is committed, and a failure is logged for the boot check to close.
func (s *service) mirrorSealedByRef(repoID string, rec changelogfile.SealedRecord) {
	dir, err := changelogfile.RepoDir(s.dataRoot, repoID)
	if err == nil {
		err = changelogfile.WriteSealed(dir, rec)
	}
	if err != nil {
		s.log.Error("substrate: could not mirror a sealed row to the repository directory; the boot check will rewrite it",
			"repository", repoID, "ref", rec.Ref, "error", err)
	}
}
