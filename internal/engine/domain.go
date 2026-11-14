package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The repository record's two managed properties for domain verification
// (decision 0208). Only the engine writes them: a generic write that sends
// another value is refused by checkManagedProps.
const (
	propDomainProof      = "domainProof"
	propDomainVerifiedAt = "domainVerifiedAt"
)

// domainProofLen is the length of a minted proof: 50 random bits, short
// enough to type into a DNS host's form, and spent at the first successful
// check.
const domainProofLen = 10

// DomainState reads the pending proof and the verified stamp off the
// repository record.
func (ds *dataset) DomainState(ctx context.Context) (substrate.DomainState, error) {
	rec, err := ds.Get(ctx, kindRepository, ds.Repository().ID)
	if err != nil {
		return substrate.DomainState{}, err
	}
	return domainStateOf(rec.Properties)
}

// StartDomainVerification answers the pending proof, minting and storing one
// when none is pending.
func (ds *dataset) StartDomainVerification(ctx context.Context) (substrate.DomainState, error) {
	var out substrate.DomainState
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		row, err := ds.repositoryRow(t)
		if err != nil {
			return err
		}
		if out, err = domainStateOf(row.Props); err != nil || out.Proof != "" {
			return err
		}
		proof, err := newDomainProof()
		if err != nil {
			return err
		}
		out.Proof = proof
		_, err = t.put(substrate.PutInput{
			Kind: kindRepository, ID: row.ID,
			Properties: map[string]any{propDomainProof: proof},
		})
		return err
	})
	return out, err
}

// MarkDomainVerified stamps the repository record verified now and spends
// the pending proof, so the next Verify domain mints a fresh one. The proof
// is compared under the row's lock: two checks of one proof stamp once, and a
// check that read a proof spent since spends nothing.
func (ds *dataset) MarkDomainVerified(ctx context.Context, proof string) (time.Time, error) {
	var at time.Time
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		row, err := ds.repositoryRow(t)
		if err != nil {
			return err
		}
		if proof != "" {
			state, err := domainStateOf(row.Props)
			if err != nil {
				return err
			}
			if state.Proof != proof {
				return fmt.Errorf("%w: the proof this check read is no longer the pending one: run the check again", substrate.ErrConflict)
			}
		}
		at = t.now
		_, err = t.put(substrate.PutInput{
			Kind: kindRepository, ID: row.ID,
			Properties: map[string]any{
				propDomainVerifiedAt: at.Format(time.RFC3339Nano),
				propDomainProof:      nil,
			},
		})
		return err
	})
	return at, err
}

// reconcileRepositoryName holds the two records that record the
// repository's name to the name its control-plane row has. A repository
// imported under another name than its archive gave (ImportArchive) carries
// them as the old one wrote them: the repository record's `name` and
// `authority` move, and its domain proof and verification, which were for
// the old name, are cleared; and the `name` of its first person's actor, which
// reads as the repository's name (core/actor), moves. A sign-in is the
// identity's, held beside the repositories, and records no repository's name.
// A record already holding the name is left alone, and a read-only process
// writes nothing: an open whose records hold the name opens no transaction,
// as every open step that finds nothing to do.
func (ds *dataset) reconcileRepositoryName(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	name := ds.info.Authority
	records := []struct {
		ref   eref
		key   string
		props map[string]any
	}{
		{eref{Kind: kindRepository, ID: ds.info.ID}, "authority", map[string]any{
			"name": name, "authority": name, propDomainProof: nil, propDomainVerifiedAt: nil,
		}},
		{eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}, "name", map[string]any{"name": name}},
	}
	behind := false
	for _, r := range records {
		var held sql.NullString
		err := ds.db.QueryRowContext(ctx, `SELECT props->>$3 FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
			r.ref.Kind, r.ref.ID, r.key).Scan(&held)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		behind = behind || held.String != name
	}
	if !behind {
		return nil
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		for _, r := range records {
			row, err := t.loadRow(r.ref, true)
			if err != nil {
				return err
			}
			if row == nil || row.DeletedAt != nil || row.Props[r.key] == name {
				continue
			}
			if _, err := t.put(substrate.PutInput{Kind: r.ref.Kind, ID: r.ref.ID, Properties: r.props}); err != nil {
				return err
			}
		}
		return nil
	})
}

// repositoryRow locks the repository record for the domain writes, so two
// starts at once mint one proof between them.
func (ds *dataset) repositoryRow(t *txn) (*erow, error) {
	row, err := t.loadRow(eref{Kind: kindRepository, ID: ds.Repository().ID}, true)
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil {
		return nil, fmt.Errorf("%w: the repository record %s is missing", substrate.ErrNotFound, ds.Repository().ID)
	}
	return row, nil
}

func domainStateOf(props map[string]any) (substrate.DomainState, error) {
	var out substrate.DomainState
	if p, ok := props[propDomainProof].(string); ok {
		out.Proof = p
	}
	switch v := props[propDomainVerifiedAt].(type) {
	case nil:
	case string:
		at, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return out, fmt.Errorf("the repository record's %s is %q: %w", propDomainVerifiedAt, v, err)
		}
		at = at.UTC()
		out.VerifiedAt = &at
	case time.Time:
		at := v.UTC()
		out.VerifiedAt = &at
	}
	return out, nil
}

// newDomainProof mints a lowercase base32 code: URL-safe, and a TXT value
// needs no quoting for it.
func newDomainProof() (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	return strings.ToLower(id[:domainProofLen]), nil
}
