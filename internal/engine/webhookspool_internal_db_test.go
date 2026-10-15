package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The door answers while another transaction holds the changelog lock: the
// request is in the spool and in no table until the lock is free, and then
// the drain records it once (decision 0167). The lock is held the way a
// background writer holds it, in a transaction of its own.
func TestAWebhookIsAnsweredWhileTheChangelogLockIsHeld(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installLedgerConnector(t, ds, nowUTC().Add(-time.Hour))
	ctx := context.Background()

	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = barrier.Rollback() }()
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatal(err)
	}

	answerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	fid, err := ds.svc.receiveWebhook(answerCtx, testdb.Repository(t), ledgerHook, "", substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{"content-type": "application/json"},
		Body:    []byte(`{"say":"answered under a held lock"}`),
	}, webhookFireDetached)
	if err != nil {
		t.Fatalf("the door did not answer while the changelog lock was held: %v", err)
	}
	if ids := failureIDs(t, ds, ledgerHook); len(ids) != 0 {
		t.Fatalf("the request reached the ledger past a held lock: %v", ids)
	}
	if st := ds.spoolStats(); st.Entries != 1 {
		t.Fatalf("the spool holds %d requests, want 1", st.Entries)
	}

	if err := barrier.Rollback(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for ds.spoolStats().Entries != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the drain never emptied the spool after the lock was freed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var n int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog WHERE op = 'delivery'
		  AND jsonb_path_exists(payload, '$.** ? (@.fireId == $fid && @.lastError == $pending)',
		      jsonb_build_object('fid', $1::text, 'pending', $2::text))`, fid, pendingWebhookError).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("delivery entries recording %s = %d, want 1", fid, n)
	}
}

// The spooled file is the request in the form the ledger records it, sealed
// under the repository's DEK and bound to its fire id: the undeclared header
// and the query are gone before the bytes reach the disk, and the file does
// not open as another fire's.
func TestASpooledWebhookIsNarrowedAndSealedBeforeItIsWritten(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	installLedgerConnector(t, ds, nowUTC().Add(-time.Hour))
	ctx := context.Background()
	tr, _, err := ds.triggerByID(ctx, ledgerHook)
	if err != nil {
		t.Fatal(err)
	}
	name, fid, err := ds.spoolWebhook(ctx, tr, substrate.WebhookRequest{
		Method: "POST", ContentType: "application/json",
		Headers: map[string]string{
			"content-type":   "application/json",
			"x-github-event": "declared-value",
			"x-api-token":    "undeclared-value",
		},
		Query: map[string][]string{"token": {"query-value"}},
		Body:  []byte(`{"say":"sealed"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := changelogfile.ReadSpoolFile(ds.dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.openPayload(sealed, spoolAAD(ds.Repository().ID, "hook-another")); err == nil {
		t.Fatal("the spooled file opens under another fire's binding")
	}
	raw, err := ds.openPayload(sealed, spoolAAD(ds.Repository().ID, fid))
	if err != nil {
		t.Fatalf("open the spooled file: %v", err)
	}
	var entry spooledWebhook
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	h := entry.Request.Headers
	if h["content-type"] != "application/json" || h["x-github-event"] != "declared-value" || h["x-api-token"] != "" || len(h) != 2 {
		t.Fatalf("spooled headers = %v, want the body's and the declared one", h)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["request"].(map[string]any)["query"]; ok {
		t.Fatalf("the spooled request carries a query: %s", raw)
	}
	if entry.Trigger != ledgerHook || entry.FireID != fid || string(entry.Request.Body) != `{"say":"sealed"}` {
		t.Fatalf("spooled entry = %+v", entry)
	}
}
