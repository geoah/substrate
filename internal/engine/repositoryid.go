package engine

// REPOSITORY IDS (decision 0261).
//
// A repository's id keys its storage: the `repository` column of every table
// and the row security that reads it, the directory under the data root, the
// binding its data key is wrapped under, the binding of its spooled webhook
// requests, and the webhook address the server gives out. It is minted at
// creation (vocabulary.NewRepositoryID) and never changes. The name keeps
// every other job: what a person signs in with, the namespace of their own
// kinds, the actor names in history.
//
// A release before ids keyed all of that by the name. The boot converts each
// such repository once, before the boot check reads anything, in two halves:
//
//  1. Its rows (keyRow): ONE transaction on the maintenance pool mints the
//     id, rewrites the `repository` column of every repository-scoped table
//     from the name to it, and moves the control-plane row with its data key
//     rewrapped under the new binding. It is not a SQL migration, because it
//     writes the rows of the fold tables, and it is not a conversion step of
//     the changelog, because no line changes: a line names kinds and actors,
//     which stay the name's.
//  2. Its directory (keyDirectory): the manifest is rewritten in format 2
//     (the id beside the name), the spooled webhook requests are sealed again
//     under the id, and the directory is renamed from the name to the id. A
//     directory with no row, restored from a release before ids, takes a new
//     id the same way, and the boot check then imports it.
//
// A CRASH IS FINISHED BY THE NEXT BOOT. The rows commit in one transaction,
// so a repository is keyed by its name or by its id, never by both. A
// directory still named by the name is found by the authority its manifest
// names: its row says which id it takes, the manifest, rewritten first, fixes
// it for the steps after, and the steps that already ran find nothing left to
// do (a spooled file already sealed under the id opens under it).
//
// What it costs: the rows half rewrites every row of the repository, one
// UPDATE per table, while the boot waits; the directory half renames one
// directory and rewrites a handful of files.
//
// THE MIGRATIONS AFTER IT. A release before ids ran repository migrations 1
// to 5 and none after: migration 6 moves the repository's record to its id,
// and every later one comes after it. They run at the repository's first
// open after the keying, in order (repomigrate.go), which refuses a ledger
// that holds a later version and not an earlier one. So a test that makes a
// repository from before ids out of one this binary created removes every
// ledger row from 6 up, not 6 alone: the binary that created it ran them all
// (repositoryid_internal_db_test.go writeKeyedByName).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ErrRepositoryNotKeyed is the refusal a process that does not run the boot
// meets on a repository a release before ids left keyed by its name: only
// the boot of a server that writes converts it.
var ErrRepositoryNotKeyed = fmt.Errorf("%w: the repository is still keyed by its name; a server that writes converts it at its boot", substrate.ErrUnavailable)

