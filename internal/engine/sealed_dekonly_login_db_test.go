package engine_test

// The first login of a repository from before DEKs re-seals the TOTP row on
// the maintenance pool, under the host key, BEFORE the open that follows
// adopts a DEK, re-keys the store and marks the repository (decision 0059).
// The mirror after that open must carry the re-keyed bytes, not the ones the
// login wrote: a directory holding a host-key payload under a manifest that
// says sealedDekOnly would import as a marked repository whose TOTP check is
// refused for good.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

func TestFirstLoginOfAPreDEKRepositoryMirrorsTheReKeyedStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	enrollment, err := svc.BeginRegistration(ctx, "eve.example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{repository: "eve.example.com", password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "eve.example.com", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	const id = "eve.example.com"
	db := rawDB(t, dsn)
	hostKey := engine.TestCredentialKeyBytes

	// Rewind to what a pre-DEK release left: every sealed row under the host
	// key, no DEK on the row, no key id, no marker.
	var wrapped []byte
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = $1`, id).Scan(&wrapped); err != nil {
		t.Fatal(err)
	}
	dek, err := engine.OpenPayloadWithKey(hostKey, wrapped, engine.DEKAAD(id))
	if err != nil {
		t.Fatalf("unwrap the DEK: %v", err)
	}
	type sealedRow struct {
		ref, kind, rid string
		payload        []byte
	}
	var sealedRows []sealedRow
	rows, err := db.Query(`SELECT ref, record_kind, record_id, payload FROM sealed WHERE repository = $1`, id)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r sealedRow
		if err := rows.Scan(&r.ref, &r.kind, &r.rid, &r.payload); err != nil {
			t.Fatal(err)
		}
		sealedRows = append(sealedRows, r)
	}
	_ = rows.Close()
	if len(sealedRows) == 0 {
		t.Fatal("registration sealed nothing")
	}
	for _, r := range sealedRows {
		aad := engine.SealedAAD(r.ref, r.kind, r.rid)
		raw, err := engine.OpenPayloadWithKey(dek, r.payload, aad)
		if err != nil {
			t.Fatalf("open %s under the DEK: %v", r.ref, err)
		}
		hostSealed, err := engine.SealWithKey(hostKey, raw, aad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE sealed SET payload = $1 WHERE repository = $2 AND ref = $3`, hostSealed, id, r.ref); err != nil {
			t.Fatalf("plant host-key payload at %s: %v", r.ref, err)
		}
	}
	if _, err := db.Exec(`UPDATE repositories SET dek = NULL, dek_key_id = NULL, sealed_dek_only = false WHERE id = $1`, id); err != nil {
		t.Fatalf("rewind the row: %v", err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	svc = mustReopen(t, dsn, root)

	// The first login: the factors open under the fallback, the TOTP step is
	// sealed under the host key, and the open that follows adopts a DEK,
	// re-keys the store and marks the repository.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: "eve.example.com", Password: testPassword, TOTPCode: u.code(t), Label: "first",
	}); err != nil {
		t.Fatalf("first login: %v", err)
	}

	// Every sealed file, the TOTP row included, now opens under the adopted
	// DEK alone, under a manifest that says so.
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = $1`, id).Scan(&wrapped); err != nil {
		t.Fatal(err)
	}
	adopted, err := engine.OpenPayloadWithKey(hostKey, wrapped, engine.DEKAAD(id))
	if err != nil {
		t.Fatalf("unwrap the adopted DEK: %v", err)
	}
	dir, err := changelogfile.RepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	files, err := changelogfile.ReadSealed(dir)
	if err != nil || len(files) != len(sealedRows) {
		t.Fatalf("sealed files after the first login: %d of %d rows, %v", len(files), len(sealedRows), err)
	}
	for _, f := range files {
		if _, err := engine.OpenPayloadWithKey(adopted, f.Payload, engine.SealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			t.Fatalf("sealed/%s is not under the DEK after the first login: %v", f.Ref, err)
		}
	}
	if m, err := changelogfile.ReadManifest(dir); err != nil || !m.SealedDEKOnly {
		t.Fatalf("the manifest after the first login: %+v, %v", m, err)
	}

	// A copy taken now imports as a marked repository, and its login works.
	_ = svc.Close()
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, engine.MigratedDSN(t), root2)
	waitStep(t)
	if _, _, err := svc2.Login(ctx, substrate.LoginInput{
		Repository: "eve.example.com", Password: testPassword, TOTPCode: u.code(t), Label: "after import",
	}); err != nil {
		t.Fatalf("login on the imported copy: %v", err)
	}
}

// A manifest that says sealedDekOnly is a claim about the files, and the
// import proves it before creating a row that would refuse the legacy forms
// for good: a copy whose sealed file does not open under the DEK is refused
// by file name, and the database is left without the row.
func TestImportRefusesAMarkedDirectoryWhoseFilesDoNotOpen(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	const kind = "substrate.reamde.dev/core/llmprovider"
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "prov",
		Properties: map[string]any{
			"label": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": "sk-marked",
		},
	})
	ref := secretRefOf(t, dsn, kind, "prov", "apiKey")
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	m, err := changelogfile.ReadManifest(dir2)
	if err != nil || !m.SealedDEKOnly {
		t.Fatalf("the copied manifest is not marked: %+v, %v", m, err)
	}
	// The secret's file, rewritten under the host key: what a pre-DEK release
	// wrote, under a manifest that promises otherwise.
	files, err := changelogfile.ReadSealed(dir2)
	if err != nil {
		t.Fatal(err)
	}
	var rec changelogfile.SealedRecord
	for _, f := range files {
		if f.Ref == ref {
			rec = f
		}
	}
	if rec.Ref == "" {
		t.Fatalf("no sealed file for %s in the copy", ref)
	}
	if rec.Payload, err = engine.SealWithKey(engine.TestCredentialKeyBytes, []byte("sk-marked"),
		engine.SealedAAD(ref, rec.RecordKind, rec.RecordID)); err != nil {
		t.Fatal(err)
	}
	if err := changelogfile.WriteSealed(dir2, rec); err != nil {
		t.Fatal(err)
	}

	dsn2 := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn2, root2, engine.TestCredentialKey)
	if err == nil {
		t.Fatal("a marked directory whose file does not open under the DEK imported")
	}
	for _, want := range []string{id, "sealedDekOnly", changelogfile.SealedFileName(ref)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	var rows int
	if err := rawDB(t, dsn2).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused import left %d row(s)", rows)
	}
}
