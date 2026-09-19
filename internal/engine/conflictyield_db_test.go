package engine_test

// Two invocations of one sync over one account. The failure this holds shut:
// an on-request trigger fires while a scheduled one is mid-drain, both read
// the account's cursor, both advance it, and one write wins — duplicated work,
// never lost data. A guarded cursor write (`ifVersion` + `onConflict: yield`)
// makes the loser exit without writing, and — the point of the policy —
// without PARKING, because a race two triggers are designed to have is not an
// operator's problem (decision 0093).

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	leasePackage = "synclease.test.dev/synclease"
	leaseAccount = leasePackage + "/account"
	leaseSync    = leasePackage + "/sync"
)

// leaseSyncSource is the fixture sync: it stamps the account's cursor guarded
// by the version it believes it read. A real sync takes that version from the
// delivery envelope (`host.version(envelope["record"])`) or from
// `config.accounts`; the fixture lets the record PIN it, which is how a stale
// stamp becomes deterministic instead of a wall-clock race.
const leaseSyncSource = `
ACCOUNT = "synclease.test.dev/synclease/account"

def main(input, host):
    rec = (input.get("envelope") or {}).get("record") or {}
    props = rec.get("properties") or {}
    pinned = props.get("stampVersion")
    version = int(pinned) if pinned else host.version(rec)
    kw = {}
    if props.get("onConflict"):
        kw["on_conflict"] = props["onConflict"]
    host.effects.patch(ACCOUNT, rec["id"],
                       properties={"syncCursor": "drained"},
                       if_version=version, **kw)
    return {"output": {"stamped": version}}
`

// installLeaseBundle stands up the account kind, the sync, and one trigger per
// id in `triggers` — two of them is the whole scenario: one delivery per
// trigger over ONE account record.
func installLeaseBundle(t *testing.T, ds substrate.Dataset, triggers ...string) {
	t.Helper()
	trigs := make([]enginetest.Trigger, 0, len(triggers))
	for _, id := range triggers {
		trigs = append(trigs, enginetest.Trigger{
			ID: id,
			Properties: map[string]any{
				"enabled": true,
				"source": map[string]any{"record": map[string]any{
					"kinds": []any{leaseAccount}, "ops": []any{"create", "update"},
				}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", leaseSync),
			},
		})
	}
	err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "synclease", Authority: leasePackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(leasePackage, 0),
			vocabulary.ActorManifest(leasePackage, vocabulary.PackageActor(leasePackage)),
			vocabulary.KindManifest(leasePackage, map[string]any{"singular": "account"},
				map[string]any{"properties": map[string]any{
					"name":         map[string]any{"type": "string"},
					"syncCursor":   map[string]any{"type": "string", "fts": false},
					"stampVersion": map[string]any{"type": "string", "fts": false},
					"onConflict":   map[string]any{"type": "string", "fts": false},
				}}),
			vocabulary.FunctionManifest(leasePackage, "sync", map[string]any{
				"description": "stamps the account's sync cursor under a version guard",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{leaseAccount}},
				"source":      leaseSyncSource,
			}),
		},
		Triggers: trigs,
	})
	if err != nil {
		t.Fatalf("install the sync-lease bundle: %v", err)
	}
}

// putLeaseAccount creates the account the sync runs over. It lands at version
// 1, so `stampVersion: "0"` is a stamp from before the record existed — the
// stale stamp a losing invocation carries.
func putLeaseAccount(t *testing.T, ds substrate.Dataset, id, stampVersion, onConflict string) *substrate.Record {
	t.Helper()
	rec, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: leaseAccount, ID: id,
		Properties: map[string]any{
			"name": "acme", "stampVersion": stampVersion, "onConflict": onConflict,
		},
	})
	if err != nil {
		t.Fatalf("put account: %v", err)
	}
	return rec
}

// runStatuses tallies the settled deliveries by status.
func runStatuses(t *testing.T, ds substrate.Dataset) map[string]int {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{triggerRunType}}, First: 200,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	out := map[string]int{}
	for _, r := range page.Records {
		status, _ := r.Properties["status"].(string)
		out[status]++
	}
	return out
}

