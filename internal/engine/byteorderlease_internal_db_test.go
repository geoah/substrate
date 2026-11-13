package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/testdb"
)

// A registration and migration 0021 interleave either way, and neither way
// lets a repository the migration did not check be served across it. A
// registration takes its writer lease before it writes its row. One whose row
// committed before the migration locks `repositories` is in the scan, and the
// lease its server holds refuses the migration. One that has its lease but
// not its row when the migration takes the table waits at its row until the
// migration commits, and creates its repository after it.
func TestTheByteOrderMigrationSeesARegistrationEitherWay(t *testing.T) {
	t.Parallel()
	var byteOrder migration
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.Version == 21 {
			byteOrder = m
		}
	}
	open := func(t *testing.T) (*service, string) {
		t.Helper()
		dsn := MigratedDSN(t)
		svc, err := OpenForTest(t, context.Background(), dsn,
			WithDataRoot(t.TempDir()), WithKindsFS(SeedKinds), WithCredentialKey(TestCredentialKey))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc.(*service), dsn
	}

	t.Run("the registration commits first", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		svc, dsn := open(t)
		name := testdb.Repository(t)
		if _, err := svc.CreateRepository(ctx, name); err != nil {
			t.Fatal(err)
		}
		conn, err := rawDBOf(t, dsn).Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := applyMigration(ctx, conn, byteOrder); err == nil || !strings.Contains(err.Error(), "holds repository "+name) {
			t.Fatalf("0021 after a registration its server still holds: %v, want it refused naming %s", err, name)
		}
	})

	t.Run("the migration takes the table first", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		svc, dsn := open(t)
		name := testdb.Repository(t)
		// The registration stops with its lease taken and its seed written,
		// before its row.
		leased, proceed := make(chan struct{}), make(chan struct{})
		svc.testFailAfterSeed = func() error {
			close(leased)
			<-proceed
			return nil
		}
		created := make(chan error, 1)
		go func() {
			_, err := svc.CreateRepository(ctx, name)
			created <- err
		}()
		<-leased

		conn, err := rawDBOf(t, dsn).Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, byteOrder.UpSQL); err != nil {
			t.Fatalf("0021 beside a registration that has no row yet: %v, want it run", err)
		}
		close(proceed)
		select {
		case err := <-created:
			t.Fatalf("the registration wrote its row while the migration held the table: %v", err)
		case <-time.After(500 * time.Millisecond):
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-created:
			if err != nil {
				t.Fatalf("the registration after the migration: %v", err)
			}
		case <-time.After(time.Minute):
			t.Fatal("the registration did not finish after the migration committed")
		}
		if _, err := svc.Dataset(ctx, name); err != nil {
			t.Fatalf("open the repository registered across the migration: %v", err)
		}
	})
}