// keyRepositories converts every repository and directory a release before
// ids left keyed by a name, rows first, then directories. One that cannot be
// converted is set aside (setaside.go), and nothing more is written to it;
// what its keying committed before the failure stays, and the next start
// carries it on.
func (s *service) keyRepositories(ctx context.Context) error {
	repos, err := s.listRepositories(ctx)
	if err != nil {
		return fmt.Errorf("substrate/engine: key repositories: %w", err)
	}
	for _, repo := range repos {
		if vocabulary.ValidRepositoryID(repo.ID) {
			continue
		}
		keyed := false
		if err := s.through(ctx, rowPass(repo, setAsideAtID), func() error {
			if err := s.keyRow(ctx, repo); err != nil {
				return fmt.Errorf("substrate/engine: give repository %s an id: %w", repo.Authority, err)
			}
			keyed = true
			return nil
		}); err != nil {
			return err
		}
		if !keyed {
			continue
		}
		if err := s.keyingFault(keyingAfterRow); err != nil {
			return err
		}
	}
	if s.testBeforeDirectories != nil {
		s.testBeforeDirectories(s)
	}
	keys, err := changelogfile.ListRepositoryDirs(s.dataRoot)
	if err != nil {
		return fmt.Errorf("substrate/engine: key repositories: a directory under %s/ is not named by a repository id or authority; move it out of the data root: %w",
			changelogfile.RepositoriesDir, err)
	}
	// The keying's own records are read before any directory is: what a
	// directory holds cannot vouch for itself once it is damaged.
	records, err := changelogfile.ReadKeyingRecords(s.dataRoot)
	if err != nil {
		return fmt.Errorf("substrate/engine: key repositories: read the keying's records: %w", err)
	}
	named := make(map[string]bool, len(keys))
	for _, key := range keys {
		named[key] = true
		if vocabulary.ValidRepositoryID(key) {
			continue
		}
		tied := s.recordedRow(ctx, key, records[key])
		id := tied
		keyed := false
		pass := passOver{keys: []string{key}, authority: key, stage: setAsideAtID, entry: func(cause error) *setAsideRepository {
			return s.keyingSetAside(ctx, key, id, cause)
		}}
		if tied != "" {
			pass.keys = append(pass.keys, tied)
		}
		if err := s.through(ctx, pass, func() error {
			var err error
			if id, err = s.keyDirectory(ctx, key, tied); err != nil {
				return fmt.Errorf("substrate/engine: give the repository directory %s its id: %w", key, err)
			}
			keyed = true
			return nil
		}); err != nil {
			return err
		}
		if !keyed {
			continue
		}
		if err := s.keyingFault(keyingAfterRename); err != nil {
			return err
		}
	}
	// A record whose directory is no longer under the name was finished by
	// a start cut short after the rename, or its directory went.
	for name := range records {
		if named[name] {
			continue
		}
		if err := changelogfile.RemoveKeyingRecord(s.dataRoot, name); err != nil {
			return fmt.Errorf("substrate/engine: key repositories: remove the keying's record of %s: %w", name, err)
		}
	}
	return nil
}

// recordedRow is the id of the row the keying's record ties the directory
// name to, or "": a record names an id whose row holds that name, or it is a
// keying that never committed and ties nothing.
func (s *service) recordedRow(ctx context.Context, name, id string) string {
	if id == "" {
		return ""
	}
	repo, err := s.repositoryByID(ctx, id)
	if err != nil || repo.Authority != name {
		return ""
	}
	return repo.ID
}

