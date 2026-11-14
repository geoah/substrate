package engine

import (
	"context"
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
	rec, err := ds.Get(ctx, kindRepository, ds.info.ID)
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

// repositoryRow locks the repository record for the domain writes, so two
// starts at once mint one proof between them.
func (ds *dataset) repositoryRow(t *txn) (*erow, error) {
	row, err := t.loadRow(eref{Kind: kindRepository, ID: ds.info.ID}, true)
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil {
		return nil, fmt.Errorf("%w: the repository record %s is missing", substrate.ErrNotFound, ds.info.ID)
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
