package providertest

import (
	"testing"
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
	t.Parallel()
	requireUV(t)
	fake := newFakeGmail(t)
	fake.listed = []string{"m1"}
	fake.msgs["m1"] = gmailMessage("m1", "t-m1", "Rack layout",
		"alice@example.com", "ada@example.com", "1754820000000", "hi")
	fake.start(t)

	_, ds := newDataset(t)
	googleInstall(t, ds, gmailAPIAt(fake.ts.URL))

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
			"kind": googleAccountType,
		},
		"record": map[string]any{
			"id": "acct-named", "kind": googleAccountType,
			"properties": gmailStepProps(nil),
		},
		"repository": map[string]any{"owner": "test"},
	}

	s := newStepper(t, ds, googleGmailFn, cfg)
	s.setEnvelope(envelope)
	effects := s.drainApplying(nil)

	stamped := map[string]bool{}
	for i := range effects {
		ef := &effects[i]
		if ef.Action == "patch" && ef.Kind == googleAccountType {
			stamped[ef.ID] = true
		}
	}
	if !stamped["acct-named"] {
		t.Fatalf("the account the envelope named was not synced; stamps: %v", stamped)
	}
	if stamped["acct-other"] {
		t.Fatalf("a record-triggered delivery synced an account the envelope did not name; "+
			"stamps: %v", stamped)
	}
}
