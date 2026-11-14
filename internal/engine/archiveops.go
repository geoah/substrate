package engine

// THE OPERATOR'S HAND IN THE ARCHIVE (decision 0279). A pass that finds the
// archive not as the database left it stops and writes nothing (rule 4), and
// the choice between the two is the operator's, never merged: the report
// says what each side holds that the other lacks; keeping the database
// starts a new archive and leaves the old one for the operator to remove;
// taking the archive replaces the repository whole, at the next boot, by the
// newest archive's newest manifest, and starts a new archive. An explicit
// import brings back a repository whose archive a deletion mark covers. Each
// runs in an operator's process that holds the repository's writer lease, so
// none of it runs beside the server.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ArchiveListed is one id the store lists under a repository's manifests: an
// archive and its newest manifests, or a deletion mark.
type ArchiveListed struct {
	ID        string   `json:"id"`
	Epoch     int64    `json:"epoch"`
	Manifests []string `json:"manifests,omitempty"`
	Deleted   bool     `json:"deleted,omitempty"`
}

// ArchiveReport is what the database and the archive each hold that the
// other lacks (`substratectl archive report`).
type ArchiveReport struct {
	Repository string `json:"repository"`
	Store      string `json:"store"`
	// Archive is the archive the record names, Published its last published
	// manifest, Pending whether one is about to be, Stopped why the passes
	// stopped, and Take the manifest the next boot replaces the repository
	// by.
	Archive   string `json:"archive"`
	Published string `json:"published,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	Stopped   string `json:"stopped,omitempty"`
	Take      string `json:"take,omitempty"`
	// Listed is every archive and mark the store lists. Restorable is the
	// archive and manifest a boot with no rows would restore, empty when
	// none would, with NotRestorable saying why.
	Listed        []ArchiveListed `json:"listed"`
	Restorable    string          `json:"restorable,omitempty"`
	NotRestorable string          `json:"notRestorable,omitempty"`
	// The database against that manifest. NotCompared says why the lines
	// were not compared; otherwise Differing counts the seqs both hold with
	// other lines, FirstDiffering is the first, and OnlyDatabase and
	// OnlyArchive count the lines past the other's head.
	DatabaseHead   int64  `json:"databaseHead"`
	ArchiveHead    int64  `json:"archiveHead"`
	NotCompared    string `json:"notCompared,omitempty"`
	Differing      int64  `json:"differing"`
	FirstDiffering int64  `json:"firstDiffering,omitempty"`
	OnlyDatabase   int64  `json:"onlyDatabase"`
	OnlyArchive    int64  `json:"onlyArchive"`
	// SealedOnlyDatabase, SealedOnlyArchive and SealedDiffering are the
	// sealed values by ref; BlobsOnlyDatabase and BlobsOnlyArchive count
	// blobs by digest.
	SealedOnlyDatabase []string `json:"sealedOnlyDatabase,omitempty"`
	SealedOnlyArchive  []string `json:"sealedOnlyArchive,omitempty"`
	SealedDiffering    []string `json:"sealedDiffering,omitempty"`
	BlobsOnlyDatabase  int      `json:"blobsOnlyDatabase"`
	BlobsOnlyArchive   int      `json:"blobsOnlyArchive"`
}

// errNoArchiveRecorded refuses an operator's command on a repository whose
// database records no archive.
var errNoArchiveRecorded = fmt.Errorf("%w: the database records no archive of this repository", substrate.ErrNotFound)

// archiveStore is the store an operator's command reads, or errNoArchive.
func (s *service) archiveStore() (archive.Store, error) {
	if s.archive == nil {
		return nil, errNoArchive
	}
	return s.archive, nil
}

// ArchiveReport compares a repository's database with its archive.
func (s *service) ArchiveReport(ctx context.Context, repository string) (ArchiveReport, error) {
	store, err := s.archiveStore()
	if err != nil {
		return ArchiveReport{}, err
	}
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return ArchiveReport{}, err
	}
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return ArchiveReport{}, err
	}
	defer func() { _ = db.Close() }()
	report := ArchiveReport{Repository: repo.ID, Store: store.String()}
	rec, ok, err := readArchiveRecord(ctx, db)
	if err != nil {
		return report, err
	}
	if ok {
		report.Archive, report.Published, report.Pending, report.Stopped = rec.archive, rec.published, rec.pending != nil, rec.stopped
	}
	if choice, ok, err := readTakeChoice(ctx, s.maint, repo.ID); err != nil {
		return report, err
	} else if ok {
		report.Take = choice.String()
	}
	listed, err := listArchives(ctx, store, repo.ID)
	if err != nil {
		return report, err
	}
	for _, id := range listed.ids() {
		l := listed[id]
		report.Listed = append(report.Listed, ArchiveListed{ID: id, Epoch: l.top, Manifests: l.tops(), Deleted: l.deleted})
	}
	archiveID, name, err := restorable(listed)
	if err != nil {
		report.NotRestorable = err.Error()
		return report, nil
	}
	report.Restorable = archiveID + "/" + name
	src, err := openArchiveManifest(ctx, store, repo.ID, archiveID, name, false, nil)
	if err != nil {
		report.NotRestorable = err.Error()
		return report, nil
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer func() { _ = tx.Rollback() }()
	if report.DatabaseHead, err = tableChangelogHead(ctx, tx); err != nil {
		return report, err
	}
	report.ArchiveHead = src.Head()
	if err := compareWithArchive(ctx, tx, src, &report); err != nil {
		return report, err
	}
	sealed, err := readSealedTable(ctx, tx)
	if err != nil {
		return report, err
	}
	for _, ref := range sortedKeys(sealed) {
		name, ok := src.manifest.Sealed[ref]
		if !ok {
			report.SealedOnlyDatabase = append(report.SealedOnlyDatabase, ref)
			continue
		}
		_, sum, err := changelogfile.EncodeSealedVersion(sealed[ref])
		if err != nil {
			return report, err
		}
		if want, _, ok := changelogfile.ParseSealedObjectName(ref, name); !ok || want != sum {
			report.SealedDiffering = append(report.SealedDiffering, ref)
		}
	}
	for _, ref := range sortedKeys(src.manifest.Sealed) {
		if _, ok := sealed[ref]; !ok {
			report.SealedOnlyArchive = append(report.SealedOnlyArchive, ref)
		}
	}
	stored, err := storedBlobs(ctx, tx)
	if err != nil {
		return report, err
	}
	prefix := changelogfile.ArchivePrefix(repo.ID, archiveID)
	objects, err := store.List(ctx, prefix+changelogfile.BlobsSubdir+"/")
	if err != nil {
		return report, err
	}
	archived := map[string]bool{}
	for _, o := range objects {
		if digest, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix)); ok {
			archived[digest] = true
		}
	}
	held := map[string]bool{}
	for _, b := range stored {
		held[b.digest] = true
		if !archived[b.digest] {
			report.BlobsOnlyDatabase++
		}
	}
	for digest := range archived {
		if !held[digest] {
			report.BlobsOnlyArchive++
		}
	}
	return report, nil
}

// compareWithArchive counts the lines the table and the archive's manifest
// hold apart, every seq both reach, in one dialect only: lines are never
// converted to compare.
func compareWithArchive(ctx context.Context, tx *sql.Tx, src *archiveSource, report *ArchiveReport) error {
	dialect, err := readChangelogDialect(ctx, tx)
	if err != nil {
		return err
	}
	both := min(report.DatabaseHead, report.ArchiveHead)
	report.OnlyDatabase, report.OnlyArchive = report.DatabaseHead-both, report.ArchiveHead-both
	if max(dialect, 1) != src.Dialect() {
		report.NotCompared = fmt.Sprintf("the archive's lines are in changelog dialect %d and the database's in %d", src.Dialect(), max(dialect, 1))
		return nil
	}
	cur, err := src.Cursor(ctx, 0)
	if err != nil {
		return err
	}
	defer func() { _ = cur.Close() }()
	for after := int64(0); after < both; {
		entries, err := cur.Next(verifyTableBatch)
		if err != nil {
			report.NotCompared = err.Error()
			return nil
		}
		if len(entries) == 0 {
			break
		}
		stamps, err := scanStampPage(ctx, tx, after, len(entries))
		if err != nil {
			return err
		}
		for i, e := range entries {
			if e.Seq > both {
				break
			}
			_, sum, err := changelogfile.Encode(e)
			if err != nil {
				return err
			}
			if i >= len(stamps) || stamps[i].entry.Seq != e.Seq || string(stamps[i].hash) != string(sum[:]) {
				report.Differing++
				if report.FirstDiffering == 0 {
					report.FirstDiffering = e.Seq
				}
			}
			after = e.Seq
		}
	}
	return nil
}

// ArchiveResolution is what `archive resolve` did.
type ArchiveResolution struct {
	Repository string `json:"repository"`
	// Archive is the new archive keeping the database started, or Take the
	// manifest the next boot replaces the repository by.
	Archive string `json:"archive,omitempty"`
	Take    string `json:"take,omitempty"`
	// Left is the archive the database no longer writes, for the operator
	// to remove (`archive remove`).
	Left string `json:"left,omitempty"`
}

// ResolveArchive settles a repository whose archive is not as the database
// left it (rule 4): keepDatabase starts a new archive the next pass writes
// from the database whole, and leaves the old one where it is; otherwise
// the next boot replaces the repository whole by the newest archive's
// newest manifest, which must be restorable now.
func (s *service) ResolveArchive(ctx context.Context, repository string, keepDatabase bool) (ArchiveResolution, error) {
	store, err := s.archiveStore()
	if err != nil {
		return ArchiveResolution{}, err
	}
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return ArchiveResolution{}, err
	}
	if err := s.lease.acquire(ctx, repo.ID); err != nil {
		return ArchiveResolution{}, err
	}
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return ArchiveResolution{}, err
	}
	defer func() { _ = db.Close() }()
	out := ArchiveResolution{Repository: repo.ID}
	rec, ok, err := readArchiveRecord(ctx, db)
	if err != nil {
		return out, err
	}
	listed, err := listArchives(ctx, store, repo.ID)
	if err != nil {
		return out, err
	}
	if !keepDatabase {
		if !ok {
			return out, errNoArchiveRecorded
		}
		// The whole snapshot read, checked and on local disk before the
		// choice is recorded, beside the copy of any earlier choice: a
		// failure records nothing and leaves that copy as it was.
		choice, err := s.stageTake(ctx, repo.ID, nil)
		if err != nil {
			return out, err
		}
		if err := s.importStage(takeAfterStaging); err != nil {
			return out, err
		}
		out.Take = choice.String()
		if _, err := s.maint.ExecContext(ctx, `
			INSERT INTO archive_takes (repository_id, stage, archive, manifest) VALUES ($1, $2, $3, $4)
			ON CONFLICT (repository_id) DO UPDATE SET stage = EXCLUDED.stage, archive = EXCLUDED.archive,
				manifest = EXCLUDED.manifest, recorded_at = now()`,
			repo.ID, choice.stage, choice.archive, choice.manifest); err != nil {
			return out, err
		}
		return out, nil
	}
	after := listed.ids()
	if ok {
		after, out.Left = append(after, rec.archive), rec.archive
	}
	if out.Archive, err = changelogfile.NewArchiveID(nowUTC(), after...); err != nil {
		return out, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()
	// The old archive's rows go with it: its objects are the operator's to
	// remove now, and no pass deletes what the record does not hold.
	if _, err := tx.ExecContext(ctx, `DELETE FROM archive_objects`); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO archive_records (archive) VALUES ($1)
		ON CONFLICT (repository) DO UPDATE SET archive = EXCLUDED.archive, epoch = 0, published = NULL, published_bytes = NULL,
			pending = NULL, stopped = NULL, updated_at = now()`, out.Archive); err != nil {
		return out, err
	}
	// The later choice replaces a take recorded before it.
	if _, err := tx.ExecContext(ctx, `DELETE FROM archive_takes WHERE repository_id = $1`, repo.ID); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	_, err = s.maint.ExecContext(ctx, `UPDATE repositories SET archived_seq = 0, archived_at = NULL, archive_missing = 0 WHERE id = $1`, repo.ID)
	return out, err
}

