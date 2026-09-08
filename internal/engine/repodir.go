package engine

// THE REPOSITORY DIRECTORY.
//
// Every repository has one directory under the data root,
// <root>/repositories/<authority>, holding its manifest, its changelog
// segments, its blob bytes and one file per sealed row (internal/changelogfile,
// decision 0051). The repository's id is its authority, so the directory, the
// row's primary key and the manifest's `authority` are one name. The Postgres
// tables stay the live index and the commit point; the directory is what a
// backup copies and what a boot reads back.
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
//     runs), and every embeddable property is queued for the drain, because
//     the vectors are not in the directory. An `import_progress` row marks
//     the repository from before the first batch of entries commits until
//     the transaction that commits the last fold pass, so a boot that finds
//     the row with equal heads resumes the fold, and no dataset opens while
//     it is set (ErrImportIncomplete).
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
// Two checks come from before the authority was the id. A `repositories` row
// whose id is not its authority refuses the boot, at Open before the
// credential key is tried against it, with the instruction to wipe the
// database and boot again, because the tree assumes fresh repositories before
// v1 and migrates no rows (checkRepositoryRows). And before the five cases, a
// directory named by the random id such a binary minted is moved under its
// authority, its DEK re-wrapped from the old binding to the new, so that the
// wiped database then imports it as case 3 (migrateLegacyDirs).
//
// Sealed files follow the changelog's direction in every case: an import
// reads them into the table; everything else writes the table out, so a
// mirror a crash skipped (a TOTP step consume appends no entry, so heads
// alone would not notice) is rewritten at the next boot.
//
// ONE WRITER PER DIRECTORY, ACROSS PROCESSES. The changelog writer holds an
// exclusive advisory lock on the directory (changelogfile.LockFileName) for
// its lifetime, so a second process that opens a repository for writing (an
// operator's `rebuild` or `user reset` beside a running server) is refused
// with ErrChangelogLocked instead of appending seqs the server is about to
// append too. The boot check's own writers are opened and closed inside
// reconcileDir, so a server never holds a lock against itself. A process that
// must run beside the server opens with WithDirectoryReadOnly: no boot check,
// no writer, every inTx write refused, and a verify that reports rather than
// repairs.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ErrDirectoryWrite is the refusal a write meets when a sealed file could not
// be staged BEFORE its transaction committed (dataset.go commitAndMirror):
// the transaction rolled back, nothing is durable anywhere, and the caller
// may retry, which is why it is an ErrUnavailable (a 503 with Retry-After on
// the wire) and the latched ErrChangelogFileBehind is not. A changelog writer
// that fails is not retryable, because it refuses every later prepare until
// the process restarts, so that failure latches instead (prepareLines).
var ErrDirectoryWrite = fmt.Errorf("%w: the repository directory could not be written, so the write was rolled back", substrate.ErrUnavailable)

// ErrChangelogFileBehind is the refusal every write meets once the dataset
// needs a restart: a step that runs AFTER its transaction committed failed
// (a rename into place, the newline that ends the transaction in the file, a
// sealed delete), a commit's answer was lost after it committed, or the
// changelog writer failed. In every case the tables may hold a write the
// directory does not, the caller got the error, and the dataset stops taking
// writes until the process restarts and the boot check catches the directory
// up. Nothing repairs inline, because a repair racing the next append is how
// two writers interleave lines.
var ErrChangelogFileBehind = errors.New("substrate/engine: the repository directory is behind the tables after a failed write; restart the server so the boot check catches it up")

// ErrChangelogDiverged is the boot check's refusal (case 4): the file and the
// table hold different entries under one seq, or the file does not verify.
var ErrChangelogDiverged = errors.New("substrate/engine: the changelog file and the changelog table disagree")

// ErrChangelogFileAhead is the refusal a dataset open or a rebuild gives when
// the file holds entries the table does not: an import runs at boot and
// nowhere else, so the answer is a restart.
var ErrChangelogFileAhead = errors.New("substrate/engine: the changelog file is ahead of the table; restart the server so the boot check imports it")

// ErrImportIncomplete is the refusal a dataset open meets while the
// repository's import-progress marker is set: an import began and the fold
// has not been rebuilt from the imported entries, so what `records` holds is
// not the changelog's. The boot check resumes the import; nothing else does.
var ErrImportIncomplete = errors.New("substrate/engine: the import of the repository directory has not completed; restart the server so the boot check resumes it")

// ErrChangelogLocked is the refusal a process meets when another one holds a
// repository's changelog writer lock: the operator ran `rebuild` or `user
// reset` beside a running server. It wraps changelogfile.ErrLocked.
var ErrChangelogLocked = errors.New("substrate/engine: another process holds the repository's changelog writer lock; stop the server before running this command")

// ErrRepositoryIDNotAuthority is the boot check's refusal of a `repositories`
// row whose id is not its authority: a database written before the authority
// became the id. There is no data migration; the operator wipes the database
// and boots again, and the repository directory under the data root imports.
var ErrRepositoryIDNotAuthority = errors.New("substrate/engine: a repository row's id is not its authority: this database was written before the authority became the repository id. Wipe the database and boot again; the repository directory under SUBSTRATE_DATA_ROOT imports")

// ErrDirectoryReadOnly is the refusal every write meets on a service opened
// with WithDirectoryReadOnly: this process is not the directory's writer.
var ErrDirectoryReadOnly = errors.New("substrate/engine: the service is open read-only (WithDirectoryReadOnly) and refuses to write")

// writerErr classifies an error from opening a repository's changelog for
// writing: another process's lock is named as such, the operator's cue to stop
// the server; damage keeps its own name.
func writerErr(err error) error {
	if errors.Is(err, changelogfile.ErrLocked) {
		return fmt.Errorf("%w: %w", ErrChangelogLocked, err)
	}
	return err
}

// directoryOpenErr is writerErr for changelogfile.Open, whose tail cut
// runs under the lock: locked is locked, anything else is divergence.
func directoryOpenErr(err error) error {
	if errors.Is(err, changelogfile.ErrLocked) {
		return writerErr(err)
	}
	return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
}

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
	// reconcileResumed is an import a previous boot began and did not
	// complete, finished from the rows already in the table.
	reconcileResumed = "resumed import"
	// reconcileSkipped is a directory with no row and no manifest: nothing
	// says whose it is, so it is neither imported nor deleted.
	reconcileSkipped = "skipped: no manifest"
)

// reconcileOutcome is what the boot check did for one repository.
type reconcileOutcome struct {
	Repository string
	Username   string
	Action     string
	// Entries is how many entries moved: appended to the file, or imported
	// into the table.
	Entries int64
	// TruncatedBytes and TruncatedEntries are the incomplete tail Open cut
	// from the active segment: its bytes, and the complete lines among them.
	TruncatedBytes   int64
	TruncatedEntries int64
	// StrayRepositoryID is the id of a live self-description record that is
	// not the repository's id: what the changelog of a pre-authority binary
	// holds, under the random id it minted. correctSelfDescription moves it.
	StrayRepositoryID string
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
	if err := s.migrateLegacyDirs(ctx); err != nil {
		return fmt.Errorf("substrate/engine: boot check: %w", err)
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
		if err := s.correctSelfDescription(ctx, out); err != nil {
			return fmt.Errorf("substrate/engine: boot check: repository %s (%s): %w", repo.ID, repo.Username, err)
		}
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
		if err := s.correctSelfDescription(ctx, out); err != nil {
			return fmt.Errorf("substrate/engine: boot check: repository directory %s: %w", id, err)
		}
	}
	return nil
}

