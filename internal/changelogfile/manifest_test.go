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
		Format: ManifestFormat, Username: "ada", Authority: authority,
		CreatedAt:         time.Date(2026, 9, 5, 10, 0, 0, 123456000, time.UTC),
		ChangelogDialect:  3,
		VocabularyDialect: 3,
		DEK:               []byte{0, 1, 2, 3, 250, 251, 252, 253},
		DEKKeyID:          "0123456789abcdef",
		SealedDEKOnly:     true,
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
	for _, want := range []string{`"format": 2`, `"authority": "ada.example.com"`, `"createdAt": "2026-09-05T10:00:00.123456Z"`, `"dek": "AAECA/r7/P0="`, `"changelogDialect": 3`, `"vocabularyDialect": 3`, `"dekKeyId": "0123456789abcdef"`, `"sealedDekOnly": true`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("manifest lacks %s:\n%s", want, raw)
		}
	}
	// The authority is the id: there is no second key for it.
	if bytes.Contains(raw, []byte(`"id"`)) {
		t.Errorf("manifest carries an id key:\n%s", raw)
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
	if got.Format != m.Format || got.Username != m.Username || got.Authority != m.Authority ||
		!got.CreatedAt.Equal(m.CreatedAt) || got.ChangelogDialect != m.ChangelogDialect ||
		got.VocabularyDialect != m.VocabularyDialect || !bytes.Equal(got.DEK, m.DEK) ||
		got.DEKKeyID != m.DEKKeyID || got.SealedDEKOnly != m.SealedDEKOnly {
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
	dir := filepath.Join(t.TempDir(), "repositories", "ada.example.com")
	if _, err := ReadManifest(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

// A format-1 manifest, the shape v0.47.0 through v0.53.0 wrote, reads with its
// format kept and no vocabulary dialect, because it recorded none; `dekKeyId`
// and `sealedDekOnly` (v0.53.0) read when present and as an unnamed key and
// an unmarked store when absent; the format-2 key in a format-1 document is
// unknown and refused.
func TestManifestReadsFormatOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ada.example.com")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	example := `{"format": 1,
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
	if m.Format != 1 || m.Authority != "ada.example.com" || m.Username != "ada" || m.ChangelogDialect != 2 || m.VocabularyDialect != 0 ||
		!m.CreatedAt.Equal(time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)) || !bytes.Equal(m.DEK, []byte{0, 1, 2, 3}) ||
		m.DEKKeyID != "" || m.SealedDEKOnly {
		t.Fatalf("manifest = %+v", m)
	}
	withMarker := strings.Replace(example, `"changelogDialect": 2,`, `"changelogDialect": 2, "dekKeyId": "0123456789abcdef", "sealedDekOnly": true,`, 1)
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(withMarker), fileMode); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadManifest(dir); err != nil || got.Format != 1 || got.DEKKeyID != "0123456789abcdef" || !got.SealedDEKOnly {
		t.Fatalf("a v0.53.0 format-1 manifest: %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(example), fileMode); err != nil {
		t.Fatal(err)
	}
	// What was read in format 1 is not written back as it is: the writer
	// writes ManifestFormat alone, and the caller fills what format 1 lacked.
	if err := WriteManifest(dir, m); !errors.Is(err, ErrManifestFormat) {
		t.Fatalf("writing a format-1 manifest: err = %v, want ErrManifestFormat", err)
	}
	m.Format, m.VocabularyDialect = ManifestFormat, 3
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifest(dir)
	if err != nil || got.Format != ManifestFormat || got.VocabularyDialect != 3 || got.ChangelogDialect != 2 {
		t.Fatalf("after the rewrite: %+v, %v", got, err)
	}
	withKey := strings.Replace(example, `"changelogDialect": 2,`, `"changelogDialect": 2, "vocabularyDialect": 3,`, 1)
	if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(withKey), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), `"vocabularyDialect"`) {
		t.Fatalf("a format-1 manifest carrying vocabularyDialect: err = %v", err)
	}
}

