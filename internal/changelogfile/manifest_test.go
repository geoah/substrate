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

func sampleManifest(authority string) Manifest {
	return Manifest{
		Format: ManifestFormat, Authority: authority,
		CreatedAt:         time.Date(2026, 9, 5, 10, 0, 0, 123456000, time.UTC),
		ChangelogDialect:  1,
		VocabularyDialect: 1,
		DEK:               []byte{0, 1, 2, 3, 250, 251, 252, 253},
		DEKKeyID:          "0123456789abcdef",
	}
}

func TestManifestRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir, err := EnsureRepoDir(root, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	m := sampleManifest("ada.example.com")
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"format": 1`, `"authority": "ada.example.com"`, `"createdAt": "2026-09-05T10:00:00.123456Z"`, `"dek": "AAECA/r7/P0="`, `"changelogDialect": 1`, `"vocabularyDialect": 1`, `"dekKeyId": "0123456789abcdef"`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("manifest lacks %s:\n%s", want, raw)
		}
	}
	// The authority is the repository's one name: no second key for it, and
	// no `username` beside it.
	for _, gone := range []string{`"id"`, `"username"`} {
		if bytes.Contains(raw, []byte(gone)) {
			t.Errorf("manifest carries a %s key:\n%s", gone, raw)
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
	if got.Format != m.Format || got.Authority != m.Authority ||
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

func TestManifestMissingIsNotExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repositories", "ada.example.com")
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
		{"format 2", func(m *Manifest) { m.Format = 2 }, ErrManifestFormat},
		{"no authority", func(m *Manifest) { m.Authority = "" }, ErrManifestIncomplete},
		{"authority is not one", func(m *Manifest) { m.Authority = "a/b" }, ErrRepositoryAuthority},
		{"authority is the old id shape", func(m *Manifest) { m.Authority = "k3j9x2m41pfq" }, ErrRepositoryAuthority},
		{"authority names another directory", func(m *Manifest) { m.Authority = "other.example.com" }, ErrManifestAuthority},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, err := EnsureRepoDir(t.TempDir(), "ada.example.com")
			if err != nil {
				t.Fatal(err)
			}
			m := sampleManifest("ada.example.com")
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
		{"format 2", `{"format":2,"authority":"ada.example.com"}`, ErrManifestFormat},
		{"no format", `{"authority":"ada.example.com"}`, ErrManifestFormat},
		{"no authority", `{"format":1}`, ErrManifestIncomplete},
		{"the retired username key", `{"format":1,"authority":"ada.example.com","username":"ada"}`, nil},
		{"wrong directory", `{"format":1,"authority":"grace.example.com"}`, ErrManifestAuthority},
		{"the retired id key", `{"format":1,"id":"k3j9x2m41pfq","authority":"ada.example.com"}`, nil},
		{"unknown key", `{"format":1,"authority":"ada.example.com","head":5}`, nil},
		{"bad time", `{"format":1,"authority":"ada.example.com","createdAt":"yesterday"}`, nil},
		{"bad base64", `{"format":1,"authority":"ada.example.com","dek":"!!"}`, nil},
		{"not JSON", `{`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ada.example.com")
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
	raw, err := json.Marshal(Manifest{Format: ManifestFormat})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":1,"authority":"","createdAt":"","changelogDialect":0,"vocabularyDialect":0,"dek":null,"dekKeyId":""}`
	if string(raw) != want {
		t.Fatalf("got %s\nwant %s", raw, want)
	}
}