// runReasons collects every settled delivery's recorded reason.
func runReasons(t *testing.T, ds substrate.Dataset) string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{triggerRunType}}, First: 200,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var sb strings.Builder
	for _, r := range page.Records {
		if reason, _ := r.Properties["reason"].(string); reason != "" {
			sb.WriteString(reason)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// TestGuardedCursorWriteYieldsWithoutParking: a stamp made under a version the
// account has moved past writes nothing and settles as a SKIP. The cursor
// advances, no failure parks, and the run record names the conflict — the
// distinguishable outcome a sync needs, because the alternative (a park every
// scheduled pass) is an alarm for something working as designed.
func TestGuardedCursorWriteYieldsWithoutParking(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installLeaseBundle(t, ds, "sync-solo")
	// stampVersion 0 against a record that exists at version 1.
	putLeaseAccount(t, ds, "acct-1", "0", "yield")

	if n := process(t, ds); n != 0 {
		t.Fatalf("a yielded delivery applied %d effects, want 0", n)
	}

	failures, err := ds.TriggerFailures(ctx, "sync-solo")
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("a yielded delivery parked: %+v", failures)
	}
	acct, err := ds.Get(ctx, leaseAccount, "acct-1")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncCursor"]; got != nil {
		t.Fatalf("the losing stamp wrote the cursor: %v", got)
	}
	if acct.Version != 1 {
		t.Fatalf("the account sits at version %d, want 1 — the yielded write was not rolled back", acct.Version)
	}
	if got := runStatuses(t, ds); got["skipped"] != 1 || got["parked"] != 0 || got["ok"] != 0 {
		t.Fatalf("run statuses %v, want exactly one skipped", got)
	}
	if reasons := runReasons(t, ds); !strings.Contains(reasons, "ifVersion 0, stored 1") {
		t.Fatalf("the skip does not name the version race: %q", reasons)
	}
	// The cursor moved past the change, so the next pass does not redeliver it.
	if st := statusOf(t, ds, "sync-solo"); st.Lag != 0 {
		t.Fatalf("a yielded delivery left the trigger %d behind", st.Lag)
	}
}

// TestGuardedCursorWriteParksWithoutTheYieldPolicy: the policy is OPT-IN. The
// same stale stamp without `onConflict` is the ordinary read-then-write
// conflict it has always been — the delivery parks, so a writer that expected
// to win still hears about losing.
func TestGuardedCursorWriteParksWithoutTheYieldPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installLeaseBundle(t, ds, "sync-solo")
	putLeaseAccount(t, ds, "acct-1", "0", "")

	process(t, ds)

	failures, err := ds.TriggerFailures(ctx, "sync-solo")
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("an unguarded-policy conflict parked %d deliveries, want 1", len(failures))
	}
	if !strings.Contains(failures[0].LastError, "version conflict") {
		t.Fatalf("the park does not name the conflict: %s", failures[0].LastError)
	}
	acct, err := ds.Get(ctx, leaseAccount, "acct-1")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if got := acct.Properties["syncCursor"]; got != nil {
		t.Fatalf("a parked delivery wrote the cursor: %v", got)
	}
}

// TestTwoInvocationsOverOneAccountOneAdvances is T-033's acceptance: two
// invocations of one sync over one account, both stamping the version they
// read. One advances the cursor; the other observes the conflict and exits
// without writing. Both invocations carry the SAME stamped version, so the
// outcome does not depend on how the two deliveries interleave — which is the
// point: whatever the scheduling, exactly one advance lands.
func TestTwoInvocationsOverOneAccountOneAdvances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installLeaseBundle(t, ds, "sync-scheduled", "sync-on-request")
	// Both deliveries read version 1 — the state of the account when the pass
	// began — exactly as two invocations that started together would.
	putLeaseAccount(t, ds, "acct-1", "1", "yield")

	process(t, ds)

	acct, err := ds.Get(ctx, leaseAccount, "acct-1")
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.Properties["syncCursor"] != "drained" {
		t.Fatalf("no invocation advanced the cursor: %v", acct.Properties)
	}
	if acct.Version != 2 {
		t.Fatalf("the account sits at version %d, want 2 — exactly one advance", acct.Version)
	}
	for _, id := range []string{"sync-scheduled", "sync-on-request"} {
		failures, err := ds.TriggerFailures(ctx, id)
		if err != nil {
			t.Fatalf("failures for %s: %v", id, err)
		}
		if len(failures) != 0 {
			t.Fatalf("%s parked: %+v", id, failures)
		}
	}
	// Both cursors moved past the change, so the loser is not redelivered and
	// the winner does not stamp twice.
	if n := process(t, ds); n != 0 {
		t.Fatalf("a second pass applied %d effects — a cursor stood still", n)
	}
	if again, err := ds.Get(ctx, leaseAccount, "acct-1"); err != nil || again.Version != 2 {
		t.Fatalf("the account moved on the second pass: %+v (%v)", again, err)
	}
	if got := runStatuses(t, ds); got["ok"] != 1 || got["skipped"] != 1 || got["parked"] != 0 {
		t.Fatalf("run statuses %v, want one ok and one skipped", got)
	}
	if reasons := runReasons(t, ds); !strings.Contains(reasons, "ifVersion 1, stored 2") {
		t.Fatalf("the loser's skip does not name the version it lost by: %q", reasons)
	}
}
