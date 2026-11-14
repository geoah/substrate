package engine

// THE BOOT CHECK (decision 0262).
//
// The changelog table is a repository's history and Postgres confirms every
// write. What the data root holds of a repository, under
// <root>/repositories/<id>, is what Postgres cannot: its blob bytes and the
// webhook requests answered and not yet recorded (the spool). Its copy is the
// ARCHIVE, written behind the commits (archive.go).
//
// The boot check runs before anything is served, in three passes:
//
//  1. A directory a release before the archive wrote (a 0.x directory: a
//     manifest, segment files and sealed files beside the blobs) gives the
//     table what the table lacks, and then gives its history up. A directory
//     with no row is imported whole, its manifest becoming the row; one with
//     a row is held to the table where both hold lines, any line it holds
//     past the table's head is imported, and a disagreement refuses the boot,
//     naming the seq. Its manifest, segments and sealed files are then
//     removed: the table holds every line they did. The blobs and the spool
//     stay where they are.
//  2. The archive, where the server writes one: a repository it holds and
//     the database does not is restored from it, and one whose archive is
//     ahead of its table (a database restored from an older copy) takes the
//     lines past the table's head. It runs before the rows' conversions, so
//     those lines meet the table in the dialect both were written in and are
//     converted with the rest; lines of two dialects never share the table
//     (history.go meetDialect). A store that cannot be listed, and the
//     archive of a repository the table holds that cannot be read or does
//     not continue the table's history, are logged and skipped: the table is
//     that repository's history, and the copy never stops the server. A
//     restore that fails, and an import that has begun, refuse the boot as
//     the import of a directory always did, and the next boot resumes it.
//  3. Every row: an import or a conversion a previous boot began is finished,
//     and a history below this binary's changelog dialect is converted; the
//     fold is rebuilt from the table wherever the import-progress marker
//     says it is not the table's.
//
// A copy of a history in an earlier dialect is held to the table by the
// checksum each line had in that dialect, which the conversion recorded
// (conversion.go sameLineAsTable).
//
// Every import is one path (history.go importHistory) and every fold is
// rebuilt from the table, so an import cut short anywhere is finished by the
// next boot before the repository is served (ErrImportIncomplete).

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ErrChangelogDiverged is the boot check's refusal: two histories of one
// repository hold different lines under one seq, or one does not verify.
var ErrChangelogDiverged = errors.New("substrate/engine: two histories of the repository disagree")

// ErrImportIncomplete is the refusal a dataset open meets while the
// repository's import-progress marker is set: an import or a conversion began
// and the fold has not been rebuilt from the table, so what `records` holds is
// not the history's. The boot check finishes it; nothing else does.
var ErrImportIncomplete = errors.New("substrate/engine: the import of the repository's history has not completed; restart the server so the boot check finishes it")

// ErrDirectoryReadOnly is the refusal every write meets on a service opened
// with WithDirectoryReadOnly: this process is not the repositories' writer.
var ErrDirectoryReadOnly = errors.New("substrate/engine: the service is open read-only (WithDirectoryReadOnly) and refuses to write")

// The boot check's actions, as its log prints them.
const (
	reconcileOK        = "ok"
	reconcileImported  = "imported"
	reconcileResumed   = "resumed import"
	reconcileConverted = "converted"
	// reconcileAdopted is a 0.x directory whose lines the table holds,
	// removed but for its blobs and its spool.
	reconcileAdopted = "history moved to the table"
	// reconcileRestored is a repository restored from the archive.
	reconcileRestored = "restored from the archive"
	// reconcileSkipped is a 0.x directory with no row and no manifest:
	// nothing says whose it is, so it is neither imported nor deleted.
	reconcileSkipped = "skipped: no manifest"
)

// reconcileOutcome is what the boot check did for one repository.
type reconcileOutcome struct {
	Repository string
	Action     string
	// Entries is how many lines came into the table.
	Entries int64
}

// repositoryDir is the repository's directory under the data root.
func (s *service) repositoryDir(id string) (string, error) {
	return changelogfile.EnsureRepoDir(s.dataRoot, id)
}