// strayRepositoryRecord is the id of a live self-description record
// (kindRepository) that is not the repository's own id, or "". A changelog a
// pre-authority binary wrote holds the record under the random id it minted,
// and the import folds it as written.
func strayRepositoryRecord(ctx context.Context, q dbx, id string) (string, error) {
	var stray string
	err := q.QueryRowContext(ctx, `
		SELECT id FROM records
		WHERE kind = $1 AND id <> $2 AND deleted_at IS NULL
		ORDER BY id LIMIT 1`, kindRepository, id).Scan(&stray)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("substrate/engine: look for a stray self-description: %w", err)
	}
	return stray, nil
}

// correctSelfDescription moves the repository's self-description record from
// the id a pre-authority binary gave it (out.StrayRepositoryID) to the
// repository's id, the authority, the one way the fold moves: two entries
// appended through the ordinary write path, a put of the record under the
// authority with the properties the stray carries and a delete of the stray,
// in ONE transaction, so the table and the segment files both receive them
// and a rebuild reproduces the fold. The dataset is opened the ordinary way,
// after the reconcile that found the stray has closed its own pool. A second
// boot finds no stray and appends nothing.
func (s *service) correctSelfDescription(ctx context.Context, out reconcileOutcome) error {
	if out.StrayRepositoryID == "" {
		return nil
	}
	repo, err := s.repositoryByID(ctx, out.Repository)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	stray, err := ds.Get(ctx, kindRepository, out.StrayRepositoryID)
	if err != nil {
		return fmt.Errorf("substrate/engine: read the self-description under %s: %w", out.StrayRepositoryID, err)
	}
	props := make(map[string]any, len(stray.Properties)+2)
	for k, v := range stray.Properties {
		props[k] = v
	}
	props["name"], props["authority"] = repo.Username, repo.ID
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if _, err := t.put(substrate.PutInput{Kind: kindRepository, ID: repo.ID, Properties: props}); err != nil {
			return err
		}
		_, err := t.softDelete(eref{Kind: kindRepository, ID: out.StrayRepositoryID})
		return err
	}); err != nil {
		return fmt.Errorf("substrate/engine: move the self-description of %s from %s to its authority: %w",
			repo.Username, out.StrayRepositoryID, err)
	}
	s.log.Info("substrate: repository self-description moved under the authority",
		"repository", repo.ID, "username", repo.Username, "from", out.StrayRepositoryID)
	return nil
}

// requireRepositoryRowsAreAuthorities runs checkRepositoryRows over the
// control-plane table at Open, in every process, read-only ones included: a
// verify beside a server cannot open such a row either.
func (s *service) requireRepositoryRowsAreAuthorities(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: boot check: list repositories: %w", err)
	}
	return checkRepositoryRows(repos)
}

// checkRepositoryRows refuses a control-plane row whose id is not a valid
// authority equal to its authority column. Migration 0015 holds every row
// written from now on to it (NOT VALID, so it applies over old rows); this is
// the check over the rows it did not validate.
func checkRepositoryRows(repos []Repository) error {
	for _, repo := range repos {
		if repo.ID != repo.Authority || validRepositoryID(repo.ID) != nil {
			return fmt.Errorf("%w (row id %q, authority %q, user %s)", ErrRepositoryIDNotAuthority, repo.ID, repo.Authority, repo.Username)
		}
	}
	return nil
}

// migrateLegacyDirs moves every directory a pre-authority binary wrote under
// its authority: `<root>/repositories/<random id>` with a manifest carrying
// `id` becomes `<root>/repositories/<authority>` with the format-1 manifest,
// and the DEK wrap, bound to the old id (dekAAD), is re-wrapped under the
// authority. The unwrap comes first, so a directory the credential key does
// not open is refused before anything moves; the rename comes before the
// manifest write, and a crash between the two leaves an authority-named
// directory holding the old manifest, which the second pass finishes under the
// same checks (legacyAuthorityFree). Under a blob store that keys objects by
// the repository id the move waits for the operator (legacyBlobsMoved). A
// directory that is neither shape is refused, by name: the boot check must
// never skip a directory that may be a repository.
func (s *service) migrateLegacyDirs(ctx context.Context) error {
	names, err := changelogfile.ListLegacyRepositoryDirs(s.dataRoot)
	if err != nil {
		return fmt.Errorf("a directory under %s/ is named neither by an authority nor by a pre-authority repository id; move it out of the data root: %w", changelogfile.RepositoriesDir, err)
	}
	for _, name := range names {
		dir, err := changelogfile.LegacyRepoDir(s.dataRoot, name)
		if err != nil {
			return err
		}
		lm, err := changelogfile.ReadLegacyManifest(dir)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("repository directory %s is not named by an authority and carries no manifest; move it out of the data root", name)
		}
		if err != nil {
			return fmt.Errorf("repository directory %s is not named by an authority and its manifest is not the pre-authority shape: %w", name, err)
		}
		if lm.ID != name {
			return fmt.Errorf("repository directory %s carries a manifest whose id is %q", name, lm.ID)
		}
		if err := s.legacyAuthorityFree(ctx, name, lm.Manifest.Authority); err != nil {
			return err
		}
		if err := s.legacyBlobsMoved(ctx, name, lm.Manifest.Authority); err != nil {
			return err
		}
		m, err := s.rewrapLegacyDEK(lm)
		if err != nil {
			return err
		}
		newDir, err := changelogfile.RenameRepoDir(s.dataRoot, name, m.Authority)
		if err != nil {
			return fmt.Errorf("move repository directory %s under its authority: %w", name, err)
		}
		if err := changelogfile.WriteManifest(newDir, m); err != nil {
			return fmt.Errorf("write the manifest of repository %s after moving it under its authority: %w", m.Authority, err)
		}
		s.log.Info("substrate: repository directory moved under its authority",
			"repository", m.Authority, "username", m.Username, "from", name)
	}
	// The crash window: renamed, manifest not yet rewritten.
	dirs, err := changelogfile.ListRepositoryDirs(s.dataRoot)
	if err != nil {
		return err
	}
	for _, authority := range dirs {
		dir, err := changelogfile.RepoDir(s.dataRoot, authority)
		if err != nil {
			return err
		}
		if _, err := changelogfile.ReadManifest(dir); err == nil || errors.Is(err, os.ErrNotExist) {
			continue
		}
		lm, err := changelogfile.ReadLegacyManifest(dir)
		if err != nil || lm.Manifest.Authority != authority {
			// Not the window; the reconcile names the manifest's real fault.
			continue
		}
		if err := s.legacyAuthorityFree(ctx, lm.ID, authority); err != nil {
			return err
		}
		m, err := s.rewrapLegacyDEK(lm)
		if err != nil {
			return err
		}
		if err := changelogfile.WriteManifest(dir, m); err != nil {
			return fmt.Errorf("write the manifest of repository %s after moving it under its authority: %w", authority, err)
		}
		s.log.Info("substrate: repository directory's manifest rewritten after an interrupted move",
			"repository", authority, "username", m.Username, "from", lm.ID)
	}
	return nil
}

