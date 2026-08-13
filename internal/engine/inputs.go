package engine

// Bundle input resolution: a bundle declares named inputs (vocabulary
// BundleInput), each naming a kind; records of that kind are ordinary and
// unbounded, and resolution picks ONE per input, in a fixed order:
//
//   1. the BOUND record — an edge on the bundle's own record row,
//      rel = the input name, written by the bind verb;
//   2. the record whose id is "default" — a well-known NAME, never a
//      marker property, so two records can never both be "the default";
//   3. the sole live record of the kind, so the one-record case needs
//      no ceremony;
//   4. nothing — a first-class state the status reports per input.
//
// Ambiguity (several records, none bound or named default) is surfaced,
// never tie-broken: a timestamp coin-flip is how other systems earned
// their contradictory defaulting rules.
//
// A binding's lifetime tracks its record's. While the bound record is only
// TOMBSTONED (resurrectable by split), the edge stands and the input reads
// DANGLING — the choice is preserved because the record may come back. When
// GC hard-deletes the record, its edges go with it (applyPurge deletes by
// dst), the choice has nothing left to honor, and resolution returns to the
// default rules like any never-bound input. That is deliberate, not
// papering-over: a permanently gone record leaves no choice to keep.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// inputDefaultID is resolution step 2: the well-known record id.
const inputDefaultID = "default"

// resolvedInput is one input's resolution: the row when one resolved, how it
// resolved (substrate.InputVia*), and the problem when none did.
type resolvedInput struct {
	Name  string
	Input vocabulary.BundleInput
	Row   *erow  // nil when unresolved
	Via   string // InputViaBound | InputViaDefault | InputViaSole; "" unresolved
	// Problem is the machine-readable reason Row is nil (or bound wrong):
	// substrate.SetupMissing, SetupAmbiguous or SetupDangling.
	Problem string
	Detail  string
}

// resolveBundleInputs resolves every declared input of a bundle, in name
// order (InputOrder).
func (ds *dataset) resolveBundleInputs(ctx context.Context, b *vocabulary.Bundle) ([]resolvedInput, error) {
	out := make([]resolvedInput, 0, len(b.InputOrder))
	for _, name := range b.InputOrder {
		ri, err := ds.resolveBundleInput(ctx, b, name)
		if err != nil {
			return nil, err
		}
		out = append(out, ri)
	}
	return out, nil
}

// resolveBundleInput resolves one input by the fixed order above.
func (ds *dataset) resolveBundleInput(ctx context.Context, b *vocabulary.Bundle, name string) (resolvedInput, error) {
	in, ok := b.Inputs[name]
	if !ok {
		return resolvedInput{}, fmt.Errorf("%w: bundle %s declares no input %q", substrate.ErrNotFound, b.Authority, name)
	}
	ri := resolvedInput{Name: name, Input: in}

	// 1. The bound record: the bind verb wrote an edge off the bundle row.
	var dst eref
	err := ds.db.QueryRowContext(ctx, `
		SELECT dst_kind, dst FROM edges
		WHERE src_kind = $1 AND src = $2 AND rel = $3
		ORDER BY created_at, dst_kind, dst LIMIT 1`,
		kindBundle, b.Identity(), name).Scan(&dst.Kind, &dst.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return ri, err
	default:
		row, err := ds.loadRowDB(ctx, dst)
		if err != nil {
			return ri, err
		}
		if row == nil || row.DeletedAt != nil || dst.Kind != in.Kind {
			// The binding outlived its record (or the kind drifted under an
			// upgrade). A dangling explicit choice is a problem to show,
			// never silently papered over by the default rules below.
			ri.Problem = substrate.SetupDangling
			ri.Detail = fmt.Sprintf("bound to %s/%s, which no longer resolves — rebind or unbind the input", dst.Kind, dst.ID)
			return ri, nil
		}
		ri.Row, ri.Via = row, substrate.InputViaBound
		return ri, nil
	}

	// 2. The well-known id.
	row, err := ds.loadRowDB(ctx, eref{Kind: in.Kind, ID: inputDefaultID})
	if err != nil {
		return ri, err
	}
	if row != nil && row.DeletedAt == nil {
		ri.Row, ri.Via = row, substrate.InputViaDefault
		return ri, nil
	}

	// 3. The sole live record.
	rows, err := ds.liveRowsOf(ctx, in.Kind)
	if err != nil {
		return ri, err
	}
	switch len(rows) {
	case 0:
		ri.Problem = substrate.SetupMissing
		ri.Detail = fmt.Sprintf("no %s record exists yet", in.Kind)
	case 1:
		ri.Row, ri.Via = rows[0], substrate.InputViaSole
	default:
		ri.Problem = substrate.SetupAmbiguous
		ri.Detail = fmt.Sprintf("%d %s records exist and none is bound or named %q — bind one to choose",
			len(rows), in.Kind, inputDefaultID)
	}
	return ri, nil
}

