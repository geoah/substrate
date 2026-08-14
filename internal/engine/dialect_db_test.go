package engine_test

// The stored-schema dialect ladder: repository open
// records the promotion steps and stamps the dialect; a binary whose maximum
// dialect is below the stored one refuses the open with the named error,
// which is a service condition, never an auth failure.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestSchemaDialectLadder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn,
			engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()

	// The open stamped the dialect and recorded every step of the ladder:
	// keyed, ordered, recorded — a fresh repository runs the (vacuous) ladder once.
	var dialect int
	if err := db.QueryRowContext(ctx, `SELECT dialect FROM vocabulary_dialect`).Scan(&dialect); err != nil {
		t.Fatalf("read stamped dialect: %v", err)
	}
	if dialect < 1 {
		t.Fatalf("stamped dialect = %d, want >= 1", dialect)
	}
	var steps int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM vocabulary_promotions`).Scan(&steps); err != nil {
		t.Fatalf("read recorded steps: %v", err)
	}
	if steps != dialect {
		t.Fatalf("recorded %d promotion steps for dialect %d — every step N -> N+1 must be recorded", steps, dialect)
	}

	// A reopen at the same dialect is silent and does not refuse.
	svc2 := open()
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen at the stamped dialect: %v", err)
	}
	_ = svc2.Close()

	// Simulate a store promoted by a NEWER binary: stored dialect above this
	// binary's maximum. The open must refuse with the named error — a service
	// condition (503 via the issue-010 path), never an auth failure, never a
	// silent skip of unknown rows.
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`, dialect+1); err != nil {
		t.Fatalf("bump stored dialect: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	_, err = svc3.Dataset(ctx, "geoah")
	if err == nil {
		t.Fatal("a store speaking a newer dialect must refuse the open")
	}
	if !errors.Is(err, engine.ErrVocabularyDialectNewer) {
		t.Fatalf("expected ErrVocabularyDialectNewer, got %v", err)
	}
	if errors.Is(err, substrate.ErrAuth) || errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refusal must not read as an auth failure or a missing repository: %v", err)
	}
	if !strings.Contains(err.Error(), "upgrade the substrate") {
		t.Fatalf("the refusal must be legible: %v", err)
	}

	// The refusal is stateless: winding the store back reopens the repository.
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`, dialect); err != nil {
		t.Fatalf("restore stored dialect: %v", err)
	}
	svc4 := open()
	defer func() { _ = svc4.Close() }()
	if _, err := svc4.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen after restoring the dialect: %v", err)
	}
}
