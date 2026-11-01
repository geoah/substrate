package appsetup

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// reports records every progress a run reports.
type reports struct {
	mu   sync.Mutex
	seen []string
	gone bool
}

func (r *reports) report(progress []string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, strings.Join(progress, ","))
	return !r.gone
}

func (r *reports) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen) == 0 {
		return ""
	}
	return r.seen[len(r.seen)-1]
}

// run plans details, as the approval checks them, and runs that plan.
func run(t *testing.T, ds *fakeDataset, cat *catalog.Catalog, details ...substrate.AuthorizationDetail) (Outcome, *reports) {
	t.Helper()
	approved := plan(t, ds, cat, details...)
	return runApproved(ds, cat, approved, details...)
}

func runApproved(ds *fakeDataset, cat *catalog.Catalog, approved []substrate.AuthorizationSetup, details ...substrate.AuthorizationDetail) (Outcome, *reports) {
	r := &reports{}
	out := NewRunner(cat).Run(context.Background(), ds, substrate.ActorConsole, details, approved, r.report)
	return out, r
}

func appSetUp() []substrate.AuthorizationDetail {
	return []substrate.AuthorizationDetail{
		webhookDetail(appHook), accountDetail("phone", map[string]any{"displayName": "Ada's phone"}), pkgDetail(appPkg, 4),
	}
}

