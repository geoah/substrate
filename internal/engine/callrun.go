package engine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A direct call of a function that declares `permissions.network` writes one
// `triggerrun` row with mode `call` (decision record 0106). Its outbound
// effects (a message sent, a pull request approved) leave nothing else in the
// repository, so the row is the audit: the callable, the caller, the time,
// the status and what the call returned. A successful call writes it in the
// transaction that commits the call's effects and settles its idempotency
// key; a call whose body ran and failed writes it in a transaction of its
// own, because nothing else commits. A call refused before the body runs
// (not found, bad input, a closed fence) writes nothing: nothing went out.

// callOutputCap is the most JSON a call run keeps of the call's output. A
// larger output lands as its size alone: the row is written in the call's
// own commit, and an answer too large for a record must not fail the call.
// An output no row stores (a NUL in a value or a key) lands the same way.
const callOutputCap = 4096

// callOutput is what a call run records of the call's output.
type callOutput struct {
	bytes int
	kept  bool
	value any
}

// auditsCall reports whether a direct call of fn writes a call run: fn, or a
// function its `permissions.call` grant reaches at any depth, reaches the
// network. A sub-call's effects land in the root's commit, so the root's run
// row covers what a networked callee sent.
func (ds *dataset) auditsCall(fn *vocabulary.Function) bool {
	reg := ds.registry()
	seen := map[string]bool{}
	var networked func(*vocabulary.Function) bool
	networked = func(f *vocabulary.Function) bool {
		if seen[f.Identity()] {
			return false
		}
		seen[f.Identity()] = true
		if len(f.Caps.Network) > 0 {
			return true
		}
		for _, name := range f.Caps.Call {
			if callee, err := reg.ResolveFunction(name); err == nil && networked(callee) {
				return true
			}
		}
		return false
	}
	return networked(fn)
}

// newCallRun opens the call run for one direct call of fn, before its body
// runs. The caller and the principal come from the request.
func newCallRun(ctx context.Context, fn *vocabulary.Function, caller substrate.Actor) runRecord {
	return runRecord{
		callable:  vocabulary.RecordPath(kindFunction, fn.Identity()),
		mode:      runner.ModeCall,
		attempt:   1,
		startedAt: nowUTC(),
		caller:    caller,
		principal: substrate.PrincipalFrom(ctx),
	}
}

// succeeded completes the call run of a call that settled clean.
func (r runRecord) succeeded(output any, effects []effect) runRecord {
	r.status = runStatusOK
	r.effects = effectsSummary(effects)
	r.output = summarizeOutput(output)
	return r
}

// summarizeOutput sizes the output as JSON and keeps it when it fits the cap
// and a row can store it. A null output keeps nothing beyond its size.
func summarizeOutput(output any) *callOutput {
	raw, err := json.Marshal(output)
	if err != nil {
		return nil
	}
	out := &callOutput{bytes: len(raw)}
	if output != nil && len(raw) <= callOutputCap && storableText(output) == nil {
		out.kept, out.value = true, output
	}
	return out
}

// putFailedCallRun records a call whose body ran and failed, in a transaction
// of its own. The request's cancellation is dropped: the body has run, and a
// client that gave up does not unmake what it sent. A failure to write the
// row is logged, never returned: the caller is owed the call's own error.
func (ds *dataset) putFailedCallRun(ctx context.Context, r runRecord, cause error) {
	r.status = runStatusFailed
	r.errMsg = storableReason(cause.Error())
	ctx = context.WithoutCancel(ctx)
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		return t.putSystemRun(r, false)
	})
	if err != nil {
		ds.svc.log.Error("substrate: write the call run of a failed call", "callable", r.callable, "error", err)
	}
}

// storableReason makes a failure message storable: a NUL or a byte that is
// not UTF-8 in a body's exception text would fail the row, and the audit of
// a call whose body ran must not be dropped over its message.
func storableReason(msg string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(msg, "\x00", "\uFFFD"), "\uFFFD")
}
