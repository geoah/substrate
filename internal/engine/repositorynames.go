package engine

// A REPOSITORY'S NAMES (decision record 0287).
//
// The name is an alias: what people type and see, held in
// `repositories.authority`, and it can change. Nothing stored binds to it:
// storage and keys bind to the id, own kinds are `~`. A rename keeps the old
// name in `repository_former_names` for formerNameHold: for that long it
// still resolves to the repository on every door and no registration or
// rename elsewhere takes it, and then it lapses at once. Deleting the
// repository frees its name and its former names together.
//
// Every door that turns a name into a repository goes through
// repositoryByNameOn, which reads both tables, so a door added later cannot
// forget the former names (TestEveryNameLookupGoesThroughTheOneFunction).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// formerNameHold is how long a repository's former name keeps resolving to
// it, and stays out of anyone else's reach, from the rename that retired it.
const formerNameHold = 90 * 24 * time.Hour

// formerName is a name a repository had before a rename, and when it stopped
// being its name.
type formerName struct {
	name      string
	retiredAt time.Time
}

// until is the moment the name lapses.
func (f formerName) until() time.Time { return f.retiredAt.Add(formerNameHold) }

// namesNow is the clock a former name lapses on: the database's clock as the
// service read it at its open, run forward on this process's monotonic
// clock, so a step of this machine's wall clock moves no name in or out of
// its hold; or the test seam's (WithTestNamesClock). Each process starts
// from the database's now and advances on its own monotonic clock, so two
// processes started on either side of a step of the database's clock
// disagree by that step about when an untaken name lapses, and about nothing
// else: a taken name's row is gone (takeFormerName), so no clock brings it
// back.
func (s *service) namesNow() time.Time {
	if s.testNamesNow != nil {
		return s.testNamesNow().UTC()
	}
	if s.namesStart.IsZero() {
		return time.Now().UTC()
	}
	return s.namesBase.Add(time.Since(s.namesStart)).UTC()
}

// repositoryByName finds the repository a name means: the one it is the name
// of, or the one it was the name of until a rename less than formerNameHold
// ago.
func (s *service) repositoryByName(ctx context.Context, name string) (Repository, error) {
	return s.repositoryByNameOn(ctx, s.maint, name)
}

// repositoryByNameOn is the lookup of a repository by name on a given maint
// handle, under this service's clock (lookupRepositoryByName).
func (s *service) repositoryByNameOn(ctx context.Context, q dbx, name string) (Repository, error) {
	return lookupRepositoryByName(ctx, q, name, s.namesNow())
}

// lookupRepositoryByName is THE lookup of a repository by name: every door
// that turns a name into a repository comes here, the operator's commands
// included. A current name wins over a former one, and two repositories never
// hold one name at once (renameOn and the registration hold to that under the
// name's registration lock). The current and the former name are read in ONE
// statement, so one snapshot answers both: two reads would let a rename back
// that commits between them, moving the name from former to current, answer
// a name valid throughout as nobody's. now is the clock a former name lapses
// on, and zero is the database's own.
func lookupRepositoryByName(ctx context.Context, q dbx, name string, now time.Time) (Repository, error) {
	clock := sql.NullTime{Time: now, Valid: !now.IsZero()}
	return scanNamed(q.QueryRowContext(ctx, `
		SELECT `+repositoryColumnsOf("x")+` FROM (
			SELECT 0 AS rank, `+repositoryColumnsOf("r")+` FROM repositories r WHERE r.authority = $1
			UNION ALL
			SELECT 1, `+repositoryColumnsOf("r")+`
			FROM repository_former_names f JOIN repositories r ON r.id = f.repository_id
			WHERE f.name = $1 AND f.retired_at > COALESCE($2::timestamptz, now()) - make_interval(secs => $3)
		) x ORDER BY x.rank LIMIT 1`, name, clock, formerNameHold.Seconds()), name)
}

// RepositoryIDByName is the id of the repository a name means, current or
// former (lookupRepositoryByName), on db, the DSN's own pool: the operator's
// door to the one lookup, on the database's clock as the server's is.
func RepositoryIDByName(ctx context.Context, db *sql.DB, name string) (string, error) {
	repo, err := lookupRepositoryByName(ctx, db, name, time.Time{})
	if err != nil {
		return "", err
	}
	return repo.ID, nil
}

// scanNamed reads one repository row, answering ErrNotFound for none.
func scanNamed(row *sql.Row, name string) (Repository, error) {
	r, err := scanRepositoryRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, fmt.Errorf("%w: repository %q", substrate.ErrNotFound, name)
	}
	return r, err
}

// repositoryColumnsOf is repositoryColumns qualified by a table alias.
func repositoryColumnsOf(alias string) string {
	return alias + `.id, ` + alias + `.authority, ` + alias + `.created_at, ` + alias + `.dek, ` +
		alias + `.history_generation, ` + alias + `.dek_key_id`
}