// legacyAuthorityFree holds the authority a pre-authority directory (oldID)
// names to what a repository id may be (validRepositoryID) and refuses one a
// `repositories` row already holds. Both passes of migrateLegacyDirs run it,
// so a move that crashed between the rename and the manifest write meets the
// same checks the first pass ran.
func (s *service) legacyAuthorityFree(ctx context.Context, oldID, authority string) error {
	if err := validRepositoryID(authority); err != nil {
		return fmt.Errorf("repository directory %s names an authority that cannot be a repository id: %w", oldID, err)
	}
	if _, err := s.repositoryByID(ctx, authority); err == nil {
		return fmt.Errorf("repository directory %s names authority %q, which a `repositories` row already holds", oldID, authority)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return err
	}
	return nil
}

// legacyBlobsMoved refuses to move a pre-authority directory while the blob
// store still keys the repository's objects by the old id. The fs backend
// keeps them in the directory, so the rename moves them; the s3 backend keys
// them `<prefix><repository id>/<digest>` and a rename on disk moves nothing
// in the bucket, so every blob of the moved repository would read as
// ErrNotStored. The move proceeds once the old prefix is empty, which is the
// operator's step; a backend that cannot list the old prefix refuses outright.
func (s *service) legacyBlobsMoved(ctx context.Context, oldID, authority string) error {
	if s.blobs.Name() == blobbytes.BackendFS {
		return nil
	}
	lister, ok := s.blobs.(blobbytes.LegacyRepositoryLister)
	if !ok {
		return fmt.Errorf("repository directory %s was written before the authority became the repository id, and the %s blob store keys objects by that id and cannot list them; move its objects from the old id to %s in the store, then boot again",
			oldID, s.blobs.Name(), authority)
	}
	objs, err := lister.ListLegacyRepository(ctx, oldID, 1)
	if err != nil {
		return fmt.Errorf("repository directory %s: list the %s blob store's objects under the old id: %w", oldID, s.blobs.Name(), err)
	}
	if len(objs) > 0 {
		return fmt.Errorf("repository directory %s was written before the authority became the repository id, and the %s blob store still holds its objects under the old id: move every object under `<SUBSTRATE_BLOB_S3_PREFIX>%s/` to `<SUBSTRATE_BLOB_S3_PREFIX>%s/` in the bucket, then boot again; the directory moves under its authority once the old prefix is empty",
			oldID, s.blobs.Name(), oldID, authority)
	}
	return nil
}

// rewrapLegacyDEK renders a pre-authority manifest as the manifest this
// binary writes, with the DEK re-wrapped from the old id's binding to the
// authority's and the wrap naming this host's key. An empty DEK (a pre-DEK
// repository) is carried as is.
func (s *service) rewrapLegacyDEK(lm changelogfile.LegacyManifest) (changelogfile.Manifest, error) {
	m := currentManifest(lm.Manifest)
	if len(lm.Manifest.DEK) == 0 {
		return m, nil
	}
	dek, err := s.unwrapDEK(lm.Manifest.DEK, lm.ID, lm.Manifest.DEKKeyID)
	if err != nil {
		return m, fmt.Errorf("the DEK in the manifest of repository %s (%s, directory %s) does not open: %w. Set the key the directory was written under, or move the directory out of the data root",
			lm.Manifest.Authority, lm.Manifest.Username, lm.ID, err)
	}
	if m.DEK, err = s.wrapDEK(dek, m.Authority); err != nil {
		return m, err
	}
	m.DEKKeyID = s.credKeyID
	return m, nil
}