// A run lands packages first, requirements before what needs them, then the
// account, then the webhook, whatever the request's order; the webhook gets
// a key of its own and the account is created once.
func TestARunSetsUpPackagesThenAccountsThenWebhooks(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	var order []string
	ds.afterPut = func(in substrate.PutInput) {
		if in.Kind != vocabulary.TriggerKind || in.IfVersion != nil {
			order = append(order, in.Kind+"/"+in.ID)
		}
	}
	out, r := run(t, ds, cat, appSetUp()...)
	if out.Failure != nil || out.Moved || out.Gone {
		t.Fatalf("the run = %+v", out)
	}
	if got := strings.Join(ds.installedSoFar(), ","); got != basePkg+","+appPkg {
		t.Fatalf("installed %s", got)
	}
	if r.last() != "done,done,done" || !slices.Contains(r.seen, "waiting,running,done") || !slices.Contains(r.seen, "running,done,done") {
		t.Fatalf("progress = %v", r.seen)
	}
	if strings.Join(order, ",") != appAccount+"/phone,"+vocabulary.TriggerKind+"/"+appHook {
		t.Fatalf("writes = %v, want the account, then the webhook", order)
	}
	hook := ds.record(vocabulary.TriggerKind, appHook)
	if hook.Properties["enabled"] != true || !regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`).MatchString(vocabulary.TriggerKey(hook.Properties)) {
		t.Fatalf("the webhook = %+v", hook.Properties)
	}
	// Run again: everything is in place, and nothing is written or keyed
	// again.
	key, version := vocabulary.TriggerKey(hook.Properties), hook.Version
	out, _ = run(t, ds, cat, appSetUp()...)
	again := ds.record(vocabulary.TriggerKind, appHook)
	if out.Failure != nil || vocabulary.TriggerKey(again.Properties) != key || again.Version != version || len(ds.installedSoFar()) != 2 {
		t.Fatalf("a second run = %+v, trigger %+v", out, again)
	}
}

// A webhook trigger whose code moved to another package after the plan is
// not turned on: the run stops at its row and the page plans again.
func TestACallableMovedBetweenPlanAndRunStopsTheRun(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.beforeInstall = func(id string) {
		if id != appPkg {
			return
		}
		ds.beforeInstall = nil
		ds.afterPut = func(in substrate.PutInput) {
			if in.ID == appHook && in.IfVersion == nil {
				ds.afterPut = nil
				rec := ds.record(vocabulary.TriggerKind, appHook)
				rec.Properties["callable"] = map[string]any{"ref": baseFunction}
			}
		}
	}
	out, r := run(t, ds, cat, appSetUp()...)
	if out.Failure == nil || out.Failure.Row == nil || *out.Failure.Row != 0 || !out.Moved || out.Failure.Message != movedProblem {
		t.Fatalf("the run = %+v %+v", out, out.Failure)
	}
	if r.last() != "failed,done,done" {
		t.Fatalf("progress = %v", r.seen)
	}
	if hook := ds.record(vocabulary.TriggerKind, appHook); hook.Properties["enabled"] == true || vocabulary.TriggerKey(hook.Properties) != "" {
		t.Fatalf("a trigger whose code moved was turned on: %+v", hook.Properties)
	}
}

// A trigger edited between the run's read and its write is not undone: the
// write is held to the version read, and the run stops with the conflict.
func TestATriggerEditedWhileTheRunTurnsItOnIsAConflict(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.putErr = func(in substrate.PutInput) error {
		if in.Kind == vocabulary.TriggerKind && in.IfVersion != nil {
			return fmt.Errorf("%w: %s moved past version %d", substrate.ErrConflict, in.ID, *in.IfVersion)
		}
		return nil
	}
	out, _ := run(t, ds, cat, appSetUp()...)
	if out.Failure == nil || out.Failure.Row == nil || *out.Failure.Row != 0 || out.Moved ||
		!strings.HasPrefix(out.Failure.Message, "Turning the webhook on failed") || !strings.Contains(out.Failure.Message, "moved past version") {
		t.Fatalf("the run = %+v %+v", out, out.Failure)
	}
}

// A requirement the owner removed while the set up waited is one the
// approval never showed: the run stops before it writes anything.
func TestARequirementGainedWhileWaitingStopsTheRun(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	details := []substrate.AuthorizationDetail{pkgDetail(appPkg, 4)}
	approved := plan(t, ds, cat, details...)
	if len(approved[0].Package.Requires) != 0 {
		t.Fatalf("app brings %+v first with base in place", approved[0].Package.Requires)
	}
	ds.mu.Lock()
	delete(ds.statuses, basePkg)
	ds.mu.Unlock()
	out, r := runApproved(ds, cat, approved, details...)
	if out.Failure == nil || *out.Failure.Row != 0 || !out.Moved || r.last() != substrate.ProgressFailed {
		t.Fatalf("the run = %+v, progress %v", out, r.seen)
	}
	if installs := ds.installedSoFar(); len(installs) != 0 {
		t.Fatalf("a run whose plan moved installed %v", installs)
	}
	// One that lost a requirement another set up landed runs what is left.
	ds = newFakeDataset()
	approved = plan(t, ds, cat, details...)
	ds.installed(cat, basePkg, 2)
	if out, _ := runApproved(ds, cat, approved, details...); out.Failure != nil {
		t.Fatalf("a run whose requirement landed meanwhile = %+v", out.Failure)
	}
	if got := strings.Join(ds.installedSoFar(), ","); got != appPkg {
		t.Fatalf("installed %s, want app alone", got)
	}
}

// A webhook in place stays the app's only while it delivers to the package
// the approval named: one rebound while the set up waited stops it, though
// it would need nothing set up.
func TestAWebhookReboundWhileWaitingStopsTheRun(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	ds.installed(cat, appPkg, 4)
	ds.put(substrate.Record{Kind: vocabulary.TriggerKind, ID: appHook, Version: 1, Properties: map[string]any{
		"enabled": true, "callable": map[string]any{"ref": appFunction},
		"source": map[string]any{"webhook": map[string]any{"key": "the-key-0123456789abcdef"}},
	}})
	details := []substrate.AuthorizationDetail{pkgDetail(basePkg, 2), pkgDetail(appPkg, 4), webhookDetail(appHook)}
	approved := plan(t, ds, cat, details...)
	if approved[2].Action != substrate.SetupNone || approved[2].Webhook.Package != appPkg {
		t.Fatalf("the webhook = %+v %+v", approved[2], approved[2].Webhook)
	}
	ds.record(vocabulary.TriggerKind, appHook).Properties["callable"] = map[string]any{"ref": baseFunction}
	out, _ := runApproved(ds, cat, approved, details...)
	if out.Failure == nil || *out.Failure.Row != 2 || !out.Moved {
		t.Fatalf("the run = %+v", out)
	}
}

// The binding the owner approved is read once more before the code exists:
// a webhook that changed hands while the other rows ran stops the run.
func TestTheApprovedBindingIsCheckedAgainBeforeTheCode(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.afterPut = func(in substrate.PutInput) {
		if in.Kind == vocabulary.TriggerKind && in.ID == appHook && in.IfVersion != nil {
			ds.afterPut = nil
			ds.record(vocabulary.TriggerKind, appHook).Properties["callable"] = map[string]any{"ref": baseFunction}
		}
	}
	out, r := run(t, ds, cat, appSetUp()...)
	if out.Failure == nil || *out.Failure.Row != 0 || !out.Moved || r.last() != "failed,done,done" {
		t.Fatalf("the run = %+v, progress %v", out, r.seen)
	}
}

// An account deleted after the plan is not brought back by the create: the
// plan moved.
func TestAnAccountDeletedWhileTheRunWaitsIsNotBroughtBack(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat, basePkg, 2)
	ds.installed(cat, appPkg, 4)
	details := []substrate.AuthorizationDetail{pkgDetail(appPkg, 4), accountDetail("phone", nil)}
	approved := plan(t, ds, cat, details...)
	ds.putErr = func(in substrate.PutInput) error {
		if in.Kind == appAccount {
			ds.putErr = nil
			deleted := substrate.Record{Kind: appAccount, ID: "phone", Version: 2, Properties: map[string]any{}}
			deleted.DeletedAt = &deleted.UpdatedAt
			ds.put(deleted)
			return fmt.Errorf("%w: phone is at version 2", substrate.ErrConflict)
		}
		return nil
	}
	out, _ := runApproved(ds, cat, approved, details...)
	if out.Failure == nil || *out.Failure.Row != 1 || !out.Moved {
		t.Fatalf("the run = %+v", out)
	}
	if rec := ds.record(appAccount, "phone"); rec.DeletedAt == nil {
		t.Fatal("the deleted account was brought back")
	}
}

// A run whose request is gone stops at its next report and writes nothing
// after it.
func TestARunWhoseRequestIsGoneWritesNothing(t *testing.T) {
	cat := testCatalog(t)
	ds := newFakeDataset()
	r := &reports{gone: true}
	details := appSetUp()
	out := NewRunner(cat).Run(context.Background(), ds, substrate.ActorConsole, details, plan(t, ds, cat, details...), r.report)
	if !out.Gone || out.Failure != nil {
		t.Fatalf("the run = %+v", out)
	}
	if installs := ds.installedSoFar(); len(installs) != 0 {
		t.Fatalf("a run whose request was gone installed %v", installs)
	}
}

// A refusal the owner can act on is said as it is; a fault of the server
// is named as one and never shown.
func TestAFailureSaysTheRefusalAndHidesTheFault(t *testing.T) {
	if got := FailureWords("Installing it", fmt.Errorf("%w: the closure does not admit", substrate.ErrValidation)); !strings.Contains(got, "does not admit") {
		t.Fatalf("a refusal = %q", got)
	}
	if got := FailureWords("Installing it", fmt.Errorf("dial tcp 10.0.0.7:5432: refused")); strings.Contains(got, "10.0.0.7") || !strings.Contains(got, "server log") {
		t.Fatalf("a fault = %q", got)
	}
	if got := FailureWords("Installing it", context.DeadlineExceeded); !strings.Contains(got, "did not finish in time") {
		t.Fatalf("a lapse = %q", got)
	}
}

func TestALogLineNeverCarriesARequestsControlCharacters(t *testing.T) {
	forged := "providers.example.com/apple\nlevel=ERROR msg=\"forged\"\r\x1b[31m"
	got := logSafe(forged)
	if strings.ContainsAny(got, "\n\r\x1b") {
		t.Fatalf("logSafe(%q) = %q, still carries a control character", forged, got)
	}
	if !strings.HasPrefix(got, "providers.example.com/apple ") {
		t.Fatalf("logSafe(%q) = %q, lost the text around the control characters", forged, got)
	}
	if long := logSafe(strings.Repeat("a", 5000)); len(long) > 2000 {
		t.Fatalf("logSafe kept %d bytes, want at most 2000", len(long))
	}
}
