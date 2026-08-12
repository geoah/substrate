package engine

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/geoah/substrate/internal/runner"
)

// The record-triggered branch: an on-connect delivery names ONE account in its
// envelope, and the body must sync exactly that account and leave every other
// connected one for the schedule.
//
// This is a regression test for a defect the v1 rename surfaced: the delivery
// envelope SPLITS identity (runner.Envelope writes `type` as the bare local
// name and `authority` beside it), but the bodies compared `record.type` against a
// FULL identity, so the branch never matched. Every on-connect delivery fell
// through to the schedule branch and synced every due account instead, which
// also meant a freshly connected account with `syncFrequency: off` never
// synced at all. Nothing covered it, which is why the rename did not catch it.
func TestGoogleRecordTriggerSyncsOnlyTheNamedAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's contacts body warms through uv at install")
	}
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", "1754820000000", "hi")
	fake.start(t)

	ds := openInternalDataset(t)
	googleInstallRewired(t, ds, func(docs []map[string]any) {
		googlePointGmailAt(docs, fake.ts.URL)
	})

	googleSeedAccount(t, ds, "acct-named")
	googleSeedAccount(t, ds, "acct-other")

	// Both accounts want gmail and both are due (neither has ever synced), so
	// only the envelope can tell them apart.
	cfg := map[string]any{"accounts": []any{
		map[string]any{
			"id": "acct-named", "type": googleAccountType,
			"properties": gmailStepProps(nil), "token": "at-1",
		},
		map[string]any{
			"id": "acct-other", "type": googleAccountType,
			"properties": gmailStepProps(nil), "token": "at-2",
		},
	}}

	// The envelope exactly as runner.Envelope builds it: local name, authority
	// beside it. A body that compares the bare `type` against a full identity
	// sees no match here.
	envelope := map[string]any{
		"change": map[string]any{
			"seq": int64(1), "op": "update", "id": "acct-named",
			"kind": googleAuthority + "/account",
		},
		"record": map[string]any{
			"id": "acct-named", "kind": googleAuthority + "/account",
			"properties": gmailStepProps(nil),
			"edges":      map[string]any{},
		},
		"repository": map[string]any{"owner": "test"},
	}

	effects := drainGoogleDelivery(t, ds, googleGmailFn, cfg, envelope)

	stamped := map[string]bool{}
	for i := range effects {
		ef := &effects[i]
		if ef.Action == "patch" && ef.Type == googleAccountType {
			stamped[ef.ID] = true
		}
	}
	if !stamped["acct-named"] {
		t.Fatalf("the account the envelope named was not synced; stamps: %v", stamped)
	}
	if stamped["acct-other"] {
		t.Fatalf("an record-triggered delivery synced an account the envelope did not name; "+
			"stamps: %v", stamped)
	}
}

// drainGoogleDelivery runs a DELIVERY (envelope carried on every page of the
// chain, the way the dispatcher does it) to completion, applying each page's
// effects, and returns every effect the run produced.
func drainGoogleDelivery(t *testing.T, ds *dataset, fnID string,
	cfg, envelope map[string]any,
) []effect {
	t.Helper()
	fn, err := ds.registry().ResolveFunction(fnID)
	if err != nil {
		t.Fatalf("resolve %s: %v", fnID, err)
	}
	s := &googleStepper{t: t, ds: ds, fn: fn, cfg: cfg}
	var all []effect
	var resume any
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatalf("the paged chain did not drain in 40 steps")
		}
		s.n++
		effects, _, more, err := ds.runCallableRaw(context.Background(), fn, runner.Input{
			Mode:           runner.ModeCall,
			Config:         cfg,
			Envelope:       envelope,
			Resume:         resume,
			IdempotencyKey: fmt.Sprintf("test/googledelivery/%d", s.n),
		})
		if err != nil {
			t.Fatalf("delivery step %d: %v", s.n, err)
		}
		all = append(all, effects...)
		s.apply(effects)
		if more == nil {
			return all
		}
		cur, ok := more.Cursor.(map[string]any)
		if !ok {
			t.Fatalf("delivery step %d: cursor is a %T, want an object", s.n, more.Cursor)
		}
		resume = cur
	}
}