func (s *service) logReconcile(out reconcileOutcome) {
	attrs := []any{"repository", out.Repository, "username", out.Username, "action", out.Action}
	if out.Entries > 0 {
		attrs = append(attrs, "entries", out.Entries)
	}
	if out.TruncatedBytes > 0 {
		attrs = append(attrs, "truncatedBytes", out.TruncatedBytes, "truncatedEntries", out.TruncatedEntries)
	}
	if out.Action == reconcileSkipped {
		s.log.Warn("substrate: repository directory has no row and no manifest; left alone", attrs...)
		return
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
	// At boot only: a creation's self-description is under the authority by
	// construction, and a stray one is what an import of an older changelog
	// leaves (correctSelfDescription).
	if allowImport {
		if out.StrayRepositoryID, err = strayRepositoryRecord(ctx, ds.db, repo.ID); err != nil {
			return out, err
		}
	}
	if fresh && out.Action == reconcileCaughtUp {
		out.Action = reconcileWroteDir
	}
	if _, err := s.ensureManifest(ctx, dir, repo, ds.db); err != nil {
		return out, err
	}
	return out, nil
}

// bareDataset is a dataset with no registry, no writer and no open ladder: the
// shape the reconcile and the import fold through. It never serves a request
// and is closed by its caller.
func (s *service) bareDataset(repo Repository, db *sql.DB, dir string) *dataset {
	return &dataset{
		svc: s, db: db, scope: repo.scope(), dir: dir, generation: repo.HistoryGeneration,
		reg: vocabulary.NewRegistry(), watch: newBroadcaster(), info: repo.info(),
	}
}

// reconcileDir is the head comparison and its consequences over the dataset's
// directory: it opens the changelog (cutting an incomplete tail), compares heads and
// the common tail, appends what the table has and the file lacks, imports what
// the file has and the table lacks when allowed, and then mirrors the sealed
// store in the same direction. It runs on a bare dataset, which has no writer:
// the writer an append needs is opened over the log and closed with the
// append, so the lock it holds is released before any dataset opens its own.
func (ds *dataset) reconcileDir(ctx context.Context, out *reconcileOutcome, allowImport bool) error {
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	log, err := changelogfile.Open(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return directoryOpenErr(err)
	}
	out.TruncatedBytes, out.TruncatedEntries = log.TruncatedBytes, log.TruncatedEntries
	fileHead := log.Head()
	if err := compareTails(ctx, ds.db, log, min(tableHead, fileHead)); err != nil {
		return err
	}
	out.Action = reconcileOK
	switch {
	case tableHead > fileHead:
		w, err := log.Writer(ds.svc.writerOptions())
		if err != nil {
			return writerErr(err)
		}
		n, err := appendFromTable(ctx, ds.db, w, fileHead, ds.svc.catchUpBatch)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		out.Action, out.Entries = reconcileCaughtUp, n
	case fileHead > tableHead:
		if !allowImport {
			return fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileAhead, fileHead, tableHead)
		}
		n, err := ds.importEntries(ctx, log, tableHead)
		if err != nil {
			return err
		}
		out.Action, out.Entries = reconcileImported, n
		return nil
	}
	// Equal heads say the rows are all there and nothing about the fold: an
	// import that died after its last batch left the marker, and the fold is
	// rebuilt before anything reads it. A catch-up above a marked repository
	// is what a binary from before the marker leaves when it served one and
	// died between a commit and its append; the Log opened above predates
	// that append, so it is opened again and the refold sees every row.
	markedHead, incomplete, err := importIncomplete(ctx, ds.db)
	if err != nil {
		return err
	}
	if !incomplete {
		return mirrorSealedFromTable(ctx, ds.db, ds.dir)
	}
	if !allowImport {
		return importIncompleteErr(ds.info.Name, markedHead)
	}
	if out.Action == reconcileCaughtUp {
		if log, err = changelogfile.Open(changelogfile.ChangelogDir(ds.dir)); err != nil {
			return directoryOpenErr(err)
		}
	}
	if err := ds.completeImport(ctx, log, markedHead); err != nil {
		return err
	}
	out.Action = reconcileResumed
	return nil
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

// appendFromTable appends every table row above the writer's head (after) to
// the file, in pages of batch rows, and returns how many it wrote.
//
// Every append is whole transactions: the writer refuses anything else, and
// rotating only after an append is what keeps a transaction inside one
// segment. A page is trimmed back to its last row that ends a transaction, so
// a batch stays at most batch rows and the trimmed rows lead the next page;
// only a page with no boundary at all, one transaction longer than the batch,
// is extended forward to that transaction's `txn`. The rows the table holds
// are always whole transactions (a transaction commits or it does not), so
// the page always reaches its `txn`; a `txn` the table cannot reach is
// divergence.
//
// A row whose stamped `hash` is not the checksum of what it holds is
// RE-STAMPED only while the file is EMPTY (after == 0). That is the one-time
// migration from the store this replaced, whose `hash` was a chain hash no
// line can carry (decision 0050): the file has nothing to compare such a row
// against, so the row as it stands is what the file records, and the table's
// stamp is made to agree with it. Above a non-empty file every row was stamped
// by this format, so a mismatch is a row whose content or stamp changed after
// commit, and it is refused as divergence (case 4) rather than written out as
// history.
func appendFromTable(ctx context.Context, q dbx, w *changelogfile.Writer, after int64, batch int) (int64, error) {
	migrating := after == 0
	head, err := tableChangelogHead(ctx, q)
	if err != nil {
		return 0, err
	}
	var n int64
	for {
		page, err := scanChecksumPage(ctx, q, after, batch)
		if err != nil {
			return n, err
		}
		if len(page) == 0 {
			return n, nil
		}
		end := len(page)
		for end > 0 && !page[end-1].entry.fileEntry().EndsTransaction() {
			end--
		}
		if end > 0 {
			page = page[:end]
		} else {
			// The CHECK holds `txn` at or above its seq and nothing else, so a
			// `txn` past the head is refused before it sizes a query.
			last := page[len(page)-1].entry
			if last.Txn > head {
				return n, fmt.Errorf("%w: seq %d ends its transaction at %d, past the table's head %d",
					ErrChangelogDiverged, last.Seq, last.Txn, head)
			}
			rest, err := scanChecksumPage(ctx, q, last.Seq, int(last.Txn-last.Seq))
			if err != nil {
				return n, err
			}
			if int64(len(rest)) != last.Txn-last.Seq {
				return n, fmt.Errorf("%w: seq %d ends its transaction at %d and the table holds %d of the %d rows between them",
					ErrChangelogDiverged, last.Seq, last.Txn, len(rest), last.Txn-last.Seq)
			}
			page = append(page, rest...)
		}
		// The line encoded for the checksum check is the line the file gets,
		// as prepareLines hands the writer the bytes settleChecksums
		// stamped: one canonicalization per row, not two.
		lines := make([]changelogfile.Line, 0, len(page))
		for _, row := range page {
			e := row.entry.fileEntry()
			line, sum, err := changelogfile.Encode(e)
			if err != nil {
				return n, fmt.Errorf("substrate/engine: seq %d does not encode as a changelog line: %w", e.Seq, err)
			}
			if !bytes.Equal(row.hash, sum[:]) {
				if !migrating {
					return n, fmt.Errorf("%w: seq %d: the table's checksum is not the checksum of the row it holds", ErrChangelogDiverged, e.Seq)
				}
				if _, err := q.ExecContext(ctx, `UPDATE changelog SET hash = $2 WHERE seq = $1`, e.Seq, sum[:]); err != nil {
					return n, fmt.Errorf("substrate/engine: re-stamp the checksum of seq %d: %w", e.Seq, err)
				}
			}
			lines = append(lines, changelogfile.Line{Seq: e.Seq, Txn: e.Txn, Bytes: line})
			after = e.Seq
		}
		if err := w.AppendLines(lines); err != nil {
			return n, fmt.Errorf("substrate/engine: append seq %d..%d to the changelog file: %w", lines[0].Seq, after, err)
		}
		n += int64(len(lines))
	}
}

// The import's durable steps, as the test seam names them (testImportFault).
const (
	importAfterBatch     = "after a batch of rows committed"
	importAfterFirstFold = "after the first fold pass committed"
)

// importEntries loads sealed/ into the table, inserts every file entry above
// the table's head, in batches of one transaction each under the changelog
// lock, then rebuilds the fold from the files. The import-progress marker is
// written before the first batch and deleted by the transaction that commits
// the last fold pass (refoldFromFiles), so every crash window in between
// leaves a state the next boot finishes: the file still ahead resumes the
// rows from the new table head, and equal heads under the marker resume the
// fold (completeImport). No row is ever inserted twice, because `seq` is the
// key and the resume starts above what the table holds.
func (ds *dataset) importEntries(ctx context.Context, log *changelogfile.Log, tableHead int64) (int64, error) {
	batch := rebuildBatch
	if ds.svc.testImportBatch > 0 {
		batch = ds.svc.testImportBatch
	}
	if err := refuseRetiredEntriesInFiles(ds.info.Name, log, tableHead, batch); err != nil {
		return 0, err
	}
	if err := loadSealedFiles(ctx, ds.db, ds.dir); err != nil {
		return 0, err
	}
	if err := markImportIncomplete(ctx, ds.db, log.Head()); err != nil {
		return 0, err
	}
	var n int64
	after := tableHead
	for {
		entries, err := log.Read(after, batch)
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
		if err := ds.importFault(importAfterBatch); err != nil {
			return n, err
		}
	}
	if err := ds.refoldFromFiles(ctx, log); err != nil {
		return n, err
	}
	return n, nil
}

// refuseRetiredEntriesInFiles is the import's half of refuseRetiredLinkEntries:
// it reads the entries an import is about to insert, those above tableHead,
// and refuses a dialect-1 `link` or `unlink` op among them. It runs twice on
// a directory with no row: in importRepositoryDir BEFORE the `repositories`
// row and the dialect rows exist, so a refused directory reserves nothing,
// and in importEntries before the first batch commits, which is the one gate
// a row's own directory running ahead of its table passes through. The fold
// would refuse the same entry (fold.go foldRefuses), but only after
// insertEntries had written rows and set the import marker, leaving a
// repository no boot can finish importing. The manifest's dialect cannot
// stand in for this probe: a directory a pre-gate binary wrote from its
// tables carries whatever stamp that store had, entries included. An import
// is a restore, and the extra reads are the price of refusing with the
// database untouched.
func refuseRetiredEntriesInFiles(repository string, log *changelogfile.Log, tableHead int64, batch int) error {
	after := tableHead
	for {
		entries, err := log.Read(after, batch)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			return nil
		}
		for _, e := range entries {
			if e.Op == opLinkRetired || e.Op == opUnlinkRetired {
				return fmt.Errorf("%w: repository %s: seq %d in the repository directory is a `%s` entry, which dialect 1 wrote and migration 0010 left nothing to fold into; there is no rung that translates it (decision 0044), so the directory cannot be imported",
					ErrChangelogPredatesReferences, repository, e.Seq, e.Op)
			}
		}
		after = entries[len(entries)-1].Seq
	}
}