// formerNamesOn are the former names of repository id still in force, newest
// first.
func (s *service) formerNamesOn(ctx context.Context, q dbx, id string) ([]formerName, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT name, retired_at FROM repository_former_names
		WHERE repository_id = $1 AND retired_at > $2
		ORDER BY retired_at DESC, name`, id, s.namesNow().Add(-formerNameHold))
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the former names of %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []formerName
	for rows.Next() {
		var f formerName
		if err := rows.Scan(&f.name, &f.retiredAt); err != nil {
			return nil, err
		}
		f.retiredAt = f.retiredAt.UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// namesAnswer is what a rename answers: the id, the name and the former names
// in force with the moment each lapses.
func namesAnswer(repo Repository, former []formerName) substrate.RepositoryNames {
	out := substrate.RepositoryNames{ID: repo.ID, Name: repo.Authority, FormerNames: []substrate.FormerName{}}
	for _, f := range former {
		out.FormerNames = append(out.FormerNames, substrate.FormerName{Name: f.name, Until: f.until()})
	}
	return out
}

// RenameRepository gives the repository a person who proves both factors and
// holds `admin` in it picks the name newName. The name it had becomes a former name, held for
// formerNameHold from now. newName may be one of its own former names, which
// is renaming back: that name stops being a former one. A name another
// repository holds, as its name or a former name in force, is refused.
func (s *service) RenameRepository(ctx context.Context, in substrate.LoginInput, newName string) (substrate.RepositoryNames, error) {
	var zero substrate.RepositoryNames
	if s.readOnly {
		return zero, ErrDirectoryReadOnly
	}
	if err := validRepositoryAuthority(newName); err != nil {
		return zero, err
	}
	if in.Repository == "" {
		return zero, fmt.Errorf("%w: name the repository to rename in `repository`", substrate.ErrValidation)
	}
	m, err := s.verifyFactors(ctx, in)
	if err != nil {
		return zero, err
	}
	s.afterProof("rename")
	repo, err := s.pickRepository(ctx, m.identity, in.Repository)
	if err != nil {
		return zero, err
	}
	// Past the factors, and before anything behind this door writes: a
	// repository the start set aside is refused by name.
	if err := s.refuseSetAside(repo.ID, repo.Authority); err != nil {
		return zero, err
	}
	if err := s.requireAdmin(ctx, repo, m.identity, "renaming a repository is an admin's: it changes every person's address of it"); err != nil {
		return zero, err
	}
	return s.rename(ctx, repo, newName, renameBy{person: m.identity, proved: m.version})
}

// rename moves repository repo, whose factors the caller proved, to newName.
//
// The control plane is the name's truth, and it moves in one transaction
// that takes newName's registration lock for itself, the lock a
// registration of that name holds, so a rename and a registration of one
// name cannot both win; the COMMIT releases it. Whether the name moved is the
// locked row's answer, never repo's, which is the row as the caller's sign-in
// read it: two renames that both read one name would otherwise leave the
// second's change unseen.
//
// The names cache's rule holds around the COMMIT (namescache.go): the
// repository renamed, and one whose lapsed former name it took, are fenced
// before it and unfenced once its outcome is proved, which is all that
// follows the commit. Nothing is closed, opened again or waited for: a
// request on the repository asks the cache for its names at each fold.
func (s *service) rename(ctx context.Context, repo Repository, newName string, by renameBy) (substrate.RepositoryNames, error) {
	var zero substrate.RepositoryNames
	done, err := s.renameOn(ctx, repo.ID, newName, by)
	if err != nil {
		return zero, err
	}
	if done.moved != "" {
		s.log.Info("substrate: repository renamed by its owner", "repository", repo.ID, "from", done.moved, "to", done.repo.Authority)
	}
	return namesAnswer(done.repo, done.former), nil
}

// renameDone is what a rename's transaction leaves: the repository as it
// stands after, its former names in force, and the name it moved from (""
// where the row already had the new one).
type renameDone struct {
	repo   Repository
	former []formerName
	moved  string
}

// renameBy is the person whose factors a rename proved, at the version it
// proved: the rename's transaction holds the identity's row to it before a
// name changes (factorsStandOn), as a delete's does. A zero renameBy is a
// rename no person proved.
type renameBy struct {
	person string
	proved time.Time
}

// renameOn moves repository id to newName in one control-plane transaction,
// which holds newName's registration lock, the person's row and the names
// lock of every repository whose names it changes until it ends.
func (s *service) renameOn(ctx context.Context, id, newName string, by renameBy) (renameDone, error) {
	now := s.namesNow()
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return renameDone{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+registrationLockKeySQL+`)`, newName); err != nil {
		return renameDone{}, fmt.Errorf("substrate/engine: registration lock for %s: %w", newName, err)
	}
	// The locks in the delete door's order: the name's registration lock,
	// then the person's row, then the repository's. A change of the factors
	// committed while the rename waited refuses it with no name moved.
	if by.person != "" {
		if err := factorsStandOn(ctx, tx, by.person, by.proved); err != nil {
			return renameDone{}, err
		}
	}
	if err := lockNames(ctx, tx, id); err != nil {
		return renameDone{}, err
	}
	cur, err := s.scanRepository(tx.QueryRowContext(ctx,
		`SELECT `+repositoryColumns+` FROM repositories WHERE id = $1 FOR UPDATE`, id), id)
	if err != nil {
		return renameDone{}, err
	}
	var done renameDone
	fenced := []string{}
	if cur.Authority != newName {
		done.moved = cur.Authority
		if holder, err := s.repositoryByNameOn(ctx, tx, newName); err == nil && holder.ID != id {
			return renameDone{}, errAuthorityTaken(newName)
		} else if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return renameDone{}, err
		}
		if !s.testNameDoorsOpen {
			if err := refuseHeldUnderName(ctx, tx, id, newName); err != nil {
				return renameDone{}, err
			}
		}
		// The new name is free, lapsed, or this repository's own former
		// name: whatever row holds it as a former name goes, and another
		// repository that held it lapsed is told after the commit.
		took, err := takeFormerName(ctx, tx, newName)
		if err != nil {
			return renameDone{}, fmt.Errorf("substrate/engine: rename %s: %w", id, err)
		}
		fenced = append(fenced, id)
		if took != "" && took != id {
			if err := lockNames(ctx, tx, took); err != nil {
				return renameDone{}, err
			}
			fenced = append(fenced, took)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO repository_former_names (name, repository_id, retired_at) VALUES ($1, $2, $3)
			ON CONFLICT (name) DO UPDATE SET repository_id = EXCLUDED.repository_id, retired_at = EXCLUDED.retired_at`,
			cur.Authority, id, now); err != nil {
			return renameDone{}, fmt.Errorf("substrate/engine: rename %s: keep %s as a former name: %w", id, cur.Authority, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE repositories SET authority = $2 WHERE id = $1`, id, newName); err != nil {
			return renameDone{}, fmt.Errorf("substrate/engine: rename %s: %w", id, err)
		}
		cur.Authority = newName
	}
	former, err := s.formerNamesOn(ctx, tx, id)
	if err != nil {
		return renameDone{}, err
	}
	if err := s.commitFencingNames(tx, false, fenced...); err != nil {
		return renameDone{}, err
	}
	done.repo, done.former = cur, former
	return done, nil
}

// A NAME UNDER WHICH A REPOSITORY HOLDS A PACKAGE THAT IS NOT ITS OWN DOES
// NOT SPELL `~` THERE (vocabulary.Registry.Spellings): every fold reads the
// spellings (TestEveryFoldReadsTheSpellings), so a publisher's kinds,
// references and grants keep their meaning whatever the repository is
// called. Three doors refuse such a name early, with a plain message, and
// are no guarantee: the rename's destination where the repository holds a
// package under it (refuseHeldUnderName), the import under another name
// against the packages its archive holds (heldUnderNameIn), and an install
// from a former name in force (refuseInstallFromName). Should a rename and an
// install pass each other, the name does not spell `~` while the package is
// held, and the repository's own kinds are written `~`. An authority row
// alone changes no meaning, and a rename leaves it as it is.

// refuseHeldUnderName refuses name to repository id where id holds a live
// package under name.
func refuseHeldUnderName(ctx context.Context, tx dbx, id, name string) error {
	var held string
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM records
		WHERE repository = $1 AND deleted_at IS NULL AND kind = $2 AND starts_with(id, $3 || '/')
		ORDER BY id LIMIT 1`, id, kindPackage, name).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("substrate/engine: rename %s: read what it holds under %s: %w", id, name, err)
	}
	return errHeldUnderName(name, held)
}

