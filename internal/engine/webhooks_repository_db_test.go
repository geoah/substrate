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
func newHookDatasetWithDSN(t *testing.T, triggers []enginetest.Trigger, fns ...map[string]any) (substrate.Service, substrate.Dataset, string) {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	importVocabulary(t, ds)
	if err := enginetest.Install(ctx, ds, owner, fnConnector(triggers, fns...)); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return svc, ds, dsn
}

// A payload parked before a repository carried an authority holds
// `repository: {owner}` alone, and the retry restores it verbatim. The engine
// fills the names it lacks from the dataset, so a replayed body reads the same
// two names a fresh delivery does.
func TestParkedEnvelopeGainsTheAuthorityOnRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newHookDatasetWithDSN(t,
		[]enginetest.Trigger{hookTrigger("hook-repo", webhookSource(""), "hookrepo", true)},
		pyFn("hookrepo", map[string]any{
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
		}, []any{widgetType}, hookRepoSource),
	)
	if _, err := engine.ReceiveWebhookSync(ctx, svc, testdb.Repository(t), "hook-repo", "", jsonHook("repo", "repo")); err != nil {
		t.Fatalf("receive: %v", err)
	}
	failures, err := ds.TriggerFailures(ctx, "hook-repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 {
		t.Fatalf("parked failures = %+v, want the one the closed gate made", failures)
	}

	// The payload as a binary before the authority wrote it.
	raw, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.ExecContext(ctx, `
		UPDATE trigger_failures
		SET payload = jsonb_set(payload, '{repository}', jsonb_build_object('owner', $2::text))
		WHERE id = $1`, failures[0].ID, testdb.Repository(t)); err != nil {
		t.Fatalf("rewrite the parked payload: %v", err)
	}

	mustPut(t, ds, owner, substrate.PutInput{Kind: widgetType, ID: "gate", Properties: map[string]any{"name": "open"}})
	if _, err := ds.RetryTriggerFailure(ctx, "hook-repo", failures[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got := hookEcho(t, ds, "repo-echo")
	if got["name"] != testdb.Repository(t) || got["want"] != testdb.Repository(t) {
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
		`INSERT INTO repositories (id, authority, dek, history_generation) VALUES ($1, $2, $3, $4)`,
		"ghost.example.com", "ghost.example.com", []byte("not a wrapped dek"), "ghost-generation"); err != nil {
		t.Fatalf("plant the repository row: %v", err)
	}
	if _, err := engine.ReceiveWebhookSync(ctx, svc, "ghost.example.com", "hook-open", "", jsonHook("x", "x")); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("err = %v, want the door's one refusal", err)
	}
}