// completeImport finishes an import whose rows are all in the table and whose
// marker is still set: a boot that died between the last batch and the last
// fold pass. sealed/ is loaded again because the direction is still the
// import's (the files are what is being restored), and the upsert is
// idempotent. markedHead is the file head the marker recorded; the log's own
// head is what the refold folds, and the two differ after a catch-up.
func (ds *dataset) completeImport(ctx context.Context, log *changelogfile.Log, markedHead int64) error {
	ds.svc.log.Warn("substrate: resuming an interrupted import of the repository directory",
		"repository", ds.scope.Repository, "username", ds.info.Name, "markedHead", markedHead, "fileHead", log.Head())
	if err := loadSealedFiles(ctx, ds.db, ds.dir); err != nil {
		return err
	}
	return ds.refoldFromFiles(ctx, log)
}

// importFault runs the test seam at one of the import's durable steps; a nil
// hook is the server.
func (ds *dataset) importFault(stage string) error {
	if ds.svc.testImportFault == nil {
		return nil
	}
	return ds.svc.testImportFault(stage)
}

// importIncomplete reports whether the repository's import-progress marker is
// set (an import began and the transaction that completes it has not
// committed) and the file head the marker recorded.
func importIncomplete(ctx context.Context, q dbx) (int64, bool, error) {
	var head int64
	err := q.QueryRowContext(ctx, `SELECT file_head FROM import_progress`).Scan(&head)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("substrate/engine: read the import-progress marker: %w", err)
	}
	return head, true, nil
}

// importIncompleteErr is the refusal a marked repository meets, naming the
// repository and the file head its import was bringing the table to.
func importIncompleteErr(repository string, markedHead int64) error {
	return fmt.Errorf("%w: repository %s, marked at file head %d", ErrImportIncomplete, repository, markedHead)
}

