package changelogfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testRepoID is a repository id in the shape the engine mints.
const testRepoID = "rnd-01jbx9m2qz7k4c5v8n3w6t1hrd"

func sampleManifest(id, authority string) Manifest {
	return Manifest{
		Format: ManifestFormat, ID: id, Authority: authority,
		CreatedAt:         time.Date(2026, 9, 5, 10, 0, 0, 123456000, time.UTC),
		ChangelogDialect:  1,
		VocabularyDialect: 1,
		DEK:               []byte{0, 1, 2, 3, 250, 251, 252, 253},
		DEKKeyID:          "0123456789abcdef",
	}
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir, err := EnsureRepoDir(root, testRepoID)
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest(testRepoID, "ada.example.com")
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"format": 2`, `"id": "` + testRepoID + `"`, `"authority": "ada.example.com"`, `"createdAt": "2026-09-05T10:00:00.123456Z"`, `"dek": "AAECA/r7/P0="`, `"changelogDialect": 1`, `"vocabularyDialect": 1`, `"dekKeyId": "0123456789abcdef"`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("manifest lacks %s:\n%s", want, raw)
		}
	}
	// No `username` beside the name.
	if bytes.Contains(raw, []byte(`"username"`)) {
		t.Errorf("manifest carries a username key:\n%s", raw)
	}
	info, err := os.Stat(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != m.Format || got.ID != m.ID || got.Authority != m.Authority ||
		!got.CreatedAt.Equal(m.CreatedAt) || got.ChangelogDialect != m.ChangelogDialect ||
		got.VocabularyDialect != m.VocabularyDialect || !bytes.Equal(got.DEK, m.DEK) ||
		got.DEKKeyID != m.DEKKeyID {
		t.Fatalf("round trip: got %+v, want %+v", got, m)
	}
	// A rewrite replaces the file whole.
	m.ChangelogDialect = 2
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	got, err = ReadManifest(dir)
	if err != nil || got.ChangelogDialect != 2 {
		t.Fatalf("after rewrite: %+v, %v", got, err)
	}
	// No temporary file is left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

// A directory a release before ids wrote is named by the name, and its
// format-1 manifest carries no id: it reads, keyed by the authority, so the
// boot can convert it, and it writes back as it was.
func TestAFormatOneManifestIsKeyedByTheName(t *testing.T) {
	dir, err := EnsureRepoDir(t.TempDir(), "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(`{"format":1,"authority":"ada.example.com","createdAt":"","changelogDialect":1,"vocabularyDialect":1,"dek":null,"dekKeyId":""}`), fileMode); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != ManifestFormatBeforeIDs || m.ID != "" || m.Key() != "ada.example.com" {
		t.Fatalf("read %+v, want format 1 keyed by the name", m)
	}
	if err := WriteManifest(dir, m); err != nil {
		t.Fatalf("write a format-1 manifest back: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"id"`)) || !bytes.Contains(raw, []byte(`"format": 1`)) {
		t.Fatalf("written back as:\n%s", raw)
	}
}