// reconcileRepositories is the boot check (the header). A repository a pass
// cannot bring to serve is set aside (setaside.go), and the check goes on to
// the next; a failure of the server's own refuses the boot.
func (s *service) reconcileRepositories(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: boot check: list repositories: %w", err)
	}
	if repos, err = s.finishDeletionsAndTakes(ctx, repos); err != nil {
		return err
	}
	dirs, err := changelogfile.ListRepositoryDirs(s.dataRoot)
	if err != nil {
		return fmt.Errorf("substrate/engine: boot check: a directory under %s/ is not named by a repository id or authority; move it out of the data root: %w",
			changelogfile.RepositoriesDir, err)
	}
	byID := make(map[string]Repository, len(repos))
	for _, repo := range repos {
		byID[repo.ID] = repo
	}
	// What each directory holds is read through the gate too: a directory
	// that cannot be read is its repository's failure, never a directory
	// with no history whose row then passes as current.
	var history, adopted []string
	for _, id := range dirs {
		pass := copyPass(id, setAsideAtImport, func() string { return directoryName(s.dataRoot, id) })
		if repo, ok := byID[id]; ok {
			pass = rowPass(repo, setAsideAtCheck)
		}
		if err := s.through(ctx, pass, func() error {
			dir, err := changelogfile.RepoDir(s.dataRoot, id)
			if err != nil {
				return err
			}
			was, has, err := changelogfile.HistoryState(dir)
			if err == nil {
				err = s.importStage(discoverStage)
			}
			if err != nil {
				return fmt.Errorf("substrate/engine: boot check: read the directory %s: %w", dir, err)
			}
			switch {
			case was:
				adopted = append(adopted, id)
			case has:
				history = append(history, id)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// Said before the work, because an import of a long history takes
	// minutes: whoever is waiting on the listener learns why it is late.
	s.log.Info("substrate: boot check started", "repositories", len(repos), "directoriesWithHistory", len(history))
	for _, id := range history {
		if repo, ok := byID[id]; ok {
			if err := s.through(ctx, rowPass(repo, setAsideAtCheck), func() error {
				out, err := s.adoptDirectory(ctx, repo)
				if err != nil {
					return fmt.Errorf("substrate/engine: boot check: repository %s: %w", repo.label(), err)
				}
				s.logReconcile(out)
				return nil
			}); err != nil {
				return err
			}
			continue
		}
		pass := copyPass(id, setAsideAtImport, func() string { return directoryName(s.dataRoot, id) })
		if err := s.through(ctx, pass, func() error {
			repo, out, err := s.importDirectory(ctx, id)
			if err != nil {
				return fmt.Errorf("substrate/engine: boot check: repository directory %s: %w", id, err)
			}
			if out.Action != reconcileSkipped {
				byID[id], repos = repo, append(repos, repo)
			}
			s.logReconcile(out)
			return nil
		}); err != nil {
			return err
		}
	}
	if err := s.reconcileArchive(ctx, byID); err != nil {
		return err
	}
	for _, repo := range repos {
		if err := s.through(ctx, rowPass(repo, setAsideAtCheck), func() error {
			out, err := s.reconcileRow(ctx, repo)
			if err != nil {
				return fmt.Errorf("substrate/engine: boot check: repository %s: %w", repo.label(), err)
			}
			if out.Action != reconcileOK {
				s.logReconcile(out)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// A directory whose history a boot adopted and whose removal it did not
	// finish, after the rows are reconciled: its history goes only once the
	// table, which holds every line of it, is converted and folded
	// (historyUnsettled). Until then the directory is left as it is, at every
	// start, because it holds the one other copy of each line. With no row
	// the table holds nothing of it after all (a database restored from
	// before the upgrade), and it is left.
	for _, id := range adopted {
		dir, err := changelogfile.RepoDir(s.dataRoot, id)
		if err != nil {
			return err
		}
		repo, ok := byID[id]
		if !ok {
			s.log.Error("substrate: a directory's history was adopted by a database that no longer holds the repository; it is left where it is",
				"repository", id, "directory", dir)
			continue
		}
		if err := s.through(ctx, rowPass(repo, setAsideAtCheck), func() error {
			db, err := s.scopedDB(repo.scope())
			if err != nil {
				return err
			}
			why, err := s.historyUnsettled(ctx, db)
			if err != nil {
				return err
			}
			if why != "" {
				s.log.Warn("substrate: a directory's adopted history is kept until its repository's history is converted",
					"repository", id, "directory", dir, "reason", why)
				return nil
			}
			if err := changelogfile.RemoveHistory(dir, s.importStage); err != nil {
				return fmt.Errorf("substrate/engine: boot check: finish removing the adopted history of %s: %w", id, err)
			}
			s.log.Info("substrate: finished removing a directory's adopted history", "repository", id)
			return nil
		}); err != nil {
			return err
		}
	}
	if err := s.removeUnnamedStages(ctx, byID); err != nil {
		return fmt.Errorf("substrate/engine: boot check: remove the staged copies no take names: %w", err)
	}
	return nil
}

// reconcileInterrupted is the action a boot check cut short by its context
// logs, so the next boot's "resuming an interrupted import" has its cause in
// the log above it.
const reconcileInterrupted = "interrupted"

// logInterrupted logs the repository a canceled boot check stopped on, and
// the cancellation's cause (the signal, from cmd/substrated). A failure that
// is not a cancellation logs nothing here: the returned error is its record.
func (s *service) logInterrupted(ctx context.Context, repository string) {
	if ctx.Err() == nil {
		return
	}
	s.log.Warn("substrate: boot check interrupted; the next boot resumes it",
		"repository", repository, "action", reconcileInterrupted,
		"cause", context.Cause(ctx).Error())
}

func (s *service) logReconcile(out reconcileOutcome) {
	attrs := []any{"repository", out.Repository, "action", out.Action}
	if out.Entries > 0 {
		attrs = append(attrs, "entries", out.Entries)
	}
	if out.Action == reconcileSkipped {
		s.log.Warn("substrate: repository directory has no row and no manifest; left alone", attrs...)
		return
	}
	s.log.Info("substrate: repository checked", attrs...)
}

// reconcileRow finishes what the import-progress marker says is unfinished
// for one repository with a row, and converts a history below this binary's
// dialect. A marker whose head the table has not reached is an import whose
// source is the archive (a 0.x directory was read before this): it is read
// again. No dataset may be open or opening on the repository while it runs.
func (s *service) reconcileRow(ctx context.Context, repo Repository) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: repo.ID, Action: reconcileOK}
	// The lease first: this reconcile WRITES (an import, a conversion, a
	// fold), so a process that is not the repository's one writer is refused
	// here (writerlease.go, decision 0083).
	if err := s.lease.acquire(ctx, repo.ID); err != nil {
		return out, err
	}
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return out, err
	}
	ds := s.bareDataset(repo, db)
	defer ds.close()
	marked, incomplete, err := importIncomplete(ctx, db)
	if err != nil {
		return out, err
	}
	stored, err := readChangelogDialect(ctx, db)
	if err != nil {
		return out, err
	}
	converting := len(pendingConversions(s.conversion, stored)) > 0
	if !incomplete && !converting {
		return out, nil
	}
	var src historySource
	if incomplete {
		head, err := tableChangelogHead(ctx, db)
		if err != nil {
			return out, err
		}
		if head < marked {
			a, err := s.archiveSourceOf(ctx, db, repo.ID)
			if err != nil {
				return out, fmt.Errorf("%w: the import was reading the archive, which cannot be read now: %w", importIncompleteErr(repo.ID, marked), err)
			}
			src = a
		}
	}
	ds.svc.log.Warn("substrate: finishing an interrupted import or conversion",
		"repository", repo.ID, "markedHead", marked, "converting", converting)
	n, err := ds.importHistory(ctx, src)
	out.Entries = n
	switch {
	case err != nil:
	case incomplete:
		out.Action = reconcileResumed
	default:
		out.Action = reconcileConverted
	}
	return out, err
}

// bareDataset is a dataset with no registry and no open ladder: the shape an
// import folds through. It never serves a request and is closed by its
// caller.
func (s *service) bareDataset(repo Repository, db *sql.DB) *dataset {
	dir, _ := changelogfile.RepoDir(s.dataRoot, repo.ID)
	// No name, as an open's dataset holds none: the names cache answers by
	// id (namescache.go).
	ds := &dataset{
		svc: s, db: db, scope: repo.scope(), dir: dir, generation: repo.HistoryGeneration,
		reg: vocabulary.NewRegistry(), watch: newBroadcaster(),
		info: substrate.RepositoryInfo{ID: repo.ID, State: repo.info().State},
	}
	ds.reg.SetNames(ds.answersTo)
	return ds
}

// openLocalHistory opens a 0.x directory's segments READ-ONLY. THE UPGRADE
// NEVER HOLDS A WRITABLE HANDLE ON A 0.x DIRECTORY'S HISTORY: an unfinished
// transaction at the end of the highest segment is left on disk, and every
// read stops at the head before it. The release that wrote it cut that tail
// at its next open, but a release that committed a transaction to Postgres
// and died appending it to the files leaves a whole, checksummed line of it
// there, the one other copy of a row the table confirmed; only SetAsideHistory
// and RemoveHistory change anything of the history, behind their gates
// (giveUpDirectory, reconcileRepositories), and a test holds the engine to
// that (TestTheUpgradeNeverWritesA0xDirectorysHistory).
func openLocalHistory(dir string) (dirSource, error) {
	m, err := changelogfile.ReadManifest(dir)
	if err != nil {
		return dirSource{}, err
	}
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		return dirSource{}, fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
	}
	return dirSource{dir: dir, log: log, dialect: m.ChangelogDialect}, nil
}

// adoptDirectory is pass 1 for a 0.x directory with a row (the header).
func (s *service) adoptDirectory(ctx context.Context, repo Repository) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: repo.ID, Action: reconcileAdopted}
	if err := s.lease.acquire(ctx, repo.ID); err != nil {
		return out, err
	}
	dir, err := changelogfile.RepoDir(s.dataRoot, repo.ID)
	if err != nil {
		return out, err
	}
	src, err := openLocalHistory(dir)
	if err != nil {
		return out, err
	}
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return out, err
	}
	ds := s.bareDataset(repo, db)
	defer ds.close()
	if err := ds.compareHistories(ctx, src); err != nil {
		return out, err
	}
	if out.Entries, err = ds.takeHistory(ctx, src); err != nil {
		return out, err
	}
	return out, s.giveUpDirectory(ctx, ds, dir)
}

