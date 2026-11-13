package engine

// A REPOSITORY'S PEOPLE TRAVEL WITH ITS ARCHIVE (decision 0274). A person's
// sign-in is no part of a repository's history: it is the server's
// `identities` row. So every archive pass copies into the manifest the row
// of each person the people index names for the repository, as stored: the
// data key wrapped under the host key and the factors sealed under it,
// nothing opened. An export carries the person who took it and nobody else,
// because an export is the owner's and the other people's sign-ins are not.
//
// A restore inserts, in the transaction that creates the repository's row,
// the people the database lacks. It writes no row of the index: that is
// derived from the person actors before the repository is offered to
// anyone, as for every restore (people.go derivePeople).
//
// WHICH COPY WINS. A row the database held before the restore began is the
// live one, and a restore never touches it. A person in several
// repositories arrives in several manifests, taken by passes at different
// times: among the copies this process's restores bring, the one whose
// factors changed last wins, so the order of the restores does not decide
// which password the person signs in with.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
)

// manifestIdentities reads the identity row of every person the people
// index names for repository, sorted by id, on the control plane: the
// repository's own role never sees the table. only, when set, narrows it to
// that one person. None is nil, so a manifest leaves the key out.
func (s *service) manifestIdentities(ctx context.Context, repository, only string) ([]changelogfile.ManifestIdentity, error) {
	rows, err := s.maint.QueryContext(ctx, `SELECT `+identityColumns+` FROM identities
		WHERE id IN (SELECT identity FROM identity_repositories WHERE repository_id = $1)
		  AND ($2 = '' OR id = $2)
		ORDER BY id`, repository, only)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the people of repository %s for its archive: %w", repository, err)
	}
	defer func() { _ = rows.Close() }()
	var out []changelogfile.ManifestIdentity
	for rows.Next() {
		r, err := scanIdentity(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, changelogfile.ManifestIdentity{
			ID: r.ID, Name: r.Name, DEK: r.DEK, DEKKeyID: r.DEKKeyID, Hash: r.Hash, TOTP: r.TOTP,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, rows.Err()
}

// identitiesPrint fingerprints a manifest's people, for the pass's "did
// anything change" check: a password changed or a person added moves no
// line of the history.
func identitiesPrint(ids []changelogfile.ManifestIdentity) (string, error) {
	data, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// identityRowOf is a manifest's copy of a person as the row it restores to.
func identityRowOf(p changelogfile.ManifestIdentity) identityRow {
	return identityRow{
		ID: p.ID, Name: p.Name, DEK: p.DEK, DEKKeyID: p.DEKKeyID, Hash: p.Hash, TOTP: p.TOTP,
		CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}

// admitIdentities holds the people a manifest carries to this host's key, as
// admitRestore holds the repository's sealed values: each one's factors must
// open. A person whose factors do not refuses the restore, unless the
// operator listed the repository, when the restore leaves them out and
// `user reset` gives them back. It writes nothing.
func (s *service) admitIdentities(m changelogfile.Manifest, adm *restoreAdmission) error {
	for _, p := range m.Identities {
		_, err := s.identityMaterial(identityRowOf(p))
		if err == nil {
			continue
		}
		if !s.acceptsLostSecrets(m.ID, m.Authority) {
			return fmt.Errorf("the sign-in of %s, a person in repository %s, does not open under this host's key: %w. Set the key it was written under and restart; or, to restore the repository without the people whose sign-in does not open (each given back with `substratectl user reset`), list %s or %s in %s and restart",
				p.Name, m.Authority, err, m.Authority, m.ID, ImportWithoutSecretsVar)
		}
		if adm.leftOut == nil {
			adm.leftOut = map[string]bool{}
		}
		adm.leftOut[p.ID] = true
	}
	return nil
}

// restoreIdentities writes, in a restore's transaction, the people m carries
// that the database lacks, and a later copy over one this process's restores
// wrote. It answers what to remember once the transaction commits. A person
// whose sign-in name another person here holds is left out, and so is one
// leftOut names; the log says so, and `user reset` gives either back.
func (s *service) restoreIdentities(ctx context.Context, tx *sql.Tx, m changelogfile.Manifest, leftOut map[string]bool) (func(), error) {
	copies := map[string]time.Time{}
	for _, p := range m.Identities {
		row := identityRowOf(p)
		if leftOut[row.ID] {
			s.log.Error("substrate: a restored repository's person does not sign in: their sign-in does not open under this host's key, and the repository is listed to restore without its secrets; `substratectl user reset` gives them back",
				"repository", m.Authority, "identity", row.Name)
			continue
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, updated_at FROM identities WHERE id = $1 OR name = $2 FOR UPDATE`, row.ID, row.Name)
		if err != nil {
			return nil, fmt.Errorf("read the restored person %s: %w", row.Name, err)
		}
		var held *time.Time
		taken := false
		for rows.Next() {
			var id string
			var at time.Time
			if err := rows.Scan(&id, &at); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if id == row.ID {
				at = at.UTC()
				held = &at
			} else {
				taken = true
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, err
		}
		switch {
		case held != nil:
			if !s.restoredCopy(row.ID, *held) || !row.UpdatedAt.After(*held) {
				continue
			}
			// A later copy of a person this process restored: the factors and
			// the key they are sealed under. The name is the first copy's.
			if _, err := tx.ExecContext(ctx, `
				UPDATE identities SET dek = $2, dek_key_id = $3, password = $4, totp = $5, updated_at = $6
				WHERE id = $1 AND updated_at = $7`,
				row.ID, row.DEK, nullString(row.DEKKeyID), row.Hash, row.TOTP, row.UpdatedAt, *held); err != nil {
				return nil, fmt.Errorf("restore a later copy of person %s: %w", row.Name, err)
			}
		case taken:
			s.log.Error("substrate: a restored repository's person does not sign in: another person here holds their sign-in name; `substratectl user reset <repository id> --identity <another name>` gives them back under it",
				"repository", m.Authority, "identity", row.Name)
			continue
		default:
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO identities (id, name, dek, dek_key_id, password, totp, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				row.ID, row.Name, row.DEK, nullString(row.DEKKeyID), row.Hash, row.TOTP, row.CreatedAt, row.UpdatedAt); err != nil {
				return nil, fmt.Errorf("restore person %s: %w", row.Name, err)
			}
		}
		copies[row.ID] = row.UpdatedAt
	}
	return func() {
		s.restoredMu.Lock()
		defer s.restoredMu.Unlock()
		if s.restoredCopies == nil {
			s.restoredCopies = map[string]time.Time{}
		}
		for id, at := range copies {
			s.restoredCopies[id] = at
		}
	}, nil
}

// restoredCopy reports whether a person's row is still the copy this
// process's restores wrote: the version it wrote, unchanged since.
func (s *service) restoredCopy(id string, at time.Time) bool {
	s.restoredMu.Lock()
	defer s.restoredMu.Unlock()
	wrote, ok := s.restoredCopies[id]
	return ok && wrote.Equal(at)
}
