package engine_test

// The stored-schema dialect gate, in both directions: repository open records
// the promotion and stamps the dialect; a binary whose maximum dialect is below
// the stored one refuses the open with the named error, which is a service
// condition and never an auth failure; and a store still holding dialect 1's
// `definition` blob refuses too, because no rung translates it any more.

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

func TestSchemaDialectLadder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
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

	// AND THE OTHER DIRECTION: a store STAMPED BELOW the maximum is a store
	// this binary cannot read. Dialect 2 spelled every kind
	// `{authority}/{name}`, and no rung can invent the package segment
	// (decision record 0047), so the open refuses instead of misreading the
	// rows, and it says what to do about it.
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`, dialect-1); err != nil {
		t.Fatalf("wind the stored dialect back: %v", err)
	}
	svc5 := open()
	defer func() { _ = svc5.Close() }()
	_, err = svc5.Dataset(ctx, "geoah")
	if err == nil {
		t.Fatal("a store stamped below the maximum must refuse the open")
	}
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("expected ErrDeclarationUntranslated, got %v", err)
	}
	if !strings.Contains(err.Error(), "no package segment") {
		t.Fatalf("the refusal must name what it cannot read: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE vocabulary_dialect SET dialect = $1`, dialect); err != nil {
		t.Fatalf("restore stored dialect: %v", err)
	}
}

// A DIALECT-1 STORE REFUSES THE OPEN, AND IS NOT STAMPED. The rung that
// translated a `definition` blob into typed properties was deleted before the
// first release (#217), so the gate is all that is left: it refuses BEFORE the
// stamp, because the stamp is durable and one-way and a store marked dialect 2
// over dialect-1 rows would carry a mark that lies about its own rows.
//
// THE BLOB IS DECIDED BY THE KEY'S PRESENCE, whatever the value is, and every
// value form is a case here. A gate that looked at the VALUE instead would wave
// a JSON null through, stamp the store dialect 2, and only then fail reading the
// row, which is exactly the half-stamped store the ordering above exists to make
// impossible. Every declaration kind is planted, because the refusal is not
// allowed to be one kind's business.
func TestSchemaDialectRefusesADefinitionBearingStore(t *testing.T) {
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
// declaration kind's row, winds the stamp back, and holds the reopen to the two
// things the gate promises: the named refusal, and no stamp. It reports whether
// it planted anything: a fresh repository holds no row of some kinds, and the
// caller checks the coverage rather than this returning early in silence. It
// must NOT call t.Skip, which would end the whole subtest and every kind after
// this one with it.
func assertDefinitionBlobRefusesTheOpen(t *testing.T, declKind string, blob any) bool {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	// One declaration row put back into dialect 1's shape. No live write path
	// produces this, which is what the planting seam is for; a jsonb null is not
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
			t.Fatalf("plant the dialect-1 row: %v", err)
		}
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
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
		t.Fatalf("%s: a store holding a `definition` must refuse the open", declKind)
	}
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("%s: expected ErrDeclarationUntranslated, got %v", declKind, err)
	}
	for _, want := range []string{target.ID, "definition", "wipe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: the refusal must name %q, got: %v", declKind, want, err)
		}
	}
	// AND IT DID NOT STAMP. A mark left behind here would say the rows are typed
	// and make the very next open return early at the maximum, so the store would
	// be permanently half-migrated: stamped dialect 2 over a dialect-1 row.
	var stamped int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM vocabulary_dialect`).Scan(&stamped); err != nil {
		t.Fatalf("read the stamp: %v", err)
	}
	if stamped != 0 {
		t.Fatalf("%s: the refused open stamped the dialect anyway (%d rows)", declKind, stamped)
	}
	return true
}

// A STAMPED STORE REFUSES THE ROW TOO. The gate returns early at the binary's
// maximum, so a `definition` that appears on an already-migrated store never
// meets it: rowDocument is what refuses there, and the two refusals have to
// agree or a blob would be read as a declaration by whichever path saw it
// second. The value is a JSON NULL, the narrowest case a value-typed check
// misses.
func TestStampedStoreRefusesANullDefinitionAtTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds)
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// The store is stamped dialect 2 (a fresh repository is typed from birth), and
	// the stamp is left alone: this row meets the live reader, not the gate.
	const fn = "samples.substrate.reamde.dev/web/findurls"
	if _, err := db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{definition}', 'null')
		WHERE kind = $1 AND id = $2`,
		"substrate.reamde.dev/core/function", fn); err != nil {
		t.Fatalf("plant a null definition: %v", err)
	}
	_ = svc.Close()

	svc2 := open()
	_, err = svc2.Dataset(ctx, "geoah")
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("a null definition must refuse by name, got %v", err)
	}
	for _, want := range []string{"definition", fn} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}

// A TYPED ROW WEARING AN INTERIM SPELLING REFUSES TOO, and this is the posture
// on the one store nothing migrates: stamped dialect 2, with a row carrying a
// spelling only an unreleased binary ever wrote (here the hoisted `emit`, before
// the grants grouped under `permissions:`). There is no rung for it and there
// will not be one, so what it must meet is an ACTIONABLE refusal at open, naming
// the row and the replacement, rather than a read that whitelists the key away
// and leaves a function that writes nothing.
func TestStampedStoreRefusesAnInterimGrantRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = svc.Close() })
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds)
	installShippedBundle(t, ds, "web")
	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// The row as the flip's own unreleased binary left it: the grant hoisted onto
	// `data` itself, with no `permissions` object.
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
	_, err = svc2.Dataset(ctx, "geoah")
	if !errors.Is(err, engine.ErrDeclarationUntranslated) {
		t.Fatalf("an interim grant row must refuse by name, got %v", err)
	}
	for _, want := range []string{fn, "`emit`", "permissions.writes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got: %v", want, err)
		}
	}
}