// giveUpDirectory is the end of a 0.x directory's import (rule 6), once the
// table holds every line it does, in its dialect, each row held to its own
// checksum (compareHistories): the directory is set aside first, so no boot
// reads its history again, then the table is converted and folded, then the
// set-aside files go, and only then. A conversion that refuses leaves them,
// and so does every start after it until the table is converted
// (reconcileRepositories). The new archive the next pass starts is
// published after.
func (s *service) giveUpDirectory(ctx context.Context, ds *dataset, dir string) error {
	if err := changelogfile.SetAsideHistory(dir, s.importStage); err != nil {
		return err
	}
	if err := ds.finishImport(ctx); err != nil {
		return err
	}
	if err := s.importStage(importBeforeCleanup); err != nil {
		return err
	}
	why, err := s.historyUnsettled(ctx, ds.db)
	if err != nil {
		return err
	}
	if why != "" {
		return fmt.Errorf("substrate/engine: the directory %s keeps its history: %s", dir, why)
	}
	return changelogfile.RemoveHistory(dir, s.importStage)
}

// historyUnsettled says why a repository's table may not yet let a 0.x
// directory's copy of its history go, "" once it may: the table stands at
// the chain's changelog dialect, every row held to its checksum by the
// conversion's walk, with no import marked incomplete. THE OLD COPY OUTLIVES
// THE DOUBT: until then the directory is the one other copy of each line.
func (s *service) historyUnsettled(ctx context.Context, db dbx) (string, error) {
	marked, incomplete, err := importIncomplete(ctx, db)
	if err != nil {
		return "", err
	}
	if incomplete {
		return fmt.Sprintf("its import is marked incomplete at head %d", marked), nil
	}
	stored, err := readChangelogDialect(ctx, db)
	if err != nil {
		return "", err
	}
	pending := pendingConversions(s.conversion, stored)
	if len(pending) == 0 {
		return "", nil
	}
	head, err := tableChangelogHead(ctx, db)
	if err != nil || head == 0 {
		return "", err
	}
	return fmt.Sprintf("its history is in changelog dialect %d and not yet converted to %d", max(stored, 1), pending[len(pending)-1].Dialect), nil
}

// importDirectory is pass 1 for a 0.x directory with no row: its manifest
// becomes the row, its history the table's, and its own history files go. A
// directory with NO MANIFEST is skipped, logged and left where it is: nothing
// says whose repository it is, so it can be neither imported nor safely
// deleted. A manifest that is present and unreadable, or whose data key the
// credential key does not open, refuses the boot.
func (s *service) importDirectory(ctx context.Context, id string) (Repository, reconcileOutcome, error) {
	out := reconcileOutcome{Repository: id, Action: reconcileImported}
	dir, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return Repository{}, out, err
	}
	m, err := changelogfile.ReadManifest(dir)
	if errors.Is(err, os.ErrNotExist) {
		out.Action = reconcileSkipped
		return Repository{}, out, nil
	}
	if err != nil {
		return Repository{}, out, fmt.Errorf("a directory with no `repositories` row must carry a manifest to import: %w", err)
	}
	// An archive's directory (an export extracted) is restored from the
	// archive, never imported from here: its layout is not a 0.x one.
	if m.Format == changelogfile.ManifestFormatArchive {
		return Repository{}, out, fmt.Errorf("%s is a repository's archive (an export extracted, decision 0278), which the boot restores from the archive and not from the data root: move it to %s in the archive (by default under %s/archive) and boot again",
			dir, changelogfile.RepositoryPrefix(id), s.dataRoot)
	}
	// The boot gave every directory from before ids its id first
	// (repositoryid.go), so the manifest names it.
	if m.Format != changelogfile.ManifestFormat {
		return Repository{}, out, fmt.Errorf("the manifest of %s is format %d, which the boot converts before it imports; restart the server", dir, m.Format)
	}
	sealed, err := changelogfile.ReadSealed(dir)
	if err != nil {
		return Repository{}, out, err
	}
	src, err := openLocalHistory(dir)
	if err != nil {
		return Repository{}, out, err
	}
	archiveID, err := changelogfile.NewArchiveID(nowUTC())
	if err != nil {
		return Repository{}, out, err
	}
	repo, release, err := s.restoreRow(ctx, m, src.Head(), sealed, dir, archiveRecord{archive: archiveID})
	if err != nil {
		return repo, out, err
	}
	defer release()
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return repo, out, err
	}
	ds := s.bareDataset(repo, db)
	defer ds.close()
	if out.Entries, err = ds.takeHistory(ctx, src); err != nil {
		return repo, out, err
	}
	return repo, out, s.giveUpDirectory(ctx, ds, dir)
}

// restoreAdmission is what a restore's admission decided (admitRestore).
type restoreAdmission struct {
	// mint says the manifest's data key does not open under this host's key
	// and the operator listed the repository: the restore mints a new one,
	// and cause is why the old one did not open.
	mint  bool
	cause error
	// leftBehind says the restore keeps sealed values its key does not
	// open, as the record of their loss, and marks the row for the open to
	// clear them (lostsecrets.go).
	leftBehind bool
	// leftOut names the people the manifest carries whose sign-in does not
	// open under this host's key, in a repository the operator listed: the
	// restore leaves them out (archiveidentities.go).
	leftOut map[string]bool
}

