package engine_test

// How the public door names a repository, on the two paths that are not the
// happy one: an authority that resolves to a repository which will not open,
// and a delivery parked by a binary that wrote no authority into its payload.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// hookRepoSource echoes the delivery envelope's repository names, indexing
// them the way a body written against the documented shape does: a missing
// `authority` raises, rather than reading as an empty string.
var hookRepoSource = `
WIDGET = "` + widgetType + `"

def main(input, host):
    if host.records.get(WIDGET, "gate") is None:
        raise RuntimeError("gate closed")
    repo = (input.get("envelope") or {})["repository"]
    host.effects.put(WIDGET, "repo-echo", properties={
        "name": repo["authority"],
        "want": repo["owner"],
    })
    return {"output": {}}
`

// newHookDatasetWithDSN is newHookDataset keeping the DSN, for a test that
// needs the tamperer's seat beside the door.
func newHookDatasetWithDSN(t *testing.T, triggers []enginetest.Trigger, fns ...map[string]any) (substrate.Service, substrate.Dataset, fnOps, string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	if err := enginetest.Install(ctx, ds, owner, fnConnector(triggers, fns...)); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	ops, ok := ds.(fnOps)
	if !ok {
		t.Fatal("dataset does not implement the automation seam")
	}
	return svc, ds, ops, dsn
}

// A payload parked before a repository carried an authority holds
// `repository: {owner}` alone, and the retry restores it verbatim. The engine
// fills the names it lacks from the dataset, so a replayed body reads the same
// two names a fresh delivery does.
func TestParkedEnvelopeGainsTheAuthorityOnRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, ops, dsn := newHookDatasetWithDSN(t,
		[]enginetest.Trigger{hookTrigger("hook-repo", webhookSource(""), "hookrepo", true)},
		pyFn("hookrepo", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
		}, []any{widgetType}, hookRepoSource),
	)
	if _, err := engine.ReceiveWebhookSync(ctx, svc, "geoah.example.com", "hook-repo", "", jsonHook("repo", "repo")); err != nil {
		t.Fatalf("receive: %v", err)
	}
	failures, err := ops.TriggerFailures(ctx, "hook-repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 {
		t.Fatalf("parked failures = %+v, want the one the closed gate made", failures)
	}

	// The payload as a binary before the authority wrote it.
	raw, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.ExecContext(ctx, `
		UPDATE trigger_failures
		SET payload = jsonb_set(payload, '{repository}', '{"owner": "geoah"}'::jsonb)
		WHERE id = $1`, failures[0].ID); err != nil {
		t.Fatalf("rewrite the parked payload: %v", err)
	}

	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "gate", Properties: map[string]any{"name": "open"}})
	if _, err := ops.RetryTriggerFailure(ctx, "hook-repo", failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got := hookEcho(t, ds, "repo-echo")
	if got["name"] != "geoah.example.com" || got["want"] != "geoah" {
		t.Fatalf("the replayed envelope carried %v, want both repository names", got)
	}
}

// An authority that RESOLVES but whose repository will not open answers the
// same refusal as an unknown one: a 500 would tell an unauthenticated prober
// that this authority exists. The reason goes to the operator's log.
func TestWebhookRefusesARepositoryThatWillNotOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	// A control-plane row whose DEK is not a sealed one: the authority
	// resolves, and the open fails unwrapping it, which is the shape of a
	// credential or storage fault without one being staged.
	if _, err := rawDB(t, dsn).ExecContext(ctx,
		`INSERT INTO repositories (id, username, authority, dek) VALUES ($1, $2, $3, $4)`,
		"ghost", "ghost", "ghost.example.com", []byte("not a wrapped dek")); err != nil {
		t.Fatalf("plant the repository row: %v", err)
	}
	if _, err := engine.ReceiveWebhookSync(ctx, svc, "ghost.example.com", "hook-open", "", jsonHook("x", "x")); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("err = %v, want the door's one refusal", err)
	}
}
