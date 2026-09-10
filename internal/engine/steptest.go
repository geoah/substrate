package engine

// The paged-callable stepper, exported for one caller: internal/providertest,
// where the provider bundle suites live. They sit outside this package so
// `go test ./internal/engine/...` spawns no uv, and a package across the line
// cannot see export_test.go, so the seam they need is exported here instead.
//
// It is a TEST SEAM of the second kind (see seams.go, which holds the first):
// it installs no hook, it drives a production path with the dispatcher's
// bookkeeping left off. Nothing in cmd/ or in internal/ outside a test calls
// it, and it exports no more than the suites read: one invocation of a
// callable without the dispatcher, its effects in the four fields a test
// asserts on, and the effect commit production runs (txn.applyEffects).

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// StepEffect is one effect a stepped invocation returned, in the fields a
// test reads: what it does, to which kind, at which id, with what. The
// effect the engine applies stays inside the Stepper, so Apply commits what
// the body returned and not a re-encoding of this view.
type StepEffect struct {
	Action     string
	Kind       string
	ID         string
	Properties map[string]any
}

// Stepper drives ONE callable page by page with no trigger machinery, so the
// paged-checkpoint cursor is observable: Step runs one invocation and hands
// back its effects and its continuation cursor while committing nothing, and
// Apply commits the last step's effects the way the dispatcher does.
type Stepper struct {
	ds   *dataset
	fn   *vocabulary.Function
	cfg  map[string]any
	env  map[string]any
	n    int
	last []effect
}

// NewStepper resolves fnID against the dataset's live registry. cfg is the
// injected `config` every invocation receives, which for a provider sync is
// the accounts it walks and their tokens.
func NewStepper(ds substrate.Dataset, fnID string, cfg map[string]any) (*Stepper, error) {
	d, ok := ds.(*dataset)
	if !ok {
		return nil, fmt.Errorf("engine: NewStepper wants this package's dataset, got a %T", ds)
	}
	fn, err := d.registry().ResolveFunction(fnID)
	if err != nil {
		return nil, fmt.Errorf("engine: resolve %s: %w", fnID, err)
	}
	return &Stepper{ds: d, fn: fn, cfg: cfg}, nil
}

// SetConfig replaces the injected config between steps, which is how a test
// flips a feature toggle mid-drain: the pinned stage list has to keep the
// walk stable across it.
func (s *Stepper) SetConfig(cfg map[string]any) { s.cfg = cfg }

// SetEnvelope puts a delivery envelope on every following step, the way the
// dispatcher carries one across a paged chain: a record-triggered delivery
// names ONE record, and a body that reads it wrong syncs the wrong thing.
func (s *Stepper) SetEnvelope(env map[string]any) { s.env = env }

// Step runs one invocation of the chain: resume is the previous page's cursor,
// nil for a fresh delivery. The third return is the continuation cursor, nil
// once the chain drained.
func (s *Stepper) Step(ctx context.Context, resume any) ([]StepEffect, any, map[string]any, error) {
	s.n++
	s.last = nil
	effects, out, more, err := s.ds.runCallableRaw(ctx, s.fn, runner.Input{
		Mode:           runner.ModeCall,
		Config:         s.cfg,
		Envelope:       s.env,
		Resume:         resume,
		IdempotencyKey: fmt.Sprintf("test/step/%s/%d", s.fn.Identity(), s.n),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	s.last = effects
	views := make([]StepEffect, 0, len(effects))
	for _, ef := range effects {
		views = append(views, StepEffect{
			Action: ef.Action, Kind: ef.Type, ID: ef.ID, Properties: ef.Properties,
		})
	}
	if more == nil {
		return views, out, nil, nil
	}
	cur, ok := more.Cursor.(map[string]any)
	if !ok {
		return nil, nil, nil, fmt.Errorf("engine: step %d handed back a %T cursor, want an object", s.n, more.Cursor)
	}
	return views, out, cur, nil
}

// Apply commits the last Step's effects through the SAME write half the
// dispatcher uses (txn.applyEffects in effects.go): one transaction under the
// callable's own actor, so a connector-owned property admits. A step that
// staged nothing commits nothing.
//
// It is the write half and nothing else. A real delivery also stamps the
// causal seq the effects were caused by, advances or clears the resume cursor
// under its version CAS, and settles the delivery in the same transaction;
// none of that happens here. So a stepped chain commits rows the way
// production does, and says nothing about attribution, resumption or
// settlement — the trigger suites in internal/engine own those.
func (s *Stepper) Apply(ctx context.Context) error {
	if len(s.last) == 0 {
		return nil
	}
	effects := s.last
	return s.ds.inTx(ctx, substrate.Actor(s.fn.Actor()), false, func(tx *txn) error {
		return tx.applyEffects(s.fn.Caps.Emit, effects)
	})
}