// errHeldUnderName is the refusal of a name the repository holds the
// package pkg under.
func errHeldUnderName(name, pkg string) error {
	return fmt.Errorf("%w: the repository holds the package %s, which %s publishes, and its own kinds answer to its name: choose another name, or uninstall %s first",
		substrate.ErrConflict, pkg, name, pkg)
}

// takeFormerName deletes the row that holds name as a former name, if one
// does, inside the caller's transaction, and answers the repository that
// held it ("" for none). A registration or a rename that takes a name calls
// it: the rows are the truth of who answers to a name, and a name taken is
// nobody's former name any more.
func takeFormerName(ctx context.Context, tx dbx, name string) (string, error) {
	var held string
	err := tx.QueryRowContext(ctx, `DELETE FROM repository_former_names WHERE name = $1 RETURNING repository_id`, name).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return held, err
}

// answersTo are the names this dataset's repository answers to now, asked of
// the names cache by its id at the moment (namescache.go): its name, then the
// former names still in force, newest first. The dataset keeps none of them.
// A read that fails answers none: a full-name spelling then folds to
// nothing, and the request meets it as a name it does not know. A dataset
// that is creating its repository answers the name being registered, which
// no row holds yet and no rename can move. A fold reads names, never these
// (TestEveryFoldReadsTheSpellings).
func (ds *dataset) answersTo() []string {
	if ds.creating != "" {
		return []string{ds.creating}
	}
	ctx, cancel := context.WithTimeout(context.Background(), namesReadTimeout)
	defer cancel()
	names, err := ds.svc.namesOf(ctx, ds.info.ID)
	if err != nil {
		ds.svc.log.Warn("substrate: could not read the repository's names", "repository", ds.info.ID, "error", err)
		return nil
	}
	return names
}

