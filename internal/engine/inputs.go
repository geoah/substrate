package engine

// Bundle input resolution: a bundle declares named inputs (vocabulary
// BundleInput), each naming a kind; records of that kind are ordinary and
// unbounded, and resolution picks ONE per input, in a fixed order:
//
//   1. the BOUND record — an entry in the bundle row's own `bindings`, a keyed
//      reference whose key is the input name, written by the bind verb;
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
// TOMBSTONED (resurrectable by split), the binding stands and the input reads
// DANGLING — the choice is preserved because the record may come back. Once GC
// hard-deletes the record the binding names nothing, and the input reads
// dangling until somebody rebinds or unbinds it: the pointer is a value on the
// bundle row, and a purge at the far end does not reach into another record's
// properties to erase what it says.

import (
	"context"
	"fmt"
	"strings"

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

	// 1. The bound record, read off the bundle row's own `bindings`.
	bundleRow, err := ds.loadRowDB(ctx, eref{Kind: kindBundle, ID: b.Identity()})
	if err != nil {
		return ri, err
	}
	if written, bound := bindingOf(bundleRow, name); bound {
		// THE BINDING FOLLOWS A MERGE. Nothing repoints a stored reference when
		// a record loses a merge (decision 0044), so the binding keeps naming
		// the id its author wrote and the former-id trail is what says where
		// that record lives now. Resolving before the liveness check is what
		// keeps merging two account records from breaking every bundle bound to
		// the loser.
		dst, err := ds.canonicalOf(ctx, ds.db, written)
		if err != nil {
			return ri, err
		}
		row, err := ds.loadRowDB(ctx, dst)
		if err != nil {
			return ri, err
		}
		if row == nil || row.DeletedAt != nil || dst.Kind != in.Kind {
			// The binding outlived its record (or the kind drifted under an
			// upgrade). A dangling explicit choice is a problem to show,
			// never silently papered over by the default rules below.
			//
			// It names the written id AND, where the trail moved, the record it
			// resolves to: an owner told only "bound to <loser>, which no longer
			// resolves" after a merge has to walk the trail by hand to find out
			// what actually went wrong with the winner.
			ri.Problem = substrate.SetupDangling
			ri.Detail = danglingDetail(written, dst, row, in.Kind)
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

// danglingDetail says what the binding names, where the former-id trail takes
// it, and what is wrong at the far end: gone, deleted, or a record of another
// kind after an upgrade moved the input. `row` is what `dst` loaded, nil when
// nothing is there.
func danglingDetail(written, dst eref, row *erow, want string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "bound to %s/%s", written.Kind, written.ID)
	if dst != written {
		fmt.Fprintf(&b, ", which merged into %s/%s", dst.Kind, dst.ID)
	}
	switch {
	case row == nil:
		b.WriteString(", which no longer exists")
	case row.DeletedAt != nil:
		b.WriteString(", which is deleted")
	default:
		fmt.Fprintf(&b, ", which is a %s and the input takes a %s", dst.Kind, want)
	}
	b.WriteString(" — rebind or unbind the input")
	return b.String()
}

// propBindings is the bundle row's keyed reference holding one binding per
// input name. It is `managed:`, so only these verbs write it and an install
// that rewrites the declaration leaves the owner's choices alone.
const propBindings = "bindings"

// bindingOf reads one input's bound record off a bundle row, and whether the
// input is bound at all. An unbound input and one bound to nothing are the same
// answer, so a caller never has to tell them apart.
func bindingOf(bundle *erow, input string) (eref, bool) {
	if bundle == nil {
		return eref{}, false
	}
	m, ok := bundle.Props[propBindings].(map[string]any)
	if !ok {
		return eref{}, false
	}
	kind, id, ok := vocabulary.SplitRecordPath(referencePathOf(m[input]))
	if !ok {
		return eref{}, false
	}
	return eref{Kind: kind, ID: id}, true
}

// bindingsOf copies a bundle row's whole binding map, ready to be written back
// with one key changed. Never the stored map itself: the row the write path
// diffs against must not move under it.
func bindingsOf(bundle *erow) map[string]any {
	out := map[string]any{}
	if bundle == nil {
		return out
	}
	if m, ok := bundle.Props[propBindings].(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// BindBundleInput points a bundle's input at a chosen record (the binding the
// resolver's first step reads), or clears the choice when recordID is empty.
// `bindings` is a managed property, so an ordinary write cannot touch it and
// this verb is the one door: it validates the input exists and the record is
// live and of the input's kind, then writes the bundle row as the system actor
// — a changelog entry like any write, so a rebuild replays it. A disabled
// bundle's configuration is frozen, and bind IS configuration, so it refuses
// like any input-record write would.
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
			// Lock the bundle row so an unbind cannot interleave a concurrent
			// uninstall's teardown or another bind.
			srcRow, err := t.loadRow(src, true)
			if err != nil {
				return err
			}
			if srcRow == nil || srcRow.DeletedAt != nil {
				return fmt.Errorf("%w: bundle %s", substrate.ErrNotFound, bundleID)
			}
			if _, bound := bindingOf(srcRow, input); !bound {
				return nil
			}
			bindings := bindingsOf(srcRow)
			// A keyed map drops the key its value is null at (coerceKeyed), so
			// this is the unbind — the key goes, not its value.
			bindings[input] = nil
			return t.writeBindings(src, bindings)
		})
	}
	dst := eref{Kind: in.Kind, ID: recordID}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		// Both ends are validated UNDER the transaction's locks (the ascending
		// (kind, id) order every multi-record path takes), so a bind can neither
		// race the target's delete into a dangling binding nor race an uninstall
		// into a binding on a gone bundle row.
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
		// Bind is single-valued by meaning: one key, one pointer, so a re-bind
		// needs no separate unbind.
		bindings := bindingsOf(srcRow)
		bindings[input] = vocabulary.RecordPath(ldst.Kind, ldst.ID)
		return t.writeBindings(lsrc, bindings)
	})
}

// writeBindings puts the bundle row's whole binding map through the ordinary
// write path: no-op suppression holds, the refs index re-derives with the row,
// and the changelog carries the delta a rebuild replays.
func (t *txn) writeBindings(src eref, bindings map[string]any) error {
	was := t.internal
	t.internal = true
	defer func() { t.internal = was }()
	_, err := t.patch(src, substrate.PatchInput{Properties: map[string]any{propBindings: bindings}})
	return err
}