// The boot's conversion writes the format-2 manifest into the directory while
// it is still named by the name, then renames it: between the two only the
// conversion's own reader takes it.
func TestAManifestBetweenItsRewriteAndItsRename(t *testing.T) {
	dir, err := EnsureRepoDir(t.TempDir(), "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest(testRepoID, "ada.example.com")
	if err := WriteManifest(dir, m); !errors.Is(err, ErrManifestAuthority) {
		t.Fatalf("WriteManifest put an id's manifest under the name: %v", err)
	}
	if err := WriteManifestToKey(dir, m); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(dir); !errors.Is(err, ErrManifestAuthority) {
		t.Fatalf("ReadManifest read an id's manifest under the name: %v", err)
	}
	got, err := ReadManifestToKey(dir)
	if err != nil || got.ID != testRepoID || got.Format != ManifestFormat {
		t.Fatalf("ReadManifestToKey: %+v, %v", got, err)
	}
	other := sampleManifest(testRepoID, "grace.example.com")
	if err := WriteManifestToKey(dir, other); !errors.Is(err, ErrManifestAuthority) {
		t.Fatalf("WriteManifestToKey wrote another repository's manifest: %v", err)
	}
}

func TestManifestMissingIsNotExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repositories", testRepoID)
	if _, err := ReadManifest(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestWriteManifestRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   error
	}{
		{"format 0", func(m *Manifest) { m.Format = 0 }, ErrManifestFormat},
		{"format 4", func(m *Manifest) { m.Format = 4 }, ErrManifestFormat},
		{"no authority", func(m *Manifest) { m.Authority = "" }, ErrManifestIncomplete},
		{"no id", func(m *Manifest) { m.ID = "" }, ErrManifestIncomplete},
		{"authority is not one", func(m *Manifest) { m.Authority = "a/b" }, ErrRepositoryAuthority},
		{"id is not one", func(m *Manifest) { m.ID = "k3j9x2m41pfq" }, ErrRepositoryKey},
		{"id names another directory", func(m *Manifest) { m.ID = "rnd-71jbx9m2qz7k4c5v8n3w6t1hrd" }, ErrManifestAuthority},
		{"format 1 under an id", func(m *Manifest) { m.Format, m.ID = ManifestFormatBeforeIDs, "" }, ErrManifestAuthority},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, err := EnsureRepoDir(t.TempDir(), testRepoID)
			if err != nil {
				t.Fatal(err)
			}
			m := sampleManifest(testRepoID, "ada.example.com")
			c.mutate(&m)
			if err := WriteManifest(dir, m); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if _, err := os.Stat(filepath.Join(dir, ManifestName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a refused manifest was written")
			}
		})
	}
}

func TestReadManifestRefusals(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"format 3", `{"format":3,"id":"` + testRepoID + `","authority":"ada.example.com"}`, ErrManifestFormat},
		{"no format", `{"id":"` + testRepoID + `","authority":"ada.example.com"}`, ErrManifestFormat},
		{"no authority", `{"format":2,"id":"` + testRepoID + `"}`, ErrManifestIncomplete},
		{"no id", `{"format":2,"authority":"ada.example.com"}`, ErrManifestIncomplete},
		{"format 1 with an id", `{"format":1,"id":"` + testRepoID + `","authority":"ada.example.com"}`, ErrManifestFormat},
		{"format 1 under an id", `{"format":1,"authority":"ada.example.com"}`, ErrManifestAuthority},
		{"the retired username key", `{"format":2,"id":"` + testRepoID + `","authority":"ada.example.com","username":"ada"}`, nil},
		{"wrong directory", `{"format":2,"id":"rnd-71jbx9m2qz7k4c5v8n3w6t1hrd","authority":"ada.example.com"}`, ErrManifestAuthority},
		{"unknown key", `{"format":2,"id":"` + testRepoID + `","authority":"ada.example.com","head":5}`, nil},
		{"bad time", `{"format":2,"id":"` + testRepoID + `","authority":"ada.example.com","createdAt":"yesterday"}`, nil},
		{"bad base64", `{"format":2,"id":"` + testRepoID + `","authority":"ada.example.com","dek":"!!"}`, nil},
		{"not JSON", `{`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), testRepoID)
			if err := os.MkdirAll(dir, dirMode); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(c.body), fileMode); err != nil {
				t.Fatal(err)
			}
			_, err := ReadManifest(dir)
			if err == nil {
				t.Fatal("read a manifest that must be refused")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestManifestJSONOmitsNothing(t *testing.T) {
	// Every key the format has is written even when zero, so a reader never
	// has to guess whether a missing key was unset or unknown to the writer.
	raw, err := json.Marshal(Manifest{Format: ManifestFormat})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":2,"id":"","authority":"","createdAt":"","changelogDialect":0,"vocabularyDialect":0,"dek":null,"dekKeyId":""}`
	if string(raw) != want {
		t.Fatalf("got %s\nwant %s", raw, want)
	}
}