// RemoveArchive removes one archive the database no longer writes, every
// object and manifest of it, and reports how many it removed. The archive
// the record names, and an id that carries a deletion mark, are refused.
func (s *service) RemoveArchive(ctx context.Context, repository, archiveID string) (int, error) {
	store, err := s.archiveStore()
	if err != nil {
		return 0, err
	}
	if !changelogfile.ValidArchiveID(archiveID) {
		return 0, fmt.Errorf("%w: %q is not an archive id", substrate.ErrValidation, archiveID)
	}
	id := repository
	if repo, err := s.repositoryByKey(ctx, repository); err == nil {
		id = repo.ID
		if err := s.lease.acquire(ctx, id); err != nil {
			return 0, err
		}
		db, err := s.scopedDB(repo.scope())
		if err != nil {
			return 0, err
		}
		rec, ok, err := readArchiveRecord(ctx, db)
		_ = db.Close()
		if err != nil {
			return 0, err
		}
		if ok && rec.archive == archiveID {
			return 0, fmt.Errorf("%w: archive %s is the one the database writes", substrate.ErrConflict, archiveID)
		}
	} else if !errors.Is(err, substrate.ErrNotFound) || !vocabulary.ValidRepositoryID(repository) {
		return 0, err
	}
	listed, err := listArchives(ctx, store, id)
	if err != nil {
		return 0, err
	}
	if l := listed[archiveID]; l != nil && l.deleted {
		return 0, fmt.Errorf("%w: %s is a deletion mark, which stays", substrate.ErrConflict, archiveID)
	}
	n := 0
	for _, prefix := range []string{changelogfile.ArchivePrefix(id, archiveID), changelogfile.ArchiveManifestsPrefix(id, archiveID)} {
		objects, err := store.List(ctx, prefix)
		if err != nil {
			return n, err
		}
		for _, o := range objects {
			if err := store.Delete(ctx, o.Key); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// ImportArchive restores, into a database that holds no row of it, a
// repository from the newest manifest of one archive the operator names:
// the explicit import a deletion mark leaves as the one way back (rule 7).
// It starts a new archive past every archive and mark the store lists.
//
// name, when set, is the name the repository takes in place of the one its
// manifest gives (decision 0259): a repository's own kinds are under `~`, so
// its name is in no record, and only the manifest says it. It is the repair
// of an archive the start set aside because another repository here holds
// its name (setaside.go). That set-aside is the start's, in this process's
// memory, and keeps nothing from an operator's action: the import reads the
// archive again and the override goes in before restoreRow, so admitRestore
// makes every refusal it makes at the start, under the new name. An archive
// that does not read, a data key that does not open (unless the new name or
// the id is listed in SUBSTRATE_IMPORT_WITHOUT_SECRETS) and a database that
// holds the id stay refusals. Nor may it take a name another repository the
// start set aside claims, which no registration may take either.
func (s *service) ImportArchive(ctx context.Context, repository, archiveID, name string) (Repository, error) {
	store, err := s.archiveStore()
	if err != nil {
		return Repository{}, err
	}
	if !vocabulary.ValidRepositoryID(repository) || !changelogfile.ValidArchiveID(archiveID) {
		return Repository{}, fmt.Errorf("%w: import takes a repository id and an archive id", substrate.ErrValidation)
	}
	if repo, err := s.repositoryByID(ctx, repository); err == nil {
		return repo, fmt.Errorf("%w: the database holds repository %s; `archive resolve --take-archive` replaces it", substrate.ErrConflict, repo.Authority)
	} else if !errors.Is(err, substrate.ErrNotFound) {
		return Repository{}, err
	}
	listed, err := listArchives(ctx, store, repository)
	if err != nil {
		return Repository{}, err
	}
	l := listed[archiveID]
	if tops := l.tops(); len(tops) != 1 {
		return Repository{}, fmt.Errorf("%w: archive %s holds %d manifests of its highest epoch", substrate.ErrConflict, archiveID, len(tops))
	}
	src, err := openArchiveManifest(ctx, store, repository, archiveID, l.tops()[0], false, s.admitManifest)
	if err != nil {
		return Repository{}, err
	}
	m := src.manifest
	if name != "" && name != m.Authority {
		if err := s.admitImportName(src, name); err != nil {
			return Repository{}, err
		}
		m.Authority = name
	}
	sealed, err := src.Sealed(ctx)
	if err != nil {
		return Repository{}, err
	}
	next, err := changelogfile.NewArchiveID(nowUTC(), listed.ids()...)
	if err != nil {
		return Repository{}, err
	}
	repo, release, err := s.restoreRow(ctx, m, src.Head(), sealed, src.String(), archiveRecord{archive: next, source: archiveID + "/" + src.name})
	var held *nameHeldError
	if errors.As(err, &held) {
		return repo, fmt.Errorf("%w; or import it under another name with --name", err)
	}
	if err != nil {
		return repo, err
	}
	defer release()
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return repo, err
	}
	ds := s.bareDataset(repo, db)
	defer ds.close()
	if _, err := ds.importHistory(ctx, src); err != nil {
		return repo, err
	}
	return repo, nil
}

// admitImportName is what an import under another name refuses beyond
// what admitRestore does. A history from before the local namespace spells
// its name in its lines, and its conversion moves that name to `~` by the
// row's (conversion step 6), so it is imported under its own name, and its
// next archive, in the current dialect, under another. And a name another
// repository the start set aside claims is not taken, as no registration
// takes it (setaside.go).
func (s *service) admitImportName(src *archiveSource, name string) error {
	if err := validRepositoryName(name); err != nil {
		return fmt.Errorf("%w: --name: %w", substrate.ErrValidation, err)
	}
	if src.Dialect() < localNamespaceDialect {
		return fmt.Errorf("%w: archive %s/%s was written in changelog dialect %d, whose lines spell the repository's name %s, so it is imported under that name only: import it without --name where that name is free, and import the archive the server writes next under another",
			substrate.ErrConflict, src.manifest.ID, src.name, src.Dialect(), src.manifest.Authority)
	}
	for _, r := range s.aside.all() {
		if r.authority == name && !slices.Contains(r.keys, src.manifest.ID) {
			return fmt.Errorf("%w: %s is the name of a repository the start set aside (%s); repair or remove it first, or choose another name",
				substrate.ErrConflict, name, r.keys[0])
		}
	}
	return nil
}

// finishDeletionsAndTakes is the boot's first look at every row: a
// repository whose deletion began is deleted (rule 7), and one whose
// operator's take is recorded loses its rows and its directory once the copy
// staged for that choice is whole, so the restore of the take replaces it
// (rule 4). It returns the repositories that stay.
//
// Which rows have one is read in one query over the control plane, whose
// failure is the database's; each repository's deletion and take then goes
// through the start's one gate.
func (s *service) finishDeletionsAndTakes(ctx context.Context, repos []Repository) ([]Repository, error) {
	began, takes, err := s.recordedDeletionsAndTakes(ctx)
	if err != nil {
		return nil, err
	}
	kept := repos[:0:0]
	for _, repo := range repos {
		switch {
		case began[repo.ID]:
			// The deletion is the owner's: nothing after this pass serves or
			// touches the repository, and it sets nothing aside, so the pass
			// names no stage. One that cannot finish, or whose repository is
			// aside, keeps deletion_began_at and is a later start's to try
			// again.
			if err := s.through(ctx, rowPass(repo, ""), func() error {
				s.finishDeletion(ctx, repo)
				return nil
			}); err != nil {
				return nil, err
			}
		case takes[repo.ID]:
			// A repository that is aside is left as it is, its take recorded
			// and waiting: the gate skips it, and the passes after skip it too.
			erased := false
			if err := s.through(ctx, rowPass(repo, setAsideAtTake), func() error {
				var err error
				erased, err = s.finishTake(ctx, repo)
				return err
			}); err != nil {
				return nil, err
			}
			if !erased {
				kept = append(kept, repo)
			}
		default:
			kept = append(kept, repo)
		}
	}
	return kept, nil
}

// recordedDeletionsAndTakes reads which rows hold a deletion that began and
// which a recorded take.
func (s *service) recordedDeletionsAndTakes(ctx context.Context) (began, takes map[string]bool, err error) {
	rows, err := s.maint.QueryContext(ctx, `
		SELECT r.id, r.deletion_began_at IS NOT NULL, t.repository_id IS NOT NULL FROM repositories r
		LEFT JOIN archive_takes t ON t.repository_id = r.id
		WHERE r.deletion_began_at IS NOT NULL OR t.repository_id IS NOT NULL`)
	if err != nil {
		return nil, nil, fmt.Errorf("substrate/engine: boot check: read the recorded deletions and takes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	began, takes = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var id string
		var deleting, taking bool
		if err := rows.Scan(&id, &deleting, &taking); err != nil {
			return nil, nil, err
		}
		began[id], takes[id] = deleting, taking
	}
	return began, takes, rows.Err()
}

// finishDeletion finishes the deletion of a repository a previous run began.
// A store that does not answer leaves it for the next start, the repository
// served by no one meanwhile.
func (s *service) finishDeletion(ctx context.Context, repo Repository) {
	s.log.Warn("substrate: finishing a repository's deletion a previous run began", "repository", repo.ID)
	if err := s.eraseRepository(ctx, repo.ID); err != nil {
		s.log.Error("substrate: a repository's deletion could not be finished; it is not served, and the next start tries again",
			"repository", repo.ID, "error", err)
	}
}

// finishTake carries out a repository's recorded take, and reports whether
// its rows and files are gone. A take whose copy the restore would refuse now
// is not a failure of the repository: it is left whole and served, its choice
// recorded, and the next start tries again. A failure once anything of it is
// being removed is its own, and sets it aside.
func (s *service) finishTake(ctx context.Context, repo Repository) (bool, error) {
	choice, ok, err := readTakeChoice(ctx, s.maint, repo.ID)
	if err != nil || !ok {
		return false, err
	}
	// Nothing of the repository goes until the copy staged for the
	// recorded choice is whole and admitted: a take that cannot stage, or
	// whose copy the restore would refuse, leaves it served as it was.
	if err := s.stageForTake(ctx, repo, choice); err != nil {
		s.log.Error("substrate: the archive its operator chose to take cannot replace the repository now; nothing of it was changed, and the next start tries again",
			"repository", repo.ID, "chose", choice.String(), "error", err)
		return false, nil
	}
	if err := s.importStage(takeAfterStaged); err != nil {
		return false, err
	}
	s.log.Warn("substrate: replacing a repository by its archive, as its operator chose", "repository", repo.ID, "chose", choice.String())
	if err := s.eraseLocal(ctx, s.maint, repo.ID); err != nil {
		return false, fmt.Errorf("substrate/engine: boot check: replace repository %s by its archive: %w", repo.ID, err)
	}
	return true, nil
}

// stageForTake makes sure the copy staged for a recorded take is whole and
// is that choice's: the one the command that recorded it staged, or, when
// that copy is not there whole, the recorded manifest staged again now. Only
// then does the boot remove the repository's rows and directory.
func (s *service) stageForTake(ctx context.Context, repo Repository, choice takeChoice) error {
	if err := s.lease.acquire(ctx, repo.ID); err != nil {
		return err
	}
	if !s.stagedFor(repo.ID, choice) {
		// Staging admits what it staged (stageTake).
		_, err := s.stageTake(ctx, repo.ID, &choice)
		return err
	}
	return s.admitStaged(ctx, repo.ID, choice)
}

// admitStaged is the take's admission of the copy staged for choice
// (admitSource), with the key and the listing as they are now: the choice
// was admitted when it was recorded, and a copy, a host key or a listing
// changed since must refuse the take before anything of the repository
// goes, not the restore after.
func (s *service) admitStaged(ctx context.Context, id string, choice takeChoice) error {
	store, err := s.stagedStore(id, choice)
	if err != nil {
		return err
	}
	src, err := openArchiveManifest(ctx, store, id, choice.archive, choice.manifest, false, nil)
	if err != nil {
		return err
	}
	return s.admitSource(ctx, src)
}

// admitSource is A RESTORE OF src READ TO ITS END AND WRITTEN NOWHERE: the
// restore's own readings (src.Sealed, src.CopyFiles and readHistory, the
// ones takeHistory runs) over every object the restore will read, with
// sinks that keep nothing, and the restore's one admission (admitRestore)
// over what they read. So every refusal the restore's reading can make is
// made here, by the same code, before a take removes anything of the
// repository: a piece that is not the bytes its manifest names, a line that
// does not verify, a sealed value, a spooled request or a blob that cannot
// be read. A take reads its copy twice, here and at the restore, and
// nothing records that this reading passed: the restore reads again.
func (s *service) admitSource(ctx context.Context, src *archiveSource) error {
	sealed, err := src.Sealed(ctx)
	if err != nil {
		return err
	}
	if _, err := s.admitRestore(ctx, src.manifest, sealed, src.String()); err != nil {
		return err
	}
	if err := src.CopyFiles(ctx, discardBlobs{}, func(string, []byte) error { return nil }); err != nil {
		return err
	}
	_, err = readHistory(ctx, src, 0, rebuildBatch, func([]changelogfile.Entry) error { return nil })
	return err
}

// discardBlobs is the blob store a take's admission copies into: it holds
// nothing, so every blob the archive holds is read, and it takes a blob as
// the server's store takes one, all of its bytes and as many as the archive
// lists.
type discardBlobs struct{}

func (discardBlobs) Put(_ context.Context, digest string, size int64, r io.Reader) error {
	n, err := io.Copy(io.Discard, r)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("blobbytes: got %d bytes of %s, the caller promised %d", n, digest, size)
	}
	return nil
}

func (discardBlobs) Exists(context.Context, string) (bool, error) { return false, nil }

func (discardBlobs) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, blobbytes.ErrNotStored
}

func (discardBlobs) Delete(context.Context, string) error { return nil }

func (discardBlobs) List(context.Context, string, int) ([]blobbytes.Object, error) { return nil, nil }

// takeAfterStaged is the take's step with its snapshot staged and nothing of
// the repository removed yet; takeAfterStaging the command's, with a choice
// staged and not yet recorded.
const (
	takeAfterStaged  = "after the take was staged, before the rows went"
	takeAfterStaging = "after a take was staged, before the choice was recorded"
)

// restoreTakes restores every repository whose take is recorded and whose
// rows are gone, from the copy staged for that choice, the boot's first
// restore, before the store is listed. The restore's last transaction ends
// the take (repodir.go refold), so a boot cut short in it resumes it from
// the same copy (archiveSourceOf).
func (s *service) restoreTakes(ctx context.Context, byID map[string]Repository) error {
	choices, err := readTakeChoices(ctx, s.maint)
	if err != nil {
		return err
	}
	for _, id := range sortedKeys(choices) {
		if _, rowed := byID[id]; rowed {
			continue
		}
		choice := choices[id]
		pass := copyPass(id, setAsideAtRestore, func() string {
			if store, err := s.stagedStore(id, choice); err == nil {
				if name := archivedName(ctx, store, id); name != "" {
					return name
				}
			}
			return archivedName(ctx, s.archive, id)
		})
		if err := s.through(ctx, pass, func() error {
			if !s.stagedFor(id, choice) {
				_, err := s.stageTake(ctx, id, &choice)
				if storeDidNotAnswer(err) {
					// Nothing of it was written (stageTake removes what it
					// staged), so it is left for a later start, as an archive
					// the store does not answer for is (reconcileArchive).
					s.log.Error("substrate: the archive's store did not answer, so the repository a take removed is not restored now; the next start stages it again",
						"repository", id, "chose", choice.String(), "store", s.archive.String(), "error", err)
					return nil
				}
				if err != nil {
					return fmt.Errorf("substrate/engine: boot check: the take of repository %s removed its rows, and the copy staged for it is gone and cannot be staged again: %w", id, err)
				}
			}
			store, err := s.stagedStore(id, choice)
			if err != nil {
				return err
			}
			out, err := s.restoreFrom(ctx, store, id, choice.archive, choice.manifest, choice.stage, byID)
			if err != nil {
				return fmt.Errorf("substrate/engine: boot check: restore repository %s from the copy staged for its take: %w", id, err)
			}
			s.logReconcile(out)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// removeUnnamedStages removes every staged copy no recorded take names: one
// whose take finished, was replaced by a later choice or ended by a
// deletion, and one staged for a choice never recorded. It is the boot's
// last step, so a copy a resumed restore read goes once the restore is done.
func (s *service) removeUnnamedStages(ctx context.Context, byID map[string]Repository) error {
	root := filepath.Join(s.dataRoot, takeRootDir)
	repos, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	choices, err := readTakeChoices(ctx, s.maint)
	if err != nil {
		return err
	}
	for _, r := range repos {
		id := r.Name()
		pass := copyPass(id, setAsideAtTake, func() string { return "" })
		if repo, ok := byID[id]; ok {
			pass = rowPass(repo, setAsideAtTake)
		}
		if err := s.through(ctx, pass, func() error {
			return removeUnnamedStagesOf(filepath.Join(root, id), choices[id], choices[id].stage != "")
		}); err != nil {
			return err
		}
	}
	return nil
}

// removeUnnamedStagesOf removes what dir holds but the stage its recorded
// take names, if it has one, and dir itself when nothing is left.
func removeUnnamedStagesOf(dir string, choice takeChoice, recorded bool) error {
	stages, err := os.ReadDir(dir)
	if err != nil {
		return os.RemoveAll(dir)
	}
	kept := 0
	for _, st := range stages {
		if recorded && st.IsDir() && st.Name() == choice.stage {
			kept++
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, st.Name())); err != nil {
			return err
		}
	}
	if kept == 0 {
		if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// TAKING THE ARCHIVE stages first (the contract's rule 4): nothing of the
// database is removed until the whole snapshot it will install has been
// read from the store, checked, and is on local disk, under
// <data root>/archive-take/<id>/<stage>/ as a directory store of its own,
// with staged.json written last. A staged copy has no authority of its own:
// only a take recorded in archive_takes starts or continues a replacement,
// and it installs only the copy staged for exactly that choice, the
// repository, the archive and the manifest by name and hash. Each choice is
// staged in a directory of its own, so a second one is staged beside the
// first and replaces it only once it is recorded.

// takeRootDir is the directory under the data root every staged copy is in.
const takeRootDir = "archive-take"

// takeChoice is a recorded take, a row of archive_takes: the stage its copy
// is in, which is also the archive the restored repository writes next, and
// the archive and the manifest that copy holds.
type takeChoice struct {
	stage, archive, manifest string
}

func (c takeChoice) String() string { return c.archive + "/" + c.manifest }

// readTakeChoice is repository id's recorded take, if one is.
func readTakeChoice(ctx context.Context, q dbx, id string) (takeChoice, bool, error) {
	var c takeChoice
	err := q.QueryRowContext(ctx, `SELECT stage, archive, manifest FROM archive_takes WHERE repository_id = $1`, id).
		Scan(&c.stage, &c.archive, &c.manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// readTakeChoices is every recorded take, by repository id.
func readTakeChoices(ctx context.Context, q dbx) (map[string]takeChoice, error) {
	rows, err := q.QueryContext(ctx, `SELECT repository_id, stage, archive, manifest FROM archive_takes`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]takeChoice{}
	for rows.Next() {
		var id string
		var c takeChoice
		if err := rows.Scan(&id, &c.stage, &c.archive, &c.manifest); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// stagedTake is staged.json, written last: the choice the copy was staged
// for.
type stagedTake struct {
	Archive  string `json:"archive"`
	Manifest string `json:"manifest"`
	Next     string `json:"next"`
}

// takeDir is where the copy for one choice of repository id is staged.
func (s *service) takeDir(id, stage string) string {
	return filepath.Join(s.dataRoot, takeRootDir, id, stage)
}

// stagedFor reports whether the copy staged for choice is whole and is that
// choice's: its staged.json names the archive, the manifest and the stage,
// and the manifest it holds is the bytes its name hashes.
func (s *service) stagedFor(id string, choice takeChoice) bool {
	dir := s.takeDir(id, choice.stage)
	raw, err := os.ReadFile(filepath.Join(dir, "staged.json"))
	if err != nil {
		return false
	}
	var st stagedTake
	if err := json.Unmarshal(raw, &st); err != nil || st != (stagedTake{Archive: choice.archive, Manifest: choice.manifest, Next: choice.stage}) {
		s.log.Warn("substrate: a staged copy is not the one its recorded take names; it is staged again", "repository", id, "chose", choice.String(), "dir", dir)
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(changelogfile.ArchiveManifestsPrefix(id, choice.archive)+choice.manifest)))
	return err == nil && changelogfile.ManifestNamesBytes(choice.manifest, data)
}

// stagedStore is the directory store the copy staged for choice sits in.
func (s *service) stagedStore(id string, choice takeChoice) (archive.Store, error) {
	return archive.NewDir(s.takeDir(id, choice.stage))
}

// stageTake stages the snapshot a take of repository id installs: want's
// manifest, or with want nil the newest archive's newest manifest under a
// stage of its own. It holds every object to what the manifest says of it
// as it goes: each piece to its checksum and every line to its own, each
// sealed value to its version, each spooled request read whole, and every
// blob of the archive to its digest. Then it admits what it staged
// (admitStaged), the restore's own reading of the copy. A failure removes
// what it staged and nothing else.
func (s *service) stageTake(ctx context.Context, id string, want *takeChoice) (takeChoice, error) {
	store, err := s.archiveStore()
	if err != nil {
		return takeChoice{}, err
	}
	var choice takeChoice
	if want != nil {
		choice = *want
	} else {
		listed, err := listArchives(ctx, store, id)
		if err != nil {
			return takeChoice{}, err
		}
		archiveID, name, err := restorable(listed)
		if err != nil {
			return takeChoice{}, err
		}
		next, err := changelogfile.NewArchiveID(nowUTC(), listed.ids()...)
		if err != nil {
			return takeChoice{}, err
		}
		choice = takeChoice{stage: next, archive: archiveID, manifest: name}
	}
	dir := s.takeDir(id, choice.stage)
	if err := os.RemoveAll(dir); err != nil {
		return takeChoice{}, err
	}
	if err := s.stageTakeInto(ctx, store, id, dir, choice); err != nil {
		_ = os.RemoveAll(dir)
		return takeChoice{}, fmt.Errorf("substrate/engine: the archive cannot replace repository %s, and nothing was changed: %w", id, err)
	}
	// What the restore would refuse is refused before the choice is
	// recorded, by the restore's own reading of what was staged.
	if err := s.admitStaged(ctx, id, choice); err != nil {
		_ = os.RemoveAll(dir)
		return takeChoice{}, fmt.Errorf("substrate/engine: the archive cannot replace repository %s, and nothing was changed: %w", id, err)
	}
	return choice, nil
}

func (s *service) stageTakeInto(ctx context.Context, store archive.Store, id, dir string, choice takeChoice) error {
	src, err := openArchiveManifest(ctx, store, id, choice.archive, choice.manifest, false, s.admitManifest)
	if err != nil {
		return err
	}
	local, err := archive.NewDir(dir)
	if err != nil {
		return err
	}
	put := func(key string, data []byte) error {
		return local.Put(ctx, key, bytes.NewReader(data), int64(len(data)))
	}
	for _, p := range src.chain {
		data, err := archive.ReadAll(ctx, store, p.Key)
		if err != nil {
			return fmt.Errorf("piece %s: %w", p.Key, err)
		}
		if changelogfile.SumText(sha256.Sum256(data)) != src.sums[p.Key] {
			return fmt.Errorf("%w: piece %s is not the bytes its manifest names", ErrChangelogDiverged, p.Key)
		}
		if err := changelogfile.ReadPiece(bytes.NewReader(data), p.First, p.Last, func(changelogfile.Entry, []byte, [32]byte) error { return nil }); err != nil {
			return fmt.Errorf("%w: piece %s: %w", ErrChangelogDiverged, p.Key, err)
		}
		if err := put(p.Key, data); err != nil {
			return err
		}
	}
	for _, ref := range sortedKeys(src.manifest.Sealed) {
		object := src.manifest.Sealed[ref]
		sum, _, _ := changelogfile.ParseSealedObjectName(ref, object)
		data, err := archive.ReadAll(ctx, store, src.objectKey(object))
		if err != nil {
			return fmt.Errorf("the sealed value %s: %w", ref, err)
		}
		if _, err := changelogfile.DecodeSealedVersion(ref, sum, data); err != nil {
			return err
		}
		if err := put(src.objectKey(object), data); err != nil {
			return err
		}
	}
	m := src.manifest
	for _, object := range m.Spool {
		data, err := archive.ReadAll(ctx, store, src.objectKey(object))
		if err != nil {
			return fmt.Errorf("the spooled request %s: %w", object, err)
		}
		if err := put(src.objectKey(object), data); err != nil {
			return err
		}
	}
	prefix := changelogfile.ArchivePrefix(id, choice.archive)
	blobs, err := store.List(ctx, prefix+changelogfile.BlobsSubdir+"/")
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, o := range blobs {
		digest, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix))
		if !ok || have[digest] {
			continue
		}
		if err := stageBlob(ctx, store, local, dir, o.Key, digest); err != nil {
			return err
		}
		have[digest] = true
	}
	// The blobs the manifest selects are any object of each digest the
	// records at its head hold, which the import's fold says: held there,
	// by verify on the restored repository. What the archive names missing
	// or pending stays so.
	key := changelogfile.ArchiveManifestsPrefix(id, choice.archive) + choice.manifest
	raw, err := archive.ReadAll(ctx, store, key)
	if err != nil {
		return err
	}
	if !changelogfile.ManifestNamesBytes(choice.manifest, raw) {
		return fmt.Errorf("%w: manifest %s is not the bytes its name hashes", ErrChangelogDiverged, key)
	}
	if err := put(key, raw); err != nil {
		return err
	}
	data, err := json.Marshal(stagedTake{Archive: choice.archive, Manifest: choice.manifest, Next: choice.stage})
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "staged.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if f, err := os.Open(tmp); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
	if err := os.Rename(tmp, filepath.Join(dir, "staged.json")); err != nil {
		return err
	}
	return changelogfile.SyncDir(dir)
}

// stageBlob copies one blob object into the staged store through a file,
// held to its digest.
func stageBlob(ctx context.Context, store archive.Store, local *archive.Dir, dir, key, digest string) error {
	rc, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("blob %s: %w", digest, err)
	}
	defer func() { _ = rc.Close() }()
	f, err := os.CreateTemp(dir, "blob-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), rc); err != nil {
		_ = f.Close()
		return fmt.Errorf("blob %s: %w", digest, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if substrate.BlobDigestPrefix+hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("blob %s holds other bytes than its digest names", key)
	}
	return local.Link(ctx, key, f.Name())
}
