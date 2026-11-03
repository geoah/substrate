package engine

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAppBuildWritesOnlyChangesOrDailyCheckIns(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	token, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "App", ClientID: "https://app.example/client.json"})
	if err != nil {
		t.Fatal(err)
	}
	ctx = substrate.WithPrincipal(ctx, token.ID)
	count := func() int {
		t.Helper()
		var n int
		if err := ds.db.QueryRowContext(ctx, "SELECT count(*) FROM changelog WHERE kind=$1 AND record_id=$2", kindToken, token.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	check := func(floors map[string]int64, version string, writes int) {
		t.Helper()
		before := count()
		if err := ds.RecordAppBuild(ctx, floors, version); err != nil {
			t.Fatal(err)
		}
		if got := count() - before; got != writes {
			t.Fatalf("check-in wrote %d entries, want %d", got, writes)
		}
	}
	floors := map[string]int64{"app.example/notes": 1}
	check(floors, "1.0", 1)
	check(floors, "1.0", 0)
	check(floors, "1.1", 1)
	floors["app.example/notes"] = 2
	check(floors, "1.1", 1)
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		_, err := tx.put(substrate.PutInput{Kind: kindToken, ID: token.ID, Properties: map[string]any{"appCheckedAt": tx.now.Add(-25 * time.Hour).Format(time.RFC3339Nano)}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check(floors, "1.1", 1)
	check(floors, "1.1", 0)
	check(nil, "1.1", 1)
	check(map[string]int64{}, "1.1", 0)
}
