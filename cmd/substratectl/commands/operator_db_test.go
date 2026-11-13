package commands

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
)

// An operator command that opens as the writer restores the archive of a
// repository the database has no row of, as the server's start does, and
// reads SUBSTRATE_IMPORT_WITHOUT_SECRETS as the server reads it (decision
// 0264). Unlisted, an archive written under another host's key is refused,
// naming the variable, and nothing is restored; listed, with the server's
// spacing and empty entries, it is restored without its secrets and marked
// for its open to clear them.
func TestAnOperatorCommandReadsTheListingAsTheServerDoes(t *testing.T) {
	const name = "ada.example.com"
	srcRoot := t.TempDir()
	e := testenv.Start(t, testenv.WithDataRoot(srcRoot), testenv.WithCredentialKey(testenv.MintCredentialKey()),
		testenv.WithUser(name, "correct-horse-battery-staple"))
	e.MustJSON(http.MethodPut, "/api/v1/substrate.reamde.dev/llm/provider/openai", map[string]any{"properties": map[string]any{
		"label": "OpenAI", "wire": "openai", "baseURL": "https://llm.example.com/v1", "apiKey": "sk-left-behind",
	}}, nil)
	ds, err := e.Service.Dataset(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	id := ds.Repository().ID
	// The stop's last pass leaves the archive at the head.
	e.Stop()
	archived := filepath.Join("archive", changelogfile.RepositoriesDir, id)

	restore := func(t *testing.T, listing string) (string, string, error) {
		t.Helper()
		root := t.TempDir()
		if err := os.CopyFS(filepath.Join(root, archived), os.DirFS(filepath.Join(srcRoot, archived))); err != nil {
			t.Fatal(err)
		}
		dsn := testdb.NewSchema(t)
		h := newHarness(t)
		t.Setenv("SUBSTRATE_DATA_ROOT", root)
		t.Setenv("SUBSTRATE_CREDENTIAL_KEY", testenv.MintCredentialKey())
		t.Setenv("SUBSTRATE_IMPORT_WITHOUT_SECRETS", listing)
		_, _, err := h.run("--dsn", dsn, "archive", "report", name)
		return dsn, root, err
	}
	rows := func(t *testing.T, dsn string) (n int, marked bool) {
		t.Helper()
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		if err := db.QueryRow(`SELECT count(*), coalesce(bool_or(secrets_left_behind), false) FROM repositories WHERE id = $1`, id).Scan(&n, &marked); err != nil {
			t.Fatal(err)
		}
		return n, marked
	}

	t.Run("unlisted", func(t *testing.T) {
		dsn, _, err := restore(t, "other.example.com")
		if err == nil || !strings.Contains(err.Error(), engine.ImportWithoutSecretsVar) || !strings.Contains(err.Error(), name) {
			t.Fatalf("an unlisted restore under another key = %v, want refused naming %s and the repository", err, engine.ImportWithoutSecretsVar)
		}
		if n, _ := rows(t, dsn); n != 0 {
			t.Fatalf("the refused restore left %d row(s)", n)
		}
	})
	t.Run("listed", func(t *testing.T) {
		dsn, _, err := restore(t, " other.example.com, ,"+name+" ")
		if err != nil {
			t.Fatalf("a listed restore under another key: %v", err)
		}
		if n, marked := rows(t, dsn); n != 1 || !marked {
			t.Fatalf("the listed restore left %d row(s), marked %v; want the repository, marked for its open to clear what it left behind", n, marked)
		}
	})
}
