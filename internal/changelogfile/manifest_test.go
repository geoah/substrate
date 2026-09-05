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

func sampleManifest(id string) Manifest {
	return Manifest{
		Format: 1, ID: id, Username: "ada", Authority: "ada.example.com",
		CreatedAt:        time.Date(2026, 9, 5, 10, 0, 0, 123456000, time.UTC),
		ChangelogDialect: 2,
		DEK:              []byte{0, 1, 2, 3, 250, 251, 252, 253},
	}
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir, err := EnsureRepoDir(root, "k3j9x2m41pfq")
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest("k3j9x2m41pfq")
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"format": 1`, `"id": "k3j9x2m41pfq"`, `"createdAt": "2026-09-05T10:00:00.123456Z"`, `"dek": "AAECA/r7/P0="`, `"changelogDialect": 2`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("manifest lacks %s:\n%s", want, raw)
		}
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
	if got.Format != m.Format || got.ID != m.ID || got.Username != m.Username || got.Authority != m.Authority ||
		!got.CreatedAt.Equal(m.CreatedAt) || got.ChangelogDialect != m.ChangelogDialect || !bytes.Equal(got.DEK, m.DEK) {
		t.Fatalf("round trip: got %+v, want %+v", got, m)
	}
	// A rewrite replaces the file whole.
	m.Username = "grace"
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	got, err = ReadManifest(dir)
	if err != nil || got.Username != "grace" {
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

func TestManifestMissingIsNotExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repositories", "abc")
	if _, err := ReadManifest(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestManifestReadsThePlanExample(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "k3j9x2m41pfq")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	example := `{"format": 1,
 "id": "k3j9x2m41pfq",
 "username": "ada",
 "authority": "ada.example.com",
 "createdAt": "2026-09-05T10:00:00.000000Z",
 "changelogDialect": 2,
 "dek": "AAECAw=="}`
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(example), fileMode); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "k3j9x2m41pfq" || m.Username != "ada" || !m.CreatedAt.Equal(time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)) || !bytes.Equal(m.DEK, []byte{0, 1, 2, 3}) {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestWriteManifestRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   error
	}{
		{"format 0", func(m *Manifest) { m.Format = 0 }, ErrManifestFormat},
		{"format 2", func(m *Manifest) { m.Format = 2 }, ErrManifestFormat},
		{"no id", func(m *Manifest) { m.ID = "" }, ErrManifestIncomplete},
		{"no username", func(m *Manifest) { m.Username = "" }, ErrManifestIncomplete},
		{"id is not a repository id", func(m *Manifest) { m.ID = "a/b" }, ErrRepositoryID},
		{"id names another directory", func(m *Manifest) { m.ID = "other" }, ErrManifestID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, err := EnsureRepoDir(t.TempDir(), "k3j9x2m41pfq")
			if err != nil {
				t.Fatal(err)
			}
			m := sampleManifest("k3j9x2m41pfq")
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
		{"format 2", `{"format":2,"id":"abc","username":"ada"}`, ErrManifestFormat},
		{"no format", `{"id":"abc","username":"ada"}`, ErrManifestFormat},
		{"no id", `{"format":1,"username":"ada"}`, ErrManifestIncomplete},
		{"no username", `{"format":1,"id":"abc"}`, ErrManifestIncomplete},
		{"wrong directory", `{"format":1,"id":"xyz","username":"ada"}`, ErrManifestID},
		{"unknown key", `{"format":1,"id":"abc","username":"ada","head":5}`, nil},
		{"bad time", `{"format":1,"id":"abc","username":"ada","createdAt":"yesterday"}`, nil},
		{"bad base64", `{"format":1,"id":"abc","username":"ada","dek":"!!"}`, nil},
		{"not JSON", `{`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "abc")
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
	// Every key is written even when zero, so a reader never has to guess
	// whether a missing key was unset or unknown to the writer.
	raw, err := json.Marshal(Manifest{Format: 1, ID: "abc", Username: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":1,"id":"abc","username":"ada","authority":"","createdAt":"","changelogDialect":0,"dek":null}`
	if string(raw) != want {
		t.Fatalf("got %s\nwant %s", raw, want)
	}
}