// markImportIncomplete sets the marker, or moves its head when a resumed
// import finds one already there.
func markImportIncomplete(ctx context.Context, q dbx, fileHead int64) error {
	if _, err := q.ExecContext(ctx, `
		INSERT INTO import_progress (file_head) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET file_head = EXCLUDED.file_head`, fileHead); err != nil {
		return fmt.Errorf("substrate/engine: mark the import in progress: %w", err)
	}
	return nil
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
		var causedBy, txn sql.NullInt64
		if e.CausedByOK {
			causedBy = sql.NullInt64{Int64: e.CausedBy, Valid: true}
		}
		if e.Txn != 0 {
			txn = sql.NullInt64{Int64: e.Txn, Valid: true}
		}
		if _, err := t.exec(`
			INSERT INTO changelog (seq, ts, actor, principal, op, record_id, kind, payload, caused_by, txn, hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11)`,
			e.Seq, e.TS, e.Actor, e.Principal, e.Op, e.RecordID, e.Kind, []byte(e.Payload), causedBy, txn, sum[:]); err != nil {
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
//
// The second pass's transaction is also the one that deletes the
// import-progress marker, so the import is complete exactly when the fold is
// the live write's. The first pass commits on its own: a crash after it
// leaves records with no `refs` and unweighted `fts`, which is why the marker
// stays set until the second pass, and the resume runs both passes again
// (each clears the fold tables first). The registry load between them reads
// the first pass's committed rows through the pool, which is why the two
// passes need not share a transaction.
//
// The same transaction converges the vectors with the fold and queues what is
// missing (reconcileEmbeddings): the fold never reaches the live write's
// enqueueEmbed, and the vectors are not in the directory, so the queue is what
// stands in for them. Into an empty database that queues every embeddable
// property; over an older database dump restored beside a newer directory it
// deletes the vectors of values the directory has since changed or cleared
// (a cleared property is not enqueued, so its vector would otherwise be
// scored for good) and queues only what changed. Under the marker's
// transaction a resumed import queues too, and a crash before the commit
// leaves nothing half-done.
func (ds *dataset) refoldFromFiles(ctx context.Context, log *changelogfile.Log) error {
	queued := 0
	replay := func(last bool) error {
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
		if last {
			n, err := ds.reconcileEmbeddings(ctx, tx, t.now)
			if err != nil {
				return err
			}
			queued = n
			if _, err := t.exec(`DELETE FROM import_progress`); err != nil {
				return fmt.Errorf("clear the import-progress marker: %w", err)
			}
		}
		return tx.Commit()
	}
	if err := replay(false); err != nil {
		return fmt.Errorf("substrate/engine: import: first fold: %w", err)
	}
	if err := ds.importFault(importAfterFirstFold); err != nil {
		return err
	}
	if err := ds.loadDeclarationsForReplay(ctx); err != nil {
		return err
	}
	if err := replay(true); err != nil {
		return fmt.Errorf("substrate/engine: import: second fold: %w", err)
	}
	if queued > 0 {
		ds.svc.log.Info("substrate: import queued the repository's embeddable properties for the drain",
			"repository", ds.scope.Repository, "username", ds.info.Name, "queued", queued)
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
//
// A directory with NO MANIFEST is skipped, logged and left where it is: without
// one nothing says whose repository it is, so it can be neither imported nor
// safely deleted, and an operator reads the log. A manifest that is present
// and unreadable, or whose DEK the credential key does not open, refuses the
// boot: the first would import under a guessed identity, the second would
// import a repository whose sealed store no login could then open, and both
// are better refused here than discovered by the user.
func (s *service) importRepositoryDir(ctx context.Context, id string) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: id}
	dir, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return out, err
	}
	m, err := changelogfile.ReadManifest(dir)
	if errors.Is(err, os.ErrNotExist) {
		out.Action = reconcileSkipped
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("a directory with no `repositories` row must carry a manifest to import: %w", err)
	}
	out.Username = m.Username
	if err := validRepositoryID(m.Authority); err != nil {
		return out, fmt.Errorf("the manifest names an authority that cannot be a repository id: %w", err)
	}
	// Both reader requirements are checked here, before the row and before
	// insertEntries writes anything: a refusal from the fold, with the rows
	// already committed, is the outage the manifest exists to prevent.
	m = currentManifest(m)
	if err := newerChangelogDialect(m.Username, m.ChangelogDialect); err != nil {
		return out, err
	}
	if err := admitVocabularyDialect(m.Username, m.VocabularyDialect); err != nil {
		return out, err
	}
	if len(m.DEK) > 0 {
		dek, err := s.unwrapDEK(m.DEK, m.Authority, m.DEKKeyID)
		if err != nil {
			return out, fmt.Errorf("the DEK in the manifest of repository %s (%s) does not open: %w. Importing it would leave a repository whose sealed store no login can open; set the key the directory was written under, or move the directory out of the data root",
				m.Authority, m.Username, err)
		}
		// A marked manifest is a claim about the files, and the row it would
		// create refuses every legacy form for good, so the claim is proven
		// before the row exists: every file under sealed/ must open under the
		// DEK alone (0059). An unmarked directory is not checked here; its
		// first open re-keys what it can and refuses, by ref, what it cannot.
		if m.SealedDEKOnly {
			files, err := changelogfile.ReadSealed(dir)
			if err != nil {
				return out, err
			}
			if err := sealedFilesOpenUnder(files, dek); err != nil {
				return out, fmt.Errorf("the manifest of repository %s (%s) says sealedDekOnly, but %w; the directory is refused rather than imported as a repository that would refuse that payload for good. Restore the directory from a copy whose files open, or clear sealedDekOnly in %s so the first open re-keys the store",
					m.Authority, m.Username, err, changelogfile.ManifestName)
			}
		}
	}
	if other, err := s.repositoryByUsername(ctx, m.Username); err == nil {
		return out, fmt.Errorf("the manifest names username %q, which repository %s already holds", m.Username, other.ID)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return out, err
	}
	// The entries' own gate, still before the row: a `link` entry refuses the
	// directory here, so it reserves neither the username nor the authority,
	// and a later boot finds no row to export an empty repository from. The
	// read-only open cuts nothing; reconcileDir opens the log again to repair.
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		return out, directoryOpenErr(err)
	}
	if err := refuseRetiredEntriesInFiles(m.Username, log, 0, rebuildBatch); err != nil {
		return out, err
	}
	// The directory is listed because no row has its authority as id, and the
	// authority column always equals the id; this is the check that the
	// manifest agrees with the directory name it was read under.
	// The marker is carried only beside a DEK: a manifest that claims a
	// DEK-only store with no DEK to be under is contradictory, and an unmarked
	// row costs one re-key pass at the first open, which is the safe reading.
	repo := Repository{
		ID: m.Authority, Username: m.Username, Authority: m.Authority, CreatedAt: m.CreatedAt,
		DEK: m.DEK, DEKKeyID: m.DEKKeyID, SealedDEKOnly: m.SealedDEKOnly && len(m.DEK) > 0,
	}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = nowUTC()
	}
	// A fresh generation, never one carried in the manifest: the directory
	// may be an older copy of a history this database's clients hold cursors
	// into, and a bare seq cannot tell the two apart. Every cursor saved
	// against the history that was here before is refused once and re-lists
	// (decision 0056).
	if repo.HistoryGeneration, err = newHistoryGeneration(); err != nil {
		return out, err
	}
	if _, err := s.maint.ExecContext(ctx, `
		INSERT INTO repositories (id, username, authority, created_at, dek, history_generation, dek_key_id, sealed_dek_only)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		repo.ID, repo.Username, repo.Authority, repo.CreatedAt, repo.DEK, repo.HistoryGeneration,
		nullString(repo.DEKKeyID), repo.SealedDEKOnly); err != nil {
		return out, fmt.Errorf("create the row from the manifest: %w", err)
	}
	if err := s.stampDialectsFromManifest(ctx, repo, m); err != nil {
		return out, err
	}
	return s.reconcileRow(ctx, repo, true)
}

// stampDialectsFromManifest stamps the imported repository with the dialects
// its manifest recorded, so the store carries the WRITER's requirements: the
// changelog gate then probes or refuses what the writer stamped, and the
// vocabulary ladder judges the rows' shape rather than assuming this binary's
// maximum. A dialect the manifest left at 0 (a changelog nobody had claimed)
// stamps nothing, as the writer had not.
func (s *service) stampDialectsFromManifest(ctx context.Context, repo Repository, m changelogfile.Manifest) error {
	if m.ChangelogDialect == 0 && m.VocabularyDialect == 0 {
		return nil
	}
	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if m.ChangelogDialect > 0 {
		if _, err := db.ExecContext(ctx, changelogDialectStamp, m.ChangelogDialect); err != nil {
			return fmt.Errorf("stamp changelog dialect %d from the manifest: %w", m.ChangelogDialect, err)
		}
	}
	if m.VocabularyDialect > 0 {
		if _, err := db.ExecContext(ctx, vocabularyDialectStamp, m.VocabularyDialect); err != nil {
			return fmt.Errorf("stamp vocabulary dialect %d from the manifest: %w", m.VocabularyDialect, err)
		}
	}
	return nil
}

// manifestOf renders the row as its manifest, with both dialects read from the
// repository's own stamps: the manifest is the directory's record of what a
// binary must understand to read it, so it says what the stamps say.
func (s *service) manifestOf(ctx context.Context, repo Repository, q dbx) (changelogfile.Manifest, error) {
	changelog, err := readChangelogDialect(ctx, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	vocabulary, err := readVocabularyDialect(ctx, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	return changelogfile.Manifest{
		Format: changelogfile.ManifestFormat, Username: repo.Username,
		Authority: repo.Authority, CreatedAt: repo.CreatedAt,
		ChangelogDialect: changelog, VocabularyDialect: vocabulary, DEK: repo.DEK,
		DEKKeyID: repo.DEKKeyID, SealedDEKOnly: repo.SealedDEKOnly,
	}, nil
}

// ensureManifest writes the repository's manifest when it is missing, when it
// is in an older format or when what the row says has moved (a DEK adopted, a
// dialect stamped, the store marked DEK-only), and returns the manifest the
// directory now holds. The row is the truth for a repository that has one.
func (s *service) ensureManifest(ctx context.Context, dir string, repo Repository, q dbx) (changelogfile.Manifest, error) {
	want, err := s.manifestOf(ctx, repo, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	have, err := changelogfile.ReadManifest(dir)
	if err == nil && manifestsEqual(have, want) {
		return want, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return changelogfile.Manifest{}, err
	}
	if err := changelogfile.WriteManifest(dir, want); err != nil {
		return changelogfile.Manifest{}, err
	}
	return want, nil
}

func manifestsEqual(a, b changelogfile.Manifest) bool {
	return a.Format == b.Format && a.Username == b.Username && a.Authority == b.Authority &&
		a.CreatedAt.Equal(b.CreatedAt) && a.ChangelogDialect == b.ChangelogDialect &&
		a.VocabularyDialect == b.VocabularyDialect && bytes.Equal(a.DEK, b.DEK) &&
		a.DEKKeyID == b.DEKKeyID && a.SealedDEKOnly == b.SealedDEKOnly
}

// formatOneVocabularyDialect is the vocabulary dialect of every directory
// whose manifest is format 1. The format recorded none, and every release
// that wrote it, v0.46.0 through v0.53.0, stored declarations in dialect 3
// (decision 0047 landed before the manifest did), so a format-1 manifest
// says 3 by its format alone. This is the writer's format read off the
// directory, not the running binary's maximum: a binary whose maximum has
// moved on still stamps such an import at 3 and lets the ladder judge it.
const formatOneVocabularyDialect = 3

// currentManifest is the manifest as this binary writes it: a manifest read
// in format 1 gains the vocabulary dialect its format implies and the format
// this binary writes; one already in ManifestFormat is returned as read.
func currentManifest(m changelogfile.Manifest) changelogfile.Manifest {
	if m.Format == changelogfile.ManifestFormat {
		return m
	}
	m.Format = changelogfile.ManifestFormat
	m.VocabularyDialect = formatOneVocabularyDialect
	return m
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

// sealedStore is the sealed directory as the mirror writes it: one file per
// ref, replaced atomically or removed. changelogfile's is the one
// implementation the server has; a test swaps in one that fails, so a write's
// refusal on a sealed-file failure is checked without a failing filesystem
// (export_test.go BreakSealedStore).
type sealedStore interface {
	// Write replaces the record's file in one step: the boot's direction,
	// where the table already committed what is written.
	Write(repoDir string, rec changelogfile.SealedRecord) error
	// Stage writes the record's pending file and Commit renames it into
	// place: a live write's two steps around its Postgres commit.
	Stage(repoDir string, rec changelogfile.SealedRecord) error
	Commit(repoDir, ref string) error
	Delete(repoDir, ref string) error
}

// fileSealedStore is the sealed directory itself.
type fileSealedStore struct{}

func (fileSealedStore) Write(repoDir string, rec changelogfile.SealedRecord) error {
	return changelogfile.WriteSealed(repoDir, rec)
}

func (fileSealedStore) Stage(repoDir string, rec changelogfile.SealedRecord) error {
	return changelogfile.StageSealed(repoDir, rec)
}

func (fileSealedStore) Commit(repoDir, ref string) error {
	return changelogfile.CommitSealed(repoDir, ref)
}

func (fileSealedStore) Delete(repoDir, ref string) error {
	return changelogfile.DeleteSealed(repoDir, ref)
}

// sealedFiles is the dataset's sealed store: the files, unless a test seam
// replaced them.
func (ds *dataset) sealedFiles() sealedStore {
	if ds.sealed != nil {
		return ds.sealed
	}
	return fileSealedStore{}
}

// applySealedMirror runs the collected sealed operations against the
// directory.
func applySealedMirror(store sealedStore, dir string, ops []sealedMirrorOp) error {
	for _, op := range ops {
		var err error
		if op.delete {
			err = store.Delete(dir, op.rec.Ref)
		} else {
			err = store.Write(dir, op.rec)
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
	if err := applySealedMirror(fileSealedStore{}, dir, ops); err != nil {
		return err
	}
	// A pending file is a write staged before a commit the directory never
	// saw finish: its transaction rolled back or the process died before it
	// committed, and the record's file above is now the row either way, so
	// a payload the table carries is already in place and one it does not
	// is dropped here. Nothing loads a pending file (ReadSealed skips it).
	_, err = changelogfile.DiscardPendingSealed(dir)
	return err
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
// serve: the import-progress marker must be clear (ErrImportIncomplete), the
// directory must be at the table's head (a creation's directory
// write racing a first open is the one way it can be behind, and it is caught
// up here), never ahead, and the common tail must agree. It opens the
// dataset's writer over the same scan, which is where a second process meets
// the running server's lock (ErrChangelogLocked).
//
// A read-only service opens the directory read-only and stops at the
// comparison: it repairs no tail, appends nothing, mirrors nothing and
// opens no writer. Either head may be ahead of the other, because the server
// that owns the directory may be between a commit and its append, and only the
// common tail is held to agree.
func (ds *dataset) openDirectory(ctx context.Context) error {
	// Before either shape: a fold an import has not finished is not served,
	// read-only or not, and the open ladder behind this (the vocabulary
	// upgrade appends) must not run over it.
	markedHead, incomplete, err := importIncomplete(ctx, ds.db)
	if err != nil {
		return err
	}
	if incomplete {
		return importIncompleteErr(ds.info.Name, markedHead)
	}
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	if ds.svc.readOnly {
		log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		return compareTails(ctx, ds.db, log, min(tableHead, log.Head()))
	}
	log, err := changelogfile.Open(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return directoryOpenErr(err)
	}
	if log.Head() > tableHead {
		return fmt.Errorf("%w: file head %d, table head %d", ErrChangelogFileAhead, log.Head(), tableHead)
	}
	if err := compareTails(ctx, ds.db, log, log.Head()); err != nil {
		return err
	}
	w, err := log.Writer(ds.svc.writerOptions())
	if err != nil {
		return writerErr(err)
	}
	ds.writer = w
	if tableHead > log.Head() {
		n, err := appendFromTable(ctx, ds.db, w, log.Head(), ds.svc.catchUpBatch)
		if err != nil {
			return err
		}
		ds.svc.log.Warn("substrate: the changelog file was behind the table at open and was caught up",
			"repository", ds.scope.Repository, "entries", n)
	}
	return mirrorSealedFromTable(ctx, ds.db, ds.dir)
}

// directoryErr is the standing refusal after a post-commit step failed.
func (ds *dataset) directoryErr() error {
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	return ds.fileErr
}

// --- the steps of a commit (dataset.go commitAndMirror) ------------------------

// The stages of commitAndMirror at which a test seam fails a step or stops
// the process (export_test.go WithTestCommitFault), in the order they run.
const (
	commitBeforeManifest = "before-manifest"
	commitAfterManifest  = "after-manifest"
	commitAfterPrepare   = "after the sealed files are staged and the changelog lines are prepared"
	commitInDoubt        = "the commit reported failure after committing"
	commitAfterCommit    = "after the transaction committed"
)

// commitFault runs the test seam at one of the commit's steps; a nil hook is
// the server.
func (s *service) commitFault(stage string) error {
	if s.testCommitFault == nil {
		return nil
	}
	return s.testCommitFault(stage)
}

// commitSealed commits a transaction that touched the sealed table and no
// changelog row, outside inTx (a refreshed token, a teardown's deletes, a
// TOTP step consume), in commitAndMirror's order: the files are durable
// before the row commits, or the caller gets the error and no row. A
// read-only process is refused as inTx refuses it: it has no writer, and a
// row it committed would be one the directory never receives, a TOTP step
// spent in a database its backup does not know.
func (ds *dataset) commitSealed(tx *sql.Tx, ops []sealedMirrorOp) error {
	if ds.svc.readOnly {
		return ErrDirectoryReadOnly
	}
	return ds.commitAndMirror(tx, &txn{ds: ds, tx: tx, sealedMirror: ops})
}

// stageSealedBeforeCommit writes a transaction's sealed writes to their
// pending files: on disk and fsynced, and not the records until
// commitStagedSealed renames them into place after the Postgres commit. The
// record's own file is untouched, so an import that never sees the commit
// loads the payload the table held. Every ref is recorded BEFORE its stage
// runs, so a stage that renamed and then failed its fsync is still
// discarded. A failure part way discards what was staged and returns the
// error; the transaction has not committed, so nothing is lost. Called with
// writerMu held.
func (ds *dataset) stageSealedBeforeCommit(writes []sealedMirrorOp) ([]string, error) {
	staged := make([]string, 0, len(writes))
	for _, op := range writes {
		staged = append(staged, op.rec.Ref)
		if err := ds.sealedFiles().Stage(ds.dir, op.rec); err != nil {
			ds.discardStaged(staged)
			return nil, fmt.Errorf("%w: repository %s: stage sealed %s: %w", ErrDirectoryWrite, ds.info.Name, op.rec.Ref, err)
		}
	}
	return staged, nil
}

// discardStaged removes the pending files of a transaction that did not
// commit. A pending file this cannot remove is one the next boot removes
// (mirrorSealedFromTable), and it is read by nothing until then, so nothing is
// latched: no store holds the write. Called with writerMu held.
func (ds *dataset) discardStaged(staged []string) {
	for _, ref := range staged {
		if err := changelogfile.DiscardSealed(ds.dir, ref); err != nil {
			ds.svc.log.Error("substrate: could not discard a staged sealed file whose transaction did not commit; the next boot removes it",
				"repository", ds.scope.Repository, "username", ds.info.Name, "ref", ref, "error", err)
		}
	}
}

// writeManifestBeforeCommit rewrites the manifest with the changelog dialect
// the transaction is about to claim, BEFORE that transaction commits or
// appends: its lines are the first in the file the dialect covers, so the
// manifest must say so before they are there. Called by commitAndMirror with
// writerMu held, on the one transaction that stamps (txn.claimsChangelogDialect),
// and a step of its own so a commit path that orders its durable steps
// differently keeps it ahead of the first append. A failure refuses the
// transaction: nothing has committed, nothing is appended, and the next write
// claims again. A rollback after a successful write leaves a manifest that
// OVERSTATES until the next claim or boot, which an older binary meets as a
// refusal to import and never as a fold over entries it cannot read.
func (ds *dataset) writeManifestBeforeCommit(dialect int) error {
	if ds.manifest.ChangelogDialect == dialect {
		return nil
	}
	if err := ds.svc.commitFault(commitBeforeManifest); err != nil {
		return fmt.Errorf("substrate/engine: write %s with changelog dialect %d before the first entry in it: %w", changelogfile.ManifestName, dialect, err)
	}
	m := ds.manifest
	m.ChangelogDialect = dialect
	if err := changelogfile.WriteManifest(ds.dir, m); err != nil {
		return fmt.Errorf("substrate/engine: write %s with changelog dialect %d before the first entry in it: %w", changelogfile.ManifestName, dialect, err)
	}
	ds.manifest = m
	return ds.svc.commitFault(commitAfterManifest)
}

// commitStagedSealed renames a committed transaction's pending files into
// place, in the order they were staged. A failure is latched: the table holds
// the row and the record's file is still the old one, which the boot check
// rewrites from the table. Called with writerMu held.
func (ds *dataset) commitStagedSealed(staged []string) error {
	for _, ref := range staged {
		if err := ds.sealedFiles().Commit(ds.dir, ref); err != nil {
			ds.latchDirectoryErr(fmt.Errorf("commit the staged sealed file %s: %w", ref, err))
			return ds.fileErr
		}
	}
	return nil
}

// prepareLines writes a transaction's changelog lines through the writer's
// Prepare: on disk and fsynced, and not history until commitLines writes the
// final newline. It reports whether it prepared anything, so the steps after
// the commit know whether there is a transaction to end. Called with writerMu
// held.
//
// Two refusals here are the latch's and not a retry's. A seq gap is the
// table ahead of the file: a commit that reported failure after Postgres had
// committed (an in-doubt commit) had its lines cut as if it rolled back, and
// this transaction's first seq now follows a row the file lacks. A writer
// that failed (an I/O error, on this prepare or an earlier one) refuses every
// prepare until the process reopens the directory, so a retry cannot
// succeed. Both name the restart; the boot's catch-up is the repair for the
// first and the reopen for the second.
func (ds *dataset) prepareLines(pending []pendingEntry) (bool, error) {
	if len(pending) == 0 {
		return false, nil
	}
	lines := make([]changelogfile.Line, 0, len(pending))
	for _, e := range pending {
		lines = append(lines, changelogfile.Line{Seq: e.Seq, Txn: e.Txn, Bytes: e.Line})
	}
	if err := ds.writer.Prepare(lines); err != nil {
		if errors.Is(err, changelogfile.ErrSeqGap) || ds.writer.Err() != nil {
			ds.latchDirectoryErr(fmt.Errorf("prepare seq %d..%d: %w", lines[0].Seq, lines[len(lines)-1].Seq, err))
			return false, ds.fileErr
		}
		return false, fmt.Errorf("%w: repository %s: prepare seq %d..%d: %w", ErrDirectoryWrite, ds.info.Name, lines[0].Seq, lines[len(lines)-1].Seq, err)
	}
	return true, nil
}

// abortLines cuts the prepared lines back off the segment after the
// transaction did not commit. A failure to cut leaves a tail the next open
// cuts, and the writer refuses every later prepare with the same error, so
// nothing is latched: no write reached one store and not the other. Called
// with writerMu held.
func (ds *dataset) abortLines(prepared bool) {
	if !prepared {
		return
	}
	if err := ds.writer.Abort(); err != nil {
		ds.svc.log.Error("substrate: could not cut a prepared transaction that did not commit; the next open cuts it",
			"repository", ds.scope.Repository, "username", ds.info.Name, "error", err)
	}
}

// commitLines writes the newline that ends the prepared transaction in the
// file, after Postgres committed it. A failure is latched: the tables hold
// the write and the file does not, and the boot check appends it from the
// table. Called with writerMu held.
func (ds *dataset) commitLines(prepared bool) error {
	if !prepared {
		return nil
	}
	if err := ds.writer.Commit(); err != nil {
		ds.latchDirectoryErr(fmt.Errorf("end the prepared transaction in the file: %w", err))
		return ds.fileErr
	}
	return nil
}

// deleteSealedAfterCommit removes the sealed files a committed transaction
// deleted the rows of. A failure is latched: a file with no row is one the
// boot check removes. Called with writerMu held.
func (ds *dataset) deleteSealedAfterCommit(deletes []sealedMirrorOp) error {
	if err := applySealedMirror(ds.sealedFiles(), ds.dir, deletes); err != nil {
		ds.latchDirectoryErr(err)
		return ds.fileErr
	}
	return nil
}

// splitSealedOps separates a transaction's sealed writes from its deletes,
// each in the order recorded, for commitAndMirror's ordering.
func splitSealedOps(ops []sealedMirrorOp) (writes, deletes []sealedMirrorOp) {
	for _, op := range ops {
		if op.delete {
			deletes = append(deletes, op)
		} else {
			writes = append(writes, op)
		}
	}
	return writes, deletes
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
