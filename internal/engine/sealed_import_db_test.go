package engine_test

// The import proves the manifest's DEK opens every sealed file before it
// creates a row (decision 0059): a copy holding a payload under another key
// is refused by file name, and the database is left without the row.

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// A copy whose sealed file does not open under the DEK its manifest carries
// is refused by file name, before any row is created.
func TestImportRefusesADirectoryWhoseSealedFilesDoNotOpen(t *testing.T) {
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
	if m, err := changelogfile.ReadManifest(dir2); err != nil || len(m.DEK) == 0 {
		t.Fatalf("the copied manifest carries no DEK: %+v, %v", m, err)
	}
	// The secret's file, rewritten under the host key: a payload the DEK the
	// manifest carries cannot open.
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
		t.Fatal("a directory whose file does not open under the DEK imported")
	}
	for _, want := range []string{id, "does not open", changelogfile.SealedFileName(ref)} {
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