// BindBundleInput points a bundle's input at a chosen record (the edge the
// resolver's first step reads), or clears the choice when recordID is empty.
// The bundle row is a system kind the generic link path refuses (and its kind
// declares no edges), so this verb is the one door: it validates the input
// exists and the record is live and of the input's kind, then writes the edge
// through the fold as the system actor — a changelog entry like any write, so
// a rebuild replays it. A disabled bundle's configuration is frozen, and
// bind IS configuration, so it refuses like any input-record write would.
func (ds *dataset) BindBundleInput(ctx context.Context, bundleID, input, recordID string) error {
	b, err := ds.bundleByID(bundleID)
	if err != nil {
		return err
	}
	in, ok := b.Inputs[input]
	if !ok {
		return fmt.Errorf("%w: bundle %s declares no input %q", substrate.ErrNotFound, bundleID, input)
	}
	state, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return err
	}
	if state.Disabled {
		return fmt.Errorf("%w: bundle %s is disabled — its configuration and accounts are frozen",
			substrate.ErrGuard, b.Authority)
	}
	src := eref{Kind: kindBundle, ID: b.Identity()}
	if recordID == "" {
		return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			// Lock the bundle row so an unbind cannot interleave a
			// concurrent uninstall's teardown or another bind.
			srcRow, err := t.loadRow(src, true)
			if err != nil {
				return err
			}
			if srcRow == nil || srcRow.DeletedAt != nil {
				return fmt.Errorf("%w: bundle %s", substrate.ErrNotFound, bundleID)
			}
			cur, err := t.edgeTargetOf(src, input)
			if err != nil || cur.ID == "" {
				return err
			}
			changed, err := t.deleteEdge(input, src, cur)
			if err != nil || !changed {
				return err
			}
			if err := t.bumpVersion(src); err != nil {
				return err
			}
			return t.appendChange(substrate.ActorSystem, substrate.OpUnlink, src.ID, src.Kind, map[string]any{
				"rel": input, "dst": cur.ID, "dstType": cur.Kind,
			})
		})
	}
	dst := eref{Kind: in.Kind, ID: recordID}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// Both ends are validated UNDER the transaction's locks (the same
		// ascending order link takes), so a bind can neither race the
		// target's delete into a dangling edge nor race an uninstall into an
		// edge off a gone bundle row.
		lsrc, ldst, err := t.lockCanonicalPair(src, dst)
		if err != nil {
			return err
		}
		srcRow, err := t.loadRow(lsrc, true)
		if err != nil {
			return err
		}
		if srcRow == nil || srcRow.DeletedAt != nil {
			return fmt.Errorf("%w: bundle %s", substrate.ErrNotFound, bundleID)
		}
		dstRow, err := t.loadRow(ldst, true)
		if err != nil {
			return err
		}
		if dstRow == nil || dstRow.DeletedAt != nil {
			return fmt.Errorf("%w: no live %s record %q to bind input %q to", substrate.ErrNotFound, in.Kind, recordID, input)
		}
		// Bind is single-target by meaning: replace-then-put keeps exactly
		// one edge per input rel, so a re-bind needs no separate unbind.
		cleared, err := t.replaceSingleEdge(input, lsrc, ldst)
		if err != nil {
			return err
		}
		put, err := t.putEdge(input, lsrc, ldst, nil, false)
		if err != nil {
			return err
		}
		if !cleared && !put {
			return nil
		}
		if err := t.bumpVersion(lsrc); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpLink, lsrc.ID, lsrc.Kind, map[string]any{
			"rel": input, "dst": ldst.ID, "dstType": ldst.Kind,
		})
	})
}
