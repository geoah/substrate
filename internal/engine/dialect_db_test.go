package engine_test

// The stored-schema dialect gate, in both directions: repository open records
// the promotion and stamps the dialect; a binary whose maximum dialect is below
// the stored one refuses the open with the named error, which is a service
// condition and never an auth failure; and a store still holding dialect 1's
// `definition` blob refuses too, because no rung translates it any more.

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
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"),
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
	// Every step the ladder RAN is recorded, and the newest recorded step is the
	// stamp. A fresh repository never had the older shapes, so it records the
	// steps it passed through rather than one row per dialect that ever existed.
	var steps, newest int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*), coalesce(max(dialect), 0) FROM vocabulary_promotions`).Scan(&steps, &newest); err != nil {
		t.Fatalf("read recorded steps: %v", err)
	}
	if steps == 0 {
		t.Fatal("the open recorded no promotion step at all")
	}
	if newest != dialect {
		t.Fatalf("the newest recorded step is %d, the stamp %d — the two must agree", newest, dialect)
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

// A DIALECT-1 STORE REFUSES THE OPEN, AND IS NOT STAMPED. The rung that
// translated a `definition` blob into typed properties was deleted before the
// first release (#217), so the gate is all that is left: it refuses BEFORE the
// stamp, because the stamp is durable and one-way and a store marked dialect 2
// over dialect-1 rows would carry a mark that lies about its own rows.
func TestSchemaDialectRefusesADefinitionBearingStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey("test-cred-key"),
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
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// One declaration row put back into dialect 1's shape: its authored content
	// inside a `definition` blob. No live write path produces this, which is what
	// the planting seam is for.
	rows := declarationRows(t, ds)[kindDecl]
	if len(rows) == 0 {
		t.Fatalf("the repository holds no %s row", kindDecl)
	}
	target := rows[0]
	props := map[string]any{"definition": map[string]any{"authority": "core.substrate.reamde.dev"}}
	for k, v := range target.Properties {
		props[k] = v
	}
	if err := planter(t, ds).PlantDeclarationRow(ctx, kindDecl, target.ID, props); err != nil {
		t.Fatalf("plant the dialect-1 row: %v", err)
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// The stamp goes back to where a pre-dialect binary left it, so the gate runs
	// instead of returning early at the maximum.
	for _, q := range []string{`DELETE FROM vocabulary_dialect`, `DELETE FROM vocabulary_promotions`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("wind the dialect back: %v", err)
		}
	}

	svc2 := open()
	defer func() { _ = svc2.Close() }()
	_, err = svc2.Dataset(ctx, "geoah")
	if err == nil {
		t.Fatal("a store holding a `definition` blob must refuse the open")
	}
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("expected ErrDeclarationUntranslated, got %v", err)
	}
	for _, want := range []string{target.ID, "definition", "wipe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
	// AND IT DID NOT STAMP. A mark left behind here would say the rows are typed
	// and make the very next open return early at the maximum.
	var stamped int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM vocabulary_dialect`).Scan(&stamped); err != nil {
		t.Fatalf("read the stamp: %v", err)
	}
	if stamped != 0 {
		t.Fatalf("the refused open stamped the dialect anyway (%d rows)", stamped)
	}
}