// admitRestore is EVERY REFUSAL A RESTORE CAN MAKE, made here and nowhere
// else, so a check added later is made on every door: the restore itself
// (restoreRow), a take's staging before its choice is recorded
// (stageTakeInto), and a recorded take again before the boot removes
// anything (stageForTake). It writes nothing, and reads the credential key
// and SUBSTRATE_IMPORT_WITHOUT_SECRETS as they are when it runs, so a
// listing removed after a take was recorded refuses that take with the
// repository whole and the choice standing (the archive's contract, rule 1:
// a refusal changes nothing). where names the copy for a refusal.
func (s *service) admitRestore(ctx context.Context, m changelogfile.Manifest, sealed []changelogfile.SealedRecord, where string) (restoreAdmission, error) {
	var adm restoreAdmission
	if err := validRepositoryName(m.Authority); err != nil {
		return adm, fmt.Errorf("the manifest names an authority that cannot be a repository's name: %w", err)
	}
	// One name, one repository: a history whose name another repository in
	// this database holds is a copy of another repository, not this one. A
	// take's own repository holds its name under the same id.
	if other, err := s.repositoryByName(ctx, m.Authority); err == nil && other.ID != m.ID {
		return adm, &nameHeldError{where: where, name: m.Authority, other: other.ID}
	} else if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		return adm, err
	}
	if err := s.admitManifest(m); err != nil {
		return adm, err
	}
	if len(m.DEK) == 0 {
		return adm, fmt.Errorf("the manifest of repository %s carries no wrapped DEK, so nothing could open its sealed store; restore it from a copy whose manifest holds one",
			m.Authority)
	}
	dek, err := s.unwrapDEK(m.DEK, m.ID, m.DEKKeyID)
	if err != nil {
		if !s.acceptsLostSecrets(m.ID, m.Authority) {
			return adm, refuseUnopened(m.Authority, m.ID, err)
		}
		// A host with no credential key would store the new DEK unsealed.
		if len(s.credKey) == 0 {
			return adm, fmt.Errorf("repository %s is listed in %s, but this host has no credential key: restoring it without its secrets would store its new DEK unsealed, so nothing was restored. Set SUBSTRATE_CREDENTIAL_KEY and restart (the DEK in its manifest does not open: %w)",
				m.Authority, ImportWithoutSecretsVar, err)
		}
		// Nothing opens under the DEK the restore mints: every sealed value
		// and every spooled request is left behind.
		adm.mint, adm.cause, adm.leftBehind = true, err, true
		return adm, s.admitIdentities(m, &adm)
	}
	// Every sealed value must open under that DEK: a repository's payloads
	// are bound-framed under it and nothing else (0059), so one that does not
	// open is a copy to restore from elsewhere rather than a repository to
	// serve. A repository the operator listed is the exception: its values
	// that do not open are the secrets it left behind.
	if err := sealedFilesOpenUnder(sealed, dek); err != nil {
		if !s.acceptsLostSecrets(m.ID, m.Authority) {
			return adm, fmt.Errorf("the sealed store of repository %s does not open under the DEK its manifest carries: %w. Restore it from a copy whose values open, or list it in %s to restore it without the secrets that do not",
				m.Authority, err, ImportWithoutSecretsVar)
		}
		adm.leftBehind = true
	}
	return adm, s.admitIdentities(m, &adm)
}

// nameHeldError is admitRestore's refusal of a copy whose name another
// repository in this database holds. A door that has another way out says
// so beside it: the start's restore names the import under another name
// (reconcileArchive), and the import itself its --name.
type nameHeldError struct {
	where, name, other string
}

func (e *nameHeldError) Error() string {
	return fmt.Sprintf("%s is repository %s, and this database holds another repository of that name (id %s); move it out of the way or delete that repository",
		e.where, e.name, e.other)
}

// restoreRow checks a manifest a restore reads (a 0.x directory's, or an
// archive's) and makes it the repository's row, under the writer lease. The
// row, its dialect stamps and the import marker naming head commit in one
// transaction: a repository is never published without the mark that says
// its history is not all in, so a boot cut short after it refuses to serve
// it until an import finishes. The release it returns gives the lease back
// unless the import that follows completed: a repository whose history is in
// is this process's to write.
func (s *service) restoreRow(ctx context.Context, m changelogfile.Manifest, head int64, sealed []changelogfile.SealedRecord, where string, rec archiveRecord) (Repository, func(), error) {
	noop := func() {}
	if err := validRepositoryName(m.Authority); err != nil {
		return Repository{}, noop, fmt.Errorf("the manifest names an authority that cannot be a repository's name: %w", err)
	}
	// The name's lease first, held until the boot has published the
	// repository, as the keying holds it (holdName): a directory given its
	// id by a boot that died before importing it is resumed here, and a
	// second server with a copy still keyed by the name, or another archive
	// of the name, must meet the lease rather than find no row and take the
	// name under a second id.
	if err := s.holdName(ctx, m.Authority); err != nil {
		return Repository{}, noop, err
	}
	if err := s.importStage(importBeforeRow); err != nil {
		return Repository{}, noop, err
	}
	// THE WRITER LEASE BEFORE THE ROW, for the reason a creation takes it
	// before its seed (engine.go createSeededRepository): the insert below
	// publishes the repository while its changelog table is still empty, and
	// another server that saw the row would serve a repository with no
	// history.
	leased, err := s.lease.acquireNew(ctx, m.ID)
	release := func() {
		if leased {
			s.lease.release(m.ID)
		}
	}
	if err != nil {
		release()
		return Repository{}, noop, err
	}
	fail := func(err error) (Repository, func(), error) {
		release()
		return Repository{}, noop, err
	}
	// Every refusal before the row: a refusal from the fold, with the rows
	// already committed, is the outage the manifest exists to prevent.
	adm, err := s.admitRestore(ctx, m, sealed, where)
	if err != nil {
		return fail(err)
	}
	if adm.mint {
		// Without the key, the secrets stay behind because the operator
		// listed the repository (lostsecrets.go); the rest of the copy
		// restores as written.
		dek, err := s.leaveSecretsBehind(m.Authority, m.ID, adm.cause)
		if err != nil {
			return fail(err)
		}
		if m.DEK, err = s.wrapDEK(dek, m.ID); err != nil {
			return fail(err)
		}
		m.DEKKeyID = s.credKeyID
		if err := s.importStage(leftBehindStage); err != nil {
			return fail(err)
		}
	}
	repo := Repository{ID: m.ID, Authority: m.Authority, CreatedAt: m.CreatedAt, DEK: m.DEK, DEKKeyID: m.DEKKeyID}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = nowUTC()
	}
	// A fresh generation, never one carried in the manifest: the history may
	// be an older copy of one this database's clients hold cursors into, and
	// a bare seq cannot tell the two apart (decision 0056).
	if repo.HistoryGeneration, err = newHistoryGeneration(); err != nil {
		return fail(err)
	}
	if err := s.insertRestoredRow(ctx, repo, m, head, rec, adm); err != nil {
		return fail(err)
	}
	// The row is in, marked, and the import that follows is idempotent: a
	// crash from here leaves a marked repository the next boot finishes, so
	// the lease is kept whatever the import does.
	leased = false
	if err := s.importStage(importAfterRow); err != nil {
		return repo, noop, err
	}
	return repo, noop, nil
}