// keyRow moves one repository's rows from its name to a new id, in one
// transaction, and rewraps its data key under the id.
func (s *service) keyRow(ctx context.Context, repo Repository) error {
	if err := s.holdName(ctx, repo.ID); err != nil {
		return err
	}
	id, err := vocabulary.NewRepositoryID()
	if err != nil {
		return err
	}
	var wrapped []byte
	if len(repo.DEK) > 0 {
		dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
		if err != nil {
			return err
		}
		if wrapped, err = s.wrapDEK(dek, id); err != nil {
			return err
		}
	}
	if err := s.recordKeying(repo.ID, id); err != nil {
		return err
	}
	started := time.Now()
	s.log.Info("substrate: giving a repository an id: rewriting its rows",
		"repository", repo.Authority, "id", id)
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var rows int64
	for _, table := range repositoryScopedTables {
		res, err := tx.ExecContext(ctx, `UPDATE `+table+` SET repository = $1 WHERE repository = $2`, id, repo.ID)
		if err != nil {
			return fmt.Errorf("rekey %s: %w", table, err)
		}
		if n, err := res.RowsAffected(); err == nil {
			rows += n
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE repositories SET id = $1, dek = $2 WHERE id = $3`,
		id, wrapped, repo.ID); err != nil {
		return fmt.Errorf("move the repositories row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("substrate: gave a repository an id",
		"repository", repo.Authority, "id", id, "rows", rows, "took", time.Since(started).Round(time.Millisecond))
	return nil
}

// keyDirectory converts one directory still named by its repository's name:
// the manifest rewritten in format 2, the spooled requests sealed again under
// the id, the directory renamed. The id is the row's when the repository has
// one, and a new one for a directory being restored. The manifest goes first,
// because it fixes the id: every step after it reads the id from it, so a
// boot cut short after any step repeats the ones after it under the same id.
// The id is returned once the directory is tied to a repository, with a
// failure too, so the start sets aside the row of that id with it and gives
// back its lease (setaside.go). A directory is tied by one of two things
// only: the keying's own record (tied, recordedRow), which holds whatever
// the directory now holds, or a proof by its data key.
//
// NOTHING IS WRITTEN UNTIL THE DIRECTORY IS PROVEN THIS REPOSITORY'S. The
// writer lease under the name comes before the first read and is held until
// the boot check has published the repository under its id (holdName), so a
// second boot over the same database, with this directory or a copy of it in
// a data root of its own, is refused rather than minting a second id; the
// lease under the id is taken before the manifest names it. A row of the
// same name is the directory's only when both hold one data key: a name
// another repository took since the copy was made is a different
// repository, and its directory is refused untouched.
func (s *service) keyDirectory(ctx context.Context, name, tied string) (string, error) {
	dir, err := changelogfile.RepoDir(s.dataRoot, name)
	if err != nil {
		return "", err
	}
	if err := s.holdName(ctx, name); err != nil {
		return "", err
	}
	m, err := changelogfile.ReadManifestToKey(dir)
	if errors.Is(err, os.ErrNotExist) {
		if tied != "" {
			return tied, fmt.Errorf("the directory %s holds no manifest, and the keying recorded it as repository %s's when it gave that repository its id; its blobs and spool are in it", dir, tied)
		}
		// No manifest says whose it is: the boot check skips it and logs it,
		// as it always has.
		return "", nil
	}
	if err != nil {
		return tied, err
	}
	if m.Format == changelogfile.ManifestFormatBeforeIDs {
		target, dek, err := s.directoryKey(ctx, dir, m)
		if err != nil {
			return tied, err
		}
		if err := s.keyingReaches(target); err != nil {
			return target, err
		}
		if err := s.keyFree(dir, target); err != nil {
			return target, err
		}
		if err := s.lease.acquire(ctx, target); err != nil {
			return target, err
		}
		m.Format, m.ID = changelogfile.ManifestFormat, target
		if m.DEK, err = s.wrapDEK(dek, target); err != nil {
			return target, err
		}
		m.DEKKeyID = s.credKeyID
		if err := changelogfile.WriteManifestToKey(dir, m); err != nil {
			return target, err
		}
	} else {
		// The manifest was rewritten before a crash, and the id it names is
		// a claim until the row of that id proves it (claimedID).
		if err := s.claimedID(ctx, dir, m); err != nil {
			return tied, err
		}
		if err := s.keyingReaches(m.ID); err != nil {
			return m.ID, err
		}
		if err := s.lease.acquire(ctx, m.ID); err != nil {
			return m.ID, err
		}
		if err := s.keyFree(dir, m.ID); err != nil {
			return m.ID, err
		}
	}
	dek, err := s.unwrapDEK(m.DEK, m.ID, m.DEKKeyID)
	if err != nil {
		return m.ID, err
	}
	if err := resealSpool(dir, dek, name, m.ID, s.acceptsLostSecrets(m.ID, name)); err != nil {
		return m.ID, err
	}
	if err := s.renameRepositoryDir(dir, m.ID); err != nil {
		return m.ID, err
	}
	// The directory is under its id, so the record has done its work.
	return m.ID, changelogfile.RemoveKeyingRecord(s.dataRoot, name)
}

// errIDUnproven is a directory's manifest naming an id that the row of that
// id does not prove: the directory claims its name and nothing more, and
// nothing of the repository the id names is leased, read past its row,
// resealed, renamed into or set aside on the manifest's word.
var errIDUnproven = errors.New("substrate/engine: the directory's manifest names an id it is not proven to hold")

// claimedID proves the id a format-2 manifest in a directory still named by
// its authority names, before anything is done under it. A row of the name
// must be the row of the id; a row of the id must hold the same name and the
// same data key (the manifest's wrap opens under the id to the row's key).
// An id no row holds is the one a keying cut short gave a directory being
// restored, and is the directory's.
func (s *service) claimedID(ctx context.Context, dir string, m changelogfile.Manifest) error {
	if repo, err := s.repositoryByName(ctx, m.Authority); err == nil && repo.ID != m.ID {
		return fmt.Errorf("%w: the directory %s names id %s, and this database's repository %s is %s; move the directory out of the data root",
			errIDUnproven, dir, m.ID, m.Authority, repo.ID)
	} else if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		return err
	}
	repo, err := s.repositoryByID(ctx, m.ID)
	if errors.Is(err, substrate.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if repo.Authority != m.Authority {
		return fmt.Errorf("%w: the directory %s names id %s, which is this database's repository %s; move the directory out of the data root",
			errIDUnproven, dir, m.ID, repo.Authority)
	}
	own, err := s.unwrapDEK(m.DEK, m.ID, m.DEKKeyID)
	if err != nil {
		return fmt.Errorf("%w: the data key in the manifest of %s does not open under id %s: %w", errIDUnproven, dir, m.ID, err)
	}
	dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
	if err != nil {
		return fmt.Errorf("%w: the data key of repository %s does not open, so nothing proves the directory %s its: %w", errIDUnproven, m.Authority, dir, err)
	}
	if !bytes.Equal(dek, own) {
		return fmt.Errorf("%w: the directory %s names id %s, and their data keys differ; move the directory out of the data root",
			errIDUnproven, dir, m.ID)
	}
	return nil
}

// keyingReaches holds the keying of a directory to the start's one gate
// under the id it was proven to take: a repository already set aside is
// changed by nothing, so its proven directory is set aside with it, untouched.
func (s *service) keyingReaches(id string) error {
	if s.asideBefore(passOver{keys: []string{id}}) {
		return fmt.Errorf("%w: the directory is repository %s's, which this start set aside; nothing of either is changed", errAlreadyAside, id)
	}
	return nil
}

// recordKeying records, beside the directory still named by name, when
// there is one, that it is the repository of id, before the row's id
// commits: the tie every later start reads before it reads the directory
// (recordedRow). A record that names an id no row took is the trace of a
// keying that never committed, and ties nothing. A repository with no
// directory under its name gets none, so a directory that appears there
// later is a stranger's.
func (s *service) recordKeying(name, id string) error {
	if s.dataRoot == "" {
		return nil
	}
	dir, err := changelogfile.RepoDir(s.dataRoot, name)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) || (err == nil && !fi.IsDir()) {
		return nil
	} else if err != nil {
		return err
	}
	return changelogfile.WriteKeyingRecord(s.dataRoot, name, id)
}

// The keying's steps the import's test seam names (testImportFault):
// keyingAfterRow a row given its id, its directory not yet;
// keyingAfterRename a directory given its id, its repository not yet
// published under it.
const (
	keyingAfterRow    = "after a row took its id"
	keyingAfterRename = "after a directory took its id"
)

func (s *service) keyingFault(stage string) error {
	if s.testImportFault == nil {
		return nil
	}
	return s.testImportFault(stage)
}

// holdName takes the writer lease under a repository's name for the rest of
// the boot: until the boot check has published the repository under its id,
// the name is the only thing two processes converting it could both find.
func (s *service) holdName(ctx context.Context, name string) error {
	if err := s.lease.acquire(ctx, name); err != nil {
		return err
	}
	if !slices.Contains(s.keyingLeases, name) {
		s.keyingLeases = append(s.keyingLeases, name)
	}
	return nil
}

// releaseKeyingLeases lets go of the names once the boot check is done.
func (s *service) releaseKeyingLeases() {
	for _, name := range s.keyingLeases {
		s.lease.release(name)
	}
	s.keyingLeases = nil
}

// directoryKey is the id a format-1 directory takes and its data key in the
// clear: its row's when the repository has one and the directory is proven
// that repository's, a new id when no row holds the name. It writes nothing.
func (s *service) directoryKey(ctx context.Context, dir string, m changelogfile.Manifest) (string, []byte, error) {
	if len(m.DEK) == 0 {
		return "", nil, fmt.Errorf("the manifest of repository %s carries no wrapped DEK, so nothing could open its sealed store", m.Authority)
	}
	own, err := s.unwrapDEK(m.DEK, m.Authority, m.DEKKeyID)
	if err != nil {
		// A directory being restored (no row holds the name) may leave its
		// secrets behind if the operator listed it (lostsecrets.go); one a
		// row holds cannot be proven that row's without its data key.
		if _, rerr := s.repositoryByName(ctx, m.Authority); !errors.Is(rerr, substrate.ErrNotFound) {
			if rerr != nil {
				return "", nil, rerr
			}
			return "", nil, fmt.Errorf("the DEK in the manifest of repository %s does not open: %w", m.Authority, err)
		}
		if !s.acceptsLostSecrets("", m.Authority) {
			return "", nil, refuseUnopened(m.Authority, "", err)
		}
		id, ierr := vocabulary.NewRepositoryID()
		if ierr != nil {
			return "", nil, ierr
		}
		dek, lerr := s.leaveSecretsBehind(m.Authority, id, err)
		if lerr != nil {
			return "", nil, lerr
		}
		if ferr := s.keyingFault(leftBehindStage); ferr != nil {
			return "", nil, ferr
		}
		return id, dek, nil
	}
	repo, err := s.repositoryByName(ctx, m.Authority)
	switch {
	case errors.Is(err, substrate.ErrNotFound):
		id, err := vocabulary.NewRepositoryID()
		if err != nil {
			return "", nil, err
		}
		return id, own, nil
	case err != nil:
		return "", nil, err
	case !vocabulary.ValidRepositoryID(repo.ID):
		return "", nil, fmt.Errorf("repository %s has no id yet, so its directory cannot take one", m.Authority)
	}
	dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
	if err != nil {
		return "", nil, err
	}
	if !bytes.Equal(dek, own) {
		return "", nil, fmt.Errorf("%w: the directory %s is not this database's repository %s (id %s): their data keys differ, so another repository took the name since the directory was written. It is left as it was; move it out of the data root, or restore it into a database where the name is free",
			errDirectoryIsAnothers,
			dir, m.Authority, repo.ID)
	}
	return repo.ID, dek, nil
}

// keyFree refuses a target id whose directory already exists, before
// anything in dir is written.
func (s *service) keyFree(dir, id string) error {
	target, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("both %s and %s are this repository's directory; move the one that is not the truth out of the data root", dir, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// renameRepositoryDir moves a repository directory to the name its id gives
// it, refusing one already there: two directories for one repository are a
// copy an operator put beside it, and which is the truth is theirs to say.
func (s *service) renameRepositoryDir(dir, id string) error {
	target, err := changelogfile.RepoDir(s.dataRoot, id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("both %s and %s are this repository's directory; move the one that is not the truth out of the data root", dir, target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(dir, target); err != nil {
		return err
	}
	return changelogfile.SyncDir(filepath.Dir(dir))
}

// resealSpool seals every spooled webhook request of the directory again
// under the id: a spooled file is bound to its repository's key and fire
// (spoolAAD). A file already sealed under the id is left as it is, which is
// what a crash part way leaves.
func resealSpool(dir string, dek []byte, oldKey, id string, keepUnopened bool) error {
	files, err := changelogfile.SpoolFiles(dir)
	if err != nil || len(files) == 0 {
		return err
	}
	ds := &dataset{dek: dek}
	for _, f := range files {
		_, fire, err := changelogfile.ParseSpoolFileName(f.Name)
		if err != nil {
			return err
		}
		sealed, err := changelogfile.ReadSpoolFile(dir, f.Name)
		if err != nil {
			return err
		}
		if _, err := ds.openPayload(sealed, spoolAAD(id, fire)); err == nil {
			continue
		}
		raw, err := ds.openPayload(sealed, spoolAAD(oldKey, fire))
		if err != nil && keepUnopened {
			// Sealed under a data key the operator accepted losing: the open
			// of the repository restored without its secrets deletes it
			// (lostsecrets.go).
			continue
		}
		if err != nil {
			return fmt.Errorf("spooled webhook request %s: %w", f.Name, err)
		}
		again, err := ds.sealPayload(raw, spoolAAD(id, fire))
		if err != nil {
			return err
		}
		if err := changelogfile.WriteSpoolFile(dir, f.Name, again); err != nil {
			return err
		}
	}
	return nil
}
