package engine_test

// The stored-schema dialect gate: repository open stamps the dialect, and a
// binary whose maximum dialect is below the stored one refuses the open with
// the named error, which is a service condition and never an auth failure. A
// row carrying a `definition` blob refuses where it is read back, whatever the
// value is.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestVocabularyDialectGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := svc.Dataset(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()

	// The open stamped the dialect: a fresh repository leaves its first open at
	// the binary's maximum.
	var dialect int
	if err := db.QueryRowContext(ctx, `SELECT dialect FROM vocabulary_dialect`).Scan(&dialect); err != nil {
		t.Fatalf("read stamped dialect: %v", err)
	}
	if dialect != engine.MaxSchemaDialect() {
		t.Fatalf("stamped dialect = %d, want %d", dialect, engine.MaxSchemaDialect())
	}

	// A reopen at the same dialect is silent and does not refuse.
	svc2 := open()
	if _, err := svc2.Dataset(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("reopen at the stamped dialect: %v", err)
	}
	_ = svc2.Close()

	// Simulate a store stamped by a NEWER binary: stored dialect above this
	// binary's maximum. The open must refuse with the named error — a service
	// condition (503 via the issue-010 path), never an auth failure, never a
	// silent skip of unknown rows.
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`, dialect+1); err != nil {
		t.Fatalf("bump stored dialect: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	_, err = svc3.Dataset(ctx, testdb.Repository(t))
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
	if _, err := svc4.Dataset(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("reopen after restoring the dialect: %v", err)
	}
}

// A ROW CARRYING A `definition` REFUSES THE OPEN, BY THE KEY'S PRESENCE and
// whatever the value is: every value form is a case here. A check that looked
// at the VALUE instead would wave a JSON null through and rebuild an authority
// from half a declaration. Every declaration kind is planted, because the
// refusal is not allowed to be one kind's business.
func TestADefinitionBearingRowRefusesTheOpen(t *testing.T) {
	t.Parallel()
	blobs := map[string]any{
		"a map":    map[string]any{"authority": "substrate.reamde.dev", "package": "core"},
		"a null":   nil,
		"a string": "authority: substrate.reamde.dev/core",
		"a list":   []any{"authority"},
		"a number": float64(1),
	}
	for name, blob := range blobs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var covered []string
			for _, decl := range declarationKindRefs {
				if assertDefinitionBlobRefusesTheOpen(t, decl, blob) {
					covered = append(covered, decl)
				}
			}
			// A fresh core-seeded repository holds no row of some declaration
			// kinds, so those are passed over rather than asserted on. What is NOT
			// allowed is passing over everything: the helper must not report a kind
			// as covered without having reached the refusal, and an earlier
			// revision of this test skipped every case in silence.
			for _, want := range []string{
				"substrate.reamde.dev/core/kind",
				"substrate.reamde.dev/core/function",
				"substrate.reamde.dev/core/package",
			} {
				if !slices.Contains(covered, want) {
					t.Errorf("%s was never planted into; covered = %v", want, covered)
				}
			}
		})
	}
}

// declarationKindRefs is every kind a declaration row stores as: the gate reads
// them all, so the test plants into each one in turn.
var declarationKindRefs = []string{
	"substrate.reamde.dev/core/authority", "substrate.reamde.dev/core/package",
	"substrate.reamde.dev/core/actor",
	"substrate.reamde.dev/core/kind", "substrate.reamde.dev/core/trait",
	"substrate.reamde.dev/core/propertytype", "substrate.reamde.dev/core/recordmapping",
	"substrate.reamde.dev/core/function", "substrate.reamde.dev/core/agent",
	"substrate.reamde.dev/core/bundle",
}

// assertDefinitionBlobRefusesTheOpen plants one `definition` value onto one
// declaration kind's row and holds the reopen to the named refusal. It reports
// whether it planted anything: a fresh repository holds no row of some kinds,
// and the caller checks the coverage rather than this returning early in
// silence. It must NOT call t.Skip, which would end the whole subtest and every
// kind after this one with it.
func assertDefinitionBlobRefusesTheOpen(t *testing.T, declKind string, blob any) bool {
	t.Helper()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	// One declaration row wearing the retired blob. No live write path produces
	// this, which is what the planting seam is for; a jsonb null is not
	// reachable through it, so that one value goes in as raw SQL below.
	rows := declarationRows(t, ds)[declKind]
	if len(rows) == 0 {
		_ = svc.Close()
		return false
	}
	target := rows[0]
	if blob != nil {
		props := map[string]any{"definition": blob}
		for k, v := range target.Properties {
			props[k] = v
		}
		if err := planter(t, ds).PlantDeclarationRow(ctx, declKind, target.ID, props); err != nil {
			t.Fatalf("plant the blob-bearing row: %v", err)
		}
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	if blob == nil {
		// The JSON null: `props ? 'definition'` is true and the value is not a
		// declaration, which is the pair a value check gets wrong.
		if _, err := db.ExecContext(ctx, `
			UPDATE records SET props = jsonb_set(props, '{definition}', 'null')
			WHERE kind = $1 AND id = $2`, declKind, target.ID); err != nil {
			t.Fatalf("plant a null definition: %v", err)
		}
	}
	svc2 := open()
	defer func() { _ = svc2.Close() }()
	_, err = svc2.Dataset(ctx, testdb.Repository(t))
	if err == nil {
		t.Fatalf("%s: a store holding a `definition` must refuse the open", declKind)
	}
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("%s: expected ErrDeclarationUntranslated, got %v", declKind, err)
	}
	for _, want := range []string{target.ID, "definition"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: the refusal must name %q, got: %v", declKind, want, err)
		}
	}
	return true
}

// A REPOSITORY CARRYING A BUNDLE'S DECLARATIONS REFUSES THE ROW TOO, on the
// value a check that looked at types would miss: a JSON NULL under
// `definition`.
func TestStoreRefusesANullDefinitionAtTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "messaging")
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	const fn = "samples.substrate.reamde.dev/web/findurls"
	if _, err := db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{definition}', 'null')
		WHERE kind = $1 AND id = $2`,
		"substrate.reamde.dev/core/function", fn); err != nil {
		t.Fatalf("plant a null definition: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	_, err = svc2.Dataset(ctx, testdb.Repository(t))
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("a null definition must refuse by name, got %v", err)
	}
	for _, want := range []string{"definition", fn} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}

// A ROW WEARING A DELETED SPELLING REFUSES TOO (here the hoisted `emit`,
// before the grants grouped under `permissions:`). Nothing translates it, so
// what it must meet is an ACTIONABLE refusal at open, naming the row and the
// replacement, rather than a read that whitelists the key away and leaves a
// function that writes nothing.
func TestStoreRefusesADeletedGrantSpelling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "messaging")
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.Repository(t), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// The grant hoisted onto `data` itself, with no `permissions` object.
	const fn = "samples.substrate.reamde.dev/web/findurls"
	if _, err := db.ExecContext(ctx, `
		UPDATE records
		SET props = jsonb_set(props - 'permissions', '{emit}', $3::jsonb)
		WHERE kind = $1 AND id = $2`,
		"substrate.reamde.dev/core/function", fn,
		`["samples.substrate.reamde.dev/web/page"]`); err != nil {
		t.Fatalf("plant an interim grant row: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	_, err = svc2.Dataset(ctx, testdb.Repository(t))
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("an interim grant row must refuse by name, got %v", err)
	}
	for _, want := range []string{fn, "`emit`", "permissions.writes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}