// insertRestoredRow is restoreRow's one transaction: the control-plane row,
// marked when the restore left secrets behind for the open to clear, the
// dialects the manifest recorded, so the store carries the WRITER's
// requirements rather than this binary's maximum (a dialect left at 0, a
// changelog nobody had claimed, stamps nothing, as the writer had not), and
// the import marker at head, and the record (rule 8): the new archive the
// repository's passes write, and the manifest a restore reads from, so an
// import a boot finishes reads that one snapshot, and the people the manifest
// carries (archiveidentities.go), so no crash leaves the repository without
// them. The scoped rows take their repository from the transaction's own
// setting, as a scoped connection's do.
func (s *service) insertRestoredRow(ctx context.Context, repo Repository, m changelogfile.Manifest, head int64, rec archiveRecord, adm restoreAdmission) error {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// A name another repository held as a former name whose hold has lapsed
	// is taken here as a registration takes it: the hold ends in this
	// transaction, and that repository's names are fenced around the COMMIT
	// (repositorynames.go takeFormerName, namescache.go).
	took, err := takeFormerName(ctx, tx, repo.Authority)
	if err != nil {
		return fmt.Errorf("take the name %s: %w", repo.Authority, err)
	}
	var fenced []string
	if took != "" {
		if err := lockNames(ctx, tx, took); err != nil {
			return err
		}
		fenced = append(fenced, took)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO repositories (id, authority, created_at, dek, history_generation, dek_key_id, secrets_left_behind)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		repo.ID, repo.Authority, repo.CreatedAt, repo.DEK, repo.HistoryGeneration,
		nullString(repo.DEKKeyID), adm.leftBehind); err != nil {
		return fmt.Errorf("create the row from the manifest: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('substrate.repository', $1, true)`, repo.ID); err != nil {
		return err
	}
	if m.ChangelogDialect > 0 {
		if _, err := tx.ExecContext(ctx, changelogDialectStamp, m.ChangelogDialect); err != nil {
			return fmt.Errorf("stamp changelog dialect %d from the manifest: %w", m.ChangelogDialect, err)
		}
	}
	if m.VocabularyDialect > 0 {
		if _, err := tx.ExecContext(ctx, vocabularyDialectStamp, m.VocabularyDialect); err != nil {
			return fmt.Errorf("stamp vocabulary dialect %d from the manifest: %w", m.VocabularyDialect, err)
		}
	}
	if err := markImportIncomplete(ctx, tx, head); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO archive_records (archive, source) VALUES ($1, nullif($2, ''))`, rec.archive, rec.source); err != nil {
		return fmt.Errorf("record the repository's new archive: %w", err)
	}
	remember, err := s.restoreIdentities(ctx, tx, m, adm.leftOut)
	if err != nil {
		return err
	}
	if err := s.commitFencingNames(tx, false, fenced...); err != nil {
		return err
	}
	remember()
	return nil
}

// reconcileArchive is pass 2 (the header): every repository the archive
// holds and the database does not is restored from it (rule 8). A
// repository the database holds is never taken from the archive here: the
// operator decides that (`substratectl archive resolve`). A restore that
// fails sets its repository aside (setaside.go), under the name its newest
// manifest gives, where one reads.
func (s *service) reconcileArchive(ctx context.Context, byID map[string]Repository) error {
	if err := s.restoreTakes(ctx, byID); err != nil {
		return err
	}
	if s.archive == nil {
		return nil
	}
	all, err := archivedRepositories(ctx, s.archive)
	if err != nil {
		s.log.Error("substrate: the archive cannot be listed; the boot goes on without restoring anything from it",
			"store", s.archive.String(), "error", err)
		return nil
	}
	for _, id := range sortedKeys(all) {
		if _, rowed := byID[id]; rowed {
			continue
		}
		var out reconcileOutcome
		var skipped error
		pass := copyPass(id, setAsideAtRestore, func() string { return archivedName(ctx, s.archive, id) })
		restored := false
		if err := s.through(ctx, pass, func() error {
			var err error
			var archiveID string
			if out, archiveID, skipped, err = s.restoreArchived(ctx, id, all[id], byID); err != nil {
				var held *nameHeldError
				if errors.As(err, &held) {
					return fmt.Errorf("substrate/engine: boot check: the archive of repository %s in %s is repository %s, and this database holds another repository of that name (id %s). Move %s out of the archive, delete that repository, or import it under another name with `substratectl archive import %s %s --name <name>`",
						id, s.archive.String(), held.name, held.other, changelogfile.RepositoryPrefix(id), id, archiveID)
				}
				return fmt.Errorf("substrate/engine: boot check: the archive of repository %s in %s: %w. To start without it, move %s out of the archive",
					id, s.archive.String(), err, changelogfile.RepositoryPrefix(id))
			}
			restored = true
			return nil
		}); err != nil {
			return err
		}
		if !restored {
			continue
		}
		switch {
		case errors.Is(skipped, errArchiveCovered):
			s.log.Info("substrate: a repository's archive ends in its deletion; it is not restored", "repository", id, "store", s.archive.String())
		case errors.Is(skipped, errArchiveAmbiguous):
			s.log.Error("substrate: a repository's archive holds two manifests of its highest epoch, and the boot does not choose between them; nothing of it is restored",
				"repository", id, "store", s.archive.String(), "error", skipped)
		case skipped != nil:
			s.log.Error("substrate: a repository's archive cannot be read; the boot goes on without it, and the next boot reads it again",
				"repository", id, "store", s.archive.String(), "error", skipped)
		case out.Action != reconcileOK:
			s.logReconcile(out)
		}
	}
	return nil
}

// restoreArchived restores one repository the database does not hold from
// the manifest rule 8 picks, into a new archive, and names the archive it
// read. skipped is an archive that is not restored now (ended by a deletion,
// two manifests of one epoch, a store that does not answer); err sets the
// repository aside.
func (s *service) restoreArchived(ctx context.Context, id string, listed archiveListing, byID map[string]Repository) (out reconcileOutcome, archiveID string, skipped, err error) {
	out = reconcileOutcome{Repository: id, Action: reconcileOK}
	archiveID, name, err := restorable(listed)
	if errors.Is(err, archive.ErrNotFound) {
		return out, archiveID, nil, nil
	}
	if err != nil {
		return out, archiveID, err, nil
	}
	src, err := openArchiveSource(ctx, s.archive, id, archiveID, name, s.admitManifest)
	if errors.Is(err, errArchiveUnreadable) || errors.Is(err, errArchiveCovered) || errors.Is(err, errArchiveAmbiguous) {
		return out, archiveID, err, nil
	}
	if err != nil {
		return out, archiveID, nil, err
	}
	sealed, err := src.Sealed(ctx)
	if errors.Is(err, errArchiveUnreadable) {
		return out, archiveID, err, nil
	}
	if err != nil {
		return out, archiveID, nil, err
	}
	next, err := changelogfile.NewArchiveID(nowUTC(), listed.ids()...)
	if err != nil {
		return out, archiveID, nil, err
	}
	out, err = s.restoreSource(ctx, src, sealed, next, byID)
	return out, archiveID, nil, err
}

// restoreFrom restores repository id, which the database does not hold,
// from manifest name of archive archiveID in store, into archive next.
func (s *service) restoreFrom(ctx context.Context, store archive.Store, id, archiveID, name, next string, byID map[string]Repository) (reconcileOutcome, error) {
	src, err := openArchiveManifest(ctx, store, id, archiveID, name, false, s.admitManifest)
	if err != nil {
		return reconcileOutcome{Repository: id}, err
	}
	sealed, err := src.Sealed(ctx)
	if err != nil {
		return reconcileOutcome{Repository: id}, err
	}
	return s.restoreSource(ctx, src, sealed, next, byID)
}

// restoreSource is a restore's row and import from src (rule 8).
func (s *service) restoreSource(ctx context.Context, src *archiveSource, sealed []changelogfile.SealedRecord, next string, byID map[string]Repository) (reconcileOutcome, error) {
	out := reconcileOutcome{Repository: src.repository, Action: reconcileOK}
	repo, release, err := s.restoreRow(ctx, src.manifest, src.Head(), sealed, src.String(),
		archiveRecord{archive: next, source: src.archive + "/" + src.name})
	if err != nil {
		return out, err
	}
	defer release()
	byID[src.repository] = repo
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return out, err
	}
	ds := s.bareDataset(repo, db)
	defer ds.close()
	if out.Entries, err = ds.importHistory(ctx, src); err != nil {
		return out, err
	}
	out.Action = reconcileRestored
	return out, nil
}

// archiveSourceOf opens the manifest a restore of repository id began from,
// as its record names it, for an import a boot finishes.
func (s *service) archiveSourceOf(ctx context.Context, db dbx, id string) (*archiveSource, error) {
	rec, ok, err := readArchiveRecord(ctx, db)
	if err != nil {
		return nil, err
	}
	archiveID, name, found := strings.Cut(rec.source, "/")
	if !ok || !found {
		return nil, fmt.Errorf("substrate/engine: the record of repository %s names no manifest a restore began from", id)
	}
	// A take's restore reads the copy staged for its recorded choice, never
	// the store again unless that copy is gone.
	if choice, ok, err := readTakeChoice(ctx, s.maint, id); err != nil {
		return nil, err
	} else if ok && choice.archive == archiveID && choice.manifest == name {
		if !s.stagedFor(id, choice) {
			if _, err := s.stageTake(ctx, id, &choice); err != nil {
				return nil, err
			}
		}
		store, err := s.stagedStore(id, choice)
		if err != nil {
			return nil, err
		}
		return openArchiveManifest(ctx, store, id, archiveID, name, false, s.admitManifest)
	}
	if s.archive == nil {
		return nil, errNoArchive
	}
	return openArchiveSource(ctx, s.archive, id, archiveID, name, s.admitManifest)
}

// archivedName is the name the newest manifest of repository id's archive in
// store gives, or "" when none reads: what keeps a set-aside restore's name
// from a registration.
func archivedName(ctx context.Context, store archive.Store, id string) string {
	if store == nil {
		return ""
	}
	src, err := openNewestArchive(ctx, store, id, nil)
	if err != nil {
		return ""
	}
	return src.manifest.Authority
}

// admitManifest refuses a manifest naming a dialect above this binary's.
func (s *service) admitManifest(m changelogfile.Manifest) error {
	if err := admitChangelogDialect(m.Authority, m.ChangelogDialect, s.maxDialect); err != nil {
		return err
	}
	return admitVocabularyDialect(m.Authority, m.VocabularyDialect)
}

// archivedRepositories is every repository the store holds manifests or
// deletion marks of, with what it holds of each, from one listing.
func archivedRepositories(ctx context.Context, store archive.Store) (map[string]archiveListing, error) {
	listed, err := store.List(ctx, changelogfile.RepositoriesDir+"/")
	if err != nil {
		return nil, err
	}
	out := map[string]archiveListing{}
	for _, o := range listed {
		k, ok := changelogfile.ParseArchiveKey(o.Key)
		if !ok || !k.Manifests || !vocabulary.ValidRepositoryID(k.Repository) {
			continue
		}
		l := out[k.Repository]
		if l == nil {
			l = archiveListing{}
			out[k.Repository] = l
		}
		a := l[k.Archive]
		if a == nil {
			a = &listedArchive{byEpoch: map[int64][]string{}}
			l[k.Archive] = a
		}
		if k.Name == changelogfile.DeletedMark {
			a.deleted = true
		} else if epoch, _, ok := changelogfile.ParseArchiveManifestName(k.Name); ok {
			a.byEpoch[epoch] = append(a.byEpoch[epoch], k.Name)
			a.top = max(a.top, epoch)
		}
	}
	for _, l := range out {
		for _, a := range l {
			for _, names := range a.byEpoch {
				slices.Sort(names)
			}
		}
	}
	return out, nil
}

func tableChangelogHead(ctx context.Context, q dbx) (int64, error) {
	var head int64
	if err := q.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return 0, fmt.Errorf("substrate/engine: read the changelog head: %w", err)
	}
	return head, nil
}

// compareHistories holds a 0.x directory to the table, line for line, before
// either is converted (rule 6): the one place lines are compared, and the
// last time a directory's word is taken. Both must be in one changelog
// dialect, every seq both reach must be held by both with one checksum, and
// neither may skip a seq. Past the shorter one's head the longer one's lines
// stand: the table's stay (the release that wrote the directory appended to
// it after the commit, so a crash left the table ahead), and the
// directory's are imported (a database restored from a backup older than the
// data root left the directory ahead).
//
// Every row of the table is held to its own checksum too, in the frame it
// was written in (framedLine), before the directory is given up: a column edited
// under an intact checksum would pass a comparison of checksums alone, the
// conversion would then refuse the row, and the directory, set aside by
// then, would hold the one good copy of it. So the comparison refuses it,
// naming the seq, and the repository is set aside with its directory as it
// was.
func (ds *dataset) compareHistories(ctx context.Context, src dirSource) error {
	tableDialect, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return err
	}
	if max(tableDialect, 1) != src.Dialect() {
		return fmt.Errorf("%w: the directory's lines are in changelog dialect %d and the table's in %d, and lines are compared in one dialect only",
			ErrChangelogDiverged, src.Dialect(), max(tableDialect, 1))
	}
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	both := min(tableHead, src.Head())
	for after := int64(0); after < both; {
		limit := int(min(int64(rebuildBatch), both-after))
		entries, err := src.log.Read(after, limit)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		stamps, err := scanChecksumPage(ctx, ds.db, after, limit)
		if err != nil {
			return err
		}
		for i := range limit {
			seq := after + int64(i) + 1
			if i >= len(entries) || entries[i].Seq != seq {
				return fmt.Errorf("%w: the directory does not hold seq %d, which the table does", ErrChangelogDiverged, seq)
			}
			if i >= len(stamps) || stamps[i].entry.Seq != seq {
				return fmt.Errorf("%w: the table does not hold seq %d, which the directory does", ErrChangelogDiverged, seq)
			}
			_, sum, err := changelogfile.Encode(entries[i])
			if err != nil {
				return fmt.Errorf("%w: seq %d: %w", ErrChangelogDiverged, seq, err)
			}
			if _, frame := framedLine(stamps[i]); frame == frameNone {
				return fmt.Errorf("%w: seq %d: the table's row is not the line its checksum was stamped for; the directory, which holds that line, is left as it is to repair the row from",
					ErrChangelogDiverged, seq)
			}
			if !bytes.Equal(sum[:], stamps[i].hash) {
				return fmt.Errorf("%w: seq %d: the table's line is not the directory's", ErrChangelogDiverged, seq)
			}
		}
		after += int64(limit)
	}
	// The rows past the directory's head stand as the table's, so they are
	// held to their own checksums too before the directory is given up: the
	// directory may still hold one of them, whole, in a transaction its
	// release never finished appending (openLocalHistory).
	for after := both; after < tableHead; {
		page, err := scanChecksumPage(ctx, ds.db, after, rebuildBatch)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if _, frame := framedLine(row); frame == frameNone {
				return fmt.Errorf("%w: seq %d: the table's row is not the line its checksum was stamped for; the directory is left as it is",
					ErrChangelogDiverged, row.entry.Seq)
			}
			after = row.entry.Seq
		}
	}
	return nil
}

// importFault runs the test seam at one of the import's durable steps; a nil
// hook is the server.
func (ds *dataset) importFault(stage string) error {
	if ds.svc.testImportFault == nil {
		return nil
	}
	return ds.svc.testImportFault(stage)
}

// The import's durable steps, as the test seam names them (testImportFault),
// beside the conversion's (conversion.go) and the removal of a 0.x
// directory's history (changelogfile.RemoveHistory*).
const (
	importBeforeRow      = "with the name held, before the restored repository's row"
	importAfterRow       = "after the restored repository's row committed"
	importAfterSealed    = "after the source's files and sealed values were taken in"
	importAfterBatch     = "after a batch of rows committed"
	importAfterFirstFold = "after the first fold pass committed"
	importBeforeCleanup  = "after the import finished, before the directory gave its history up"
)

// importStage runs the import's test seam at a step no dataset is open for.
// discoverStage is the boot check's read of one directory, for a test that
// stands its failure in for a directory the server may not read.
const discoverStage = "while the boot check reads a repository's directory"

func (s *service) importStage(stage string) error {
	if s.testImportFault == nil {
		return nil
	}
	return s.testImportFault(stage)
}

// importIncomplete reports whether the repository's import-progress marker is
// set (an import or a conversion began and the fold that completes it has
// not committed) and the head the marker recorded.
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
// repository and the head its import was bringing the table to.
func importIncompleteErr(repository string, markedHead int64) error {
	return fmt.Errorf("%w: repository %s, marked at head %d", ErrImportIncomplete, repository, markedHead)
}

// markImportIncomplete sets the marker, or moves its head when a resumed
// import finds one already there.
func markImportIncomplete(ctx context.Context, q dbx, head int64) error {
	if _, err := q.ExecContext(ctx, `
		INSERT INTO import_progress (file_head) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET file_head = EXCLUDED.file_head`, head); err != nil {
		return fmt.Errorf("substrate/engine: mark the import in progress: %w", err)
	}
	return nil
}

// insertEntries writes one batch of entries as changelog rows, each with the
// checksum its line carries, in one transaction holding the changelog lock.
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
			INSERT INTO changelog (seq, ts, actor, on_behalf_of, principal, op, record_id, kind, payload, caused_by, txn, hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12)`,
			e.Seq, e.TS, e.Actor, e.OnBehalfOf, lineToken(e), e.Op, e.RecordID, e.Kind, []byte(e.Payload), causedBy, e.Txn, sum[:]); err != nil {
			return fmt.Errorf("substrate/engine: import seq %d: %w", e.Seq, err)
		}
	}
	return tx.Commit()
}

