package engine

// putBulk declines a run, with nothing written, where an input names a
// record a row, a tombstone or a former id already answers: the per-record
// path is the one that knows what to do with each.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestBulkRunDeclinesARecordAlreadyNamed(t *testing.T) {
	t.Parallel()
	unit := func(id string) substrate.PutInput {
		return substrate.PutInput{Kind: vocabulary.KindUnit, ID: id, Properties: map[string]any{"name": id}}
	}
	for _, c := range []struct {
		name  string
		plant func(context.Context, *dataset) error
	}{
		{"a row", func(ctx context.Context, ds *dataset) error {
			_, err := ds.Put(ctx, substrate.ActorSystem, unit("kg"))
			return err
		}},
		{"a tombstone", func(ctx context.Context, ds *dataset) error {
			if _, err := ds.Put(ctx, substrate.ActorSystem, unit("kg")); err != nil {
				return err
			}
			_, err := ds.Delete(ctx, substrate.ActorSystem, vocabulary.KindUnit, "kg", substrate.DeleteInput{})
			return err
		}},
		// The trail a merge of kg into kilo leaves, with no row of kg's: the
		// former-id effect rides the put's entry (settleFold).
		{"a former id", func(ctx context.Context, ds *dataset) error {
			return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				if _, err := t.put(unit("kilo")); err != nil {
					return err
				}
				return t.recordFormerID(vocabulary.KindUnit, "kg", "kilo")
			})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey),
				WithKindsFS(SeedKinds), WithUnitSeed(false))
			if err != nil {
				t.Fatalf("open engine: %v", err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			d, err := svc.Dataset(ctx, testdb.Repository(t))
			if err != nil {
				t.Fatal(err)
			}
			ds := d.(*dataset)
			if err := c.plant(ctx, ds); err != nil {
				t.Fatalf("plant %s: %v", c.name, err)
			}
			var refusal bulkRefusal
			if err := ds.inTx(ctx, substrate.ActorSeed, true, func(tx *txn) error {
				var err error
				if refusal, err = tx.putBulk([]substrate.PutInput{unit("g"), unit("kg")}); err != nil {
					return err
				}
				if tx.bulk != nil || tx.maxSeq != 0 || len(tx.folded) != 0 {
					t.Errorf("the declined run left a queue %v, entries to %d or %d effects", tx.bulk != nil, tx.maxSeq, len(tx.folded))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if refusal != bulkHeld {
				t.Fatalf("the run with %s under an input: %q, want %q", c.name, refusal, bulkHeld)
			}
			if rec, err := ds.Get(ctx, vocabulary.KindUnit, "g"); err == nil {
				t.Fatalf("the declined run wrote g: %+v", rec)
			}
		})
	}
}