// name is the repository's name now (answersTo), or "" for none.
func (ds *dataset) name() string {
	if names := ds.answersTo(); len(names) > 0 {
		return names[0]
	}
	return ""
}

// names are the names that spell `~` on an input here: every name the
// repository answers to but one a package it holds is published under
// (vocabulary.Registry.Spellings), read against the live declarations.
func (ds *dataset) names() []string { return ds.registry().Spellings(ds.answersTo()...) }

// names is ds.names read against the declarations this transaction writes
// under, so an install of a package under one of the repository's names
// stops that name folding inside its own transaction.
func (t *txn) names() []string { return t.declarations().Spellings(t.ds.answersTo()...) }

// namesReadTimeout bounds a read of a repository's names from its row.
const namesReadTimeout = 10 * time.Second

// reconcileRepositoryName holds the two records that record the
// repository's name to the name its control-plane row has: after a rename, or
// an import under another name than its archive gave (ImportArchive), the
// repository record's `name` and `authority` move, and its domain proof and
// verification, which were for the old name, are cleared; and so does the
// `name` of its first person's actor record, which reads as the repository's
// name (core/actor). A sign-in is the identity's, held beside the
// repositories, and records no repository's name. The row is the truth and
// the records follow it: the name is read from the row, never from a copy,
// and read again AFTER the transaction holds the repository's changelog lock,
// which every write of the records takes, so two reconciles apply what they
// read in the order they read it and an older one cannot write over a newer
// one. A record already holding the name is left alone, and an open whose
// records all hold it opens no transaction, as every open step that finds
// nothing to do. A read-only process writes nothing; the writer reconciles
// at its own open and after a rename (reconcileNames).
func (ds *dataset) reconcileRepositoryName(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	repo, err := ds.svc.repositoryByID(ctx, ds.info.ID)
	if err != nil {
		return err
	}
	if behind, err := ds.nameRecordsBehind(ctx, repo.Authority); err != nil || !behind {
		return err
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.lockChangelog(); err != nil {
			return err
		}
		repo, err := ds.svc.repositoryByID(ctx, ds.info.ID)
		if err != nil {
			return err
		}
		if ds.svc.testReconcileRead != nil {
			ds.svc.testReconcileRead(ds.info.ID, repo.Authority)
		}
		for _, r := range ds.nameRecords(repo.Authority) {
			row, err := t.loadRow(r.ref, true)
			if err != nil {
				return err
			}
			if row == nil || row.DeletedAt != nil || row.Props[r.key] == repo.Authority {
				continue
			}
			if _, err := t.put(substrate.PutInput{Kind: r.ref.Kind, ID: r.ref.ID, Properties: r.props}); err != nil {
				return err
			}
		}
		return nil
	})
}

// nameRecord is one record that records the repository's name: key is the
// property that holds it, props what a reconcile writes.
type nameRecord struct {
	ref   eref
	key   string
	props map[string]any
}

// nameRecords are the records reconcileRepositoryName holds to name.
func (ds *dataset) nameRecords(name string) []nameRecord {
	return []nameRecord{
		{eref{Kind: kindRepository, ID: ds.info.ID}, "authority", map[string]any{
			"name": name, "authority": name, propDomainProof: nil, propDomainVerifiedAt: nil,
		}},
		{eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}, "name", map[string]any{"name": name}},
	}
}

// nameRecordsBehind reports whether a live name record holds another name
// than name.
func (ds *dataset) nameRecordsBehind(ctx context.Context, name string) (bool, error) {
	for _, r := range ds.nameRecords(name) {
		var held sql.NullString
		err := ds.db.QueryRowContext(ctx, `SELECT props->>$3 FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
			r.ref.Kind, r.ref.ID, r.key).Scan(&held)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if held.String != name {
			return true, nil
		}
	}
	return false, nil
}