// refold rebuilds the fold from the table in TWO passes. The fold consults the
// registry for the weighted search bands (fold.go foldFTS) and the reference
// sites (refs.go syncRefs), and the registry is built from declaration
// RECORDS that do not exist until the first pass has folded them. So the
// first pass folds under an empty registry to bring the declaration rows into
// being, the registry is loaded from them without writing anything, and the
// second pass folds every row the way the live write did. Nothing appends
// here: an import runs before any dataset is open.
//
// The second pass's transaction is also the one that deletes the
// import-progress marker, so the import is complete exactly when the fold is
// the live write's, and the operator's take the import carries out (rule 4),
// which no boot acts on after it. The first pass commits on its own: a crash after it
// leaves records with no `refs` and unweighted `fts`, which is why the marker
// stays set until the second pass, and the resume runs both passes again
// (each clears the fold tables first).
//
// The same transaction converges the vectors with the fold and queues what is
// missing (reconcileEmbeddings): the fold never reaches the live write's
// enqueueEmbed, and the vectors are in no archive, so the queue is what stands
// in for them.
func (ds *dataset) refold(ctx context.Context) error {
	queued := 0
	replay := func(last bool) error {
		tx, err := ds.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		t := &txn{
			ctx: ctx, ds: ds, tx: tx, actor: substrate.ActorSystem,
			now: nowUTC(), internal: true,
		}
		var report RebuildReport
		if err := t.rebuild(&report); err != nil {
			return err
		}
		if _, err := t.queueObservedRederivation(); err != nil {
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
			if _, err := t.exec(`DELETE FROM archive_takes WHERE repository_id = $1`, ds.info.ID); err != nil {
				return fmt.Errorf("end the take the import carried out: %w", err)
			}
		}
		// No token-cache fence (tokencache.go): a replay commits only where
		// nothing is served from the cache, at the boot before the server
		// listens or at a creation holding the repository's open slot, which
		// replays nothing. The repository's entries go after all, so the
		// claim costs nothing if it is ever wrong.
		if err := tx.Commit(); err != nil {
			return err
		}
		ds.svc.tokens.forget(ds.info.ID)
		return nil
	}
	if err := replay(false); err != nil {
		return fmt.Errorf("substrate/engine: import: first fold: %w", err)
	}
	if err := ds.importFault(importAfterFirstFold); err != nil {
		return err
	}
	if err := ds.loadDeclarationsForReplay(ctx, ds.db); err != nil {
		return err
	}
	if err := replay(true); err != nil {
		return fmt.Errorf("substrate/engine: import: second fold: %w", err)
	}
	if queued > 0 {
		ds.svc.log.Info("substrate: import queued the repository's embeddable properties for the drain",
			"repository", ds.scope.Repository, "queued", queued)
	}
	return nil
}