// The manifest a pre-authority binary wrote carries `id`, which ReadManifest
// refuses as an unknown key and ReadLegacyManifest reads, holding the id to the
// directory name and the authority to the grammar the directory will be
// renamed to.
func TestLegacyManifest(t *testing.T) {
	legacy := `{"format": 1,
 "id": "k3j9x2m41pfq",
 "username": "ada",
 "authority": "ada.example.com",
 "createdAt": "2026-09-05T10:00:00.000000Z",
 "changelogDialect": 2,
 "dek": "AAECAw=="}`
	write := func(t *testing.T, dirName, body string) string {
		dir := filepath.Join(t.TempDir(), RepositoriesDir, dirName)
		if err := os.MkdirAll(dir, dirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ManifestName), []byte(body), fileMode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dir := write(t, "k3j9x2m41pfq", legacy)
	if _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), `"id"`) {
		t.Fatalf("ReadManifest read a manifest carrying id: err = %v", err)
	}
	lm, err := ReadLegacyManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if lm.ID != "k3j9x2m41pfq" || lm.Manifest.Authority != "ada.example.com" || lm.Manifest.Username != "ada" ||
		lm.Manifest.ChangelogDialect != 2 || !bytes.Equal(lm.Manifest.DEK, []byte{0, 1, 2, 3}) ||
		!lm.Manifest.CreatedAt.Equal(time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy manifest = %+v", lm)
	}
	// The read manifest is format 1, so it is not written back as it is; minus
	// the id and brought up to ManifestFormat, it is one WriteManifest accepts
	// once the directory is renamed.
	renamed, err := RenameRepoDir(filepath.Dir(filepath.Dir(dir)), "k3j9x2m41pfq", "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if lm.Manifest.Format != 1 {
		t.Fatalf("a legacy manifest reads as format %d", lm.Manifest.Format)
	}
	if err := WriteManifest(renamed, lm.Manifest); !errors.Is(err, ErrManifestFormat) {
		t.Fatalf("writing the legacy manifest as read: err = %v, want ErrManifestFormat", err)
	}
	m := lm.Manifest
	m.Format, m.VocabularyDialect = ManifestFormat, 3
	if err := WriteManifest(renamed, m); err != nil {
		t.Fatalf("write the new manifest: %v", err)
	}
	if got, err := ReadManifest(renamed); err != nil || got.Authority != "ada.example.com" || got.Format != ManifestFormat {
		t.Fatalf("after the rename: %+v, %v", got, err)
	}

	refusals := []struct {
		name, dir, body string
		want            error
	}{
		{"no id", "k3j9x2m41pfq", `{"format":1,"username":"ada","authority":"ada.example.com"}`, ErrManifestIncomplete},
		{"authority is not one", "k3j9x2m41pfq", `{"format":1,"id":"k3j9x2m41pfq","username":"ada","authority":"nodot"}`, ErrRepositoryAuthority},
		{"format 2", "k3j9x2m41pfq", `{"format":2,"id":"k3j9x2m41pfq","username":"ada","authority":"ada.example.com"}`, ErrManifestFormat},
		{"unknown key", "k3j9x2m41pfq", `{"format":1,"id":"k3j9x2m41pfq","username":"ada","authority":"ada.example.com","head":5}`, nil},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			dir := write(t, c.dir, c.body)
			_, err := ReadLegacyManifest(dir)
			if err == nil {
				t.Fatal("read a legacy manifest that must be refused")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestWriteManifestRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   error
	}{
		{"format 0", func(m *Manifest) { m.Format = 0 }, ErrManifestFormat},
		{"format 1 is read, never written", func(m *Manifest) { m.Format = 1 }, ErrManifestFormat},
		{"format 3", func(m *Manifest) { m.Format = 3 }, ErrManifestFormat},
		{"no authority", func(m *Manifest) { m.Authority = "" }, ErrManifestIncomplete},
		{"no username", func(m *Manifest) { m.Username = "" }, ErrManifestIncomplete},
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
		{"format 3", `{"format":3,"authority":"ada.example.com","username":"ada"}`, ErrManifestFormat},
		{"no format", `{"authority":"ada.example.com","username":"ada"}`, ErrManifestFormat},
		{"no authority", `{"format":2,"username":"ada"}`, ErrManifestIncomplete},
		{"no username", `{"format":2,"authority":"ada.example.com"}`, ErrManifestIncomplete},
		{"format 1 with no authority", `{"format":1,"username":"ada"}`, ErrManifestIncomplete},
		{"wrong directory", `{"format":2,"authority":"grace.example.com","username":"ada"}`, ErrManifestAuthority},
		{"the pre-authority id key", `{"format":1,"id":"k3j9x2m41pfq","authority":"ada.example.com","username":"ada"}`, nil},
		{"the pre-authority id key in format 2", `{"format":2,"id":"k3j9x2m41pfq","authority":"ada.example.com","username":"ada"}`, nil},
		{"unknown key", `{"format":2,"authority":"ada.example.com","username":"ada","head":5}`, nil},
		{"format 1 with the format-2 key", `{"format":1,"authority":"ada.example.com","username":"ada","vocabularyDialect":3}`, nil},
		{"bad time", `{"format":2,"authority":"ada.example.com","username":"ada","createdAt":"yesterday"}`, nil},
		{"bad base64", `{"format":2,"authority":"ada.example.com","username":"ada","dek":"!!"}`, nil},
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
	raw, err := json.Marshal(Manifest{Format: ManifestFormat, Username: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"format":2,"username":"ada","authority":"","createdAt":"","changelogDialect":0,"vocabularyDialect":0,"dek":null,"dekKeyId":"","sealedDekOnly":false}`
	if string(raw) != want {
		t.Fatalf("got %s\nwant %s", raw, want)
	}
}