// loadDeclarationsForReplay fills the dataset's registry from its stored
// declaration rows and WRITES NOTHING: loadStoredVocabulary clears quarantine
// markers through a patch, which appends an entry, and an import may not
// append. A closure that does not admit is left out, which is what the open
// ladder does too, and parked the same way (fold.go parkedSet), so the second
// pass derives its rows' indexes as the live fold did.
func (ds *dataset) loadDeclarationsForReplay(ctx context.Context, q dbx) error {
	built, unparsed, err := ds.storedPackages(ctx, q, nil)
	if err != nil {
		return err
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	_, parked, _ := ds.admitStored(built)
	ds.parked = newParkedSet(ds.reg, parked, unparsedNames(unparsed))
	return nil
}

// manifestOf renders the row as its manifest, with both dialects read from the
// repository's own stamps: the manifest is the archive's record of what a
// binary must understand to read it, so it says what the stamps say.
func (s *service) manifestOf(ctx context.Context, repo Repository, q dbx) (changelogfile.Manifest, error) {
	// The name the repository has now, asked of the names cache by id as the
	// manifest is built (namescache.go): repo was read before, and a rename
	// may have committed since. A repository no row holds yet (a creation, an
	// import) has the name it is being written under.
	name := repo.Authority
	names, err := s.namesOf(ctx, repo.ID)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	if len(names) > 0 {
		name = names[0]
	}
	changelog, err := readChangelogDialect(ctx, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	vocabulary, err := readVocabularyDialect(ctx, q)
	if err != nil {
		return changelogfile.Manifest{}, err
	}
	return changelogfile.Manifest{
		Format: changelogfile.ManifestFormatArchive, ID: repo.ID,
		Authority: name, CreatedAt: repo.CreatedAt,
		ChangelogDialect: changelog, VocabularyDialect: vocabulary, DEK: repo.DEK,
		DEKKeyID: repo.DEKKeyID,
	}, nil
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

// --- the dataset's side ------------------------------------------------------

// openHistory is what an open asks of the repository's history before it
// serves: the import-progress marker must be clear (ErrImportIncomplete), so
// a fold an import has not finished is never served. A read-only process
// asks the same, and nothing more.
func (ds *dataset) openHistory(ctx context.Context) error {
	markedHead, incomplete, err := importIncomplete(ctx, ds.db)
	if err != nil {
		return err
	}
	if incomplete {
		return importIncompleteErr(ds.info.ID, markedHead)
	}
	if ds.svc.readOnly {
		return nil
	}
	return ds.eraseUnheldSealedAtOpen(ctx)
}

// eraseUnheldSealedAtOpen deletes every sealed row its owner does not hold
// (credentials.go sealedUnheld), so neither the archive nor `repository
// verify` meets one (#829). It runs here, with the fold complete and before
// the dataset serves, so no write sits between inserting a sealed row and
// storing its ref on the owner, the window sweepUnheldSealed closes with the
// changelog lock.
func (ds *dataset) eraseUnheldSealedAtOpen(ctx context.Context) error {
	res, err := ds.db.ExecContext(ctx, `DELETE FROM sealed s WHERE `+sealedUnheld)
	if err != nil {
		return fmt.Errorf("substrate/engine: erase sealed rows no record holds: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		ds.svc.log.Info("substrate: the open erased sealed rows no record holds",
			"repository", ds.scope.Repository, "rows", n)
	}
	return nil
}

// ownConnection dials a one-connection pool pinned to the repository, outside
// the shared pool's cap, for a caller that must not draw a second shared
// connection while it holds one. The caller closes it.
func (ds *dataset) ownConnection() (*sql.DB, error) {
	db, err := openScoped(ds.svc.dsn, ds.scope, ds.svc.appRole)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// The stages of a commit at which a test seam fails it or stops the process
// (export_test.go WithTestCommitFault).
const (
	commitBeforeCommit = "before the transaction commits"
	commitUnpublished  = "the commit reported failure after committing, before the registry published"
	commitInDoubt      = "the commit reported failure after committing"
	commitAfterCommit  = "after the transaction committed"
)

// commitFault runs the test seam at one of the commit's steps; a nil hook is
// the server.
func (s *service) commitFault(stage string) error {
	if s.testCommitFault == nil {
		return nil
	}
	return s.testCommitFault(stage)
}

// bulkFault runs the bulk seed's test seam at one stage of a flush.
func (s *service) bulkFault(stage string) error {
	if s.testBulkFault == nil {
		return nil
	}
	return s.testBulkFault(stage)
}

// commitSealed commits a transaction that touched the sealed table and no
// changelog row, outside inTx (a refreshed token, a teardown's deletes, a
// TOTP step consume). A read-only process is refused as inTx refuses it.
// tx and wc come from beginWrite, so a request that ends during the commit
// does not leave the row in doubt.
func (ds *dataset) commitSealed(tx *sql.Tx, wc *writeCtx) error {
	if ds.svc.readOnly {
		return ErrDirectoryReadOnly
	}
	if err := ds.svc.lease.err(); err != nil {
		return err
	}
	if fault := ds.testSealedFault.Load(); fault != nil {
		if err := (*fault)(); err != nil {
			return err
		}
	}
	return ds.commit(tx, &txn{ds: ds, tx: tx, wc: wc})
}
