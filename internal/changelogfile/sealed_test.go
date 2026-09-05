package changelogfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSealedRoundTrip(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repositories", "abc")
	if err := os.MkdirAll(repoDir, dirMode); err != nil {
		t.Fatal(err)
	}
	// The sealed directory does not exist yet: WriteSealed creates it.
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	recs := []SealedRecord{
		{Ref: "secret:9f8e7d6c", RecordKind: "ada.example.com/account", RecordID: "r1", Payload: []byte("cipher-1"), UpdatedAt: time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)},
		{Ref: "auth:password:00ff", RecordKind: "substrate.reamde.dev/core/credential", RecordID: "self", Payload: []byte("cipher-2"), UpdatedAt: time.Date(2026, 9, 5, 11, 0, 0, 0, time.UTC)},
		{Ref: "kq3v9x2m41pf", RecordKind: "ada.example.com/account", RecordID: "r2", Payload: []byte("cipher-3"), ExpiresAt: &exp, UpdatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)},
		{Ref: "secret:erased", RecordKind: "ada.example.com/account", RecordID: "r3", Payload: []byte("x"), UpdatedAt: time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)},
	}
	for _, r := range recs {
		if err := WriteSealed(repoDir, r); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"secret-9f8e7d6c.json", "auth-password-00ff.json", "kq3v9x2m41pf.json", "secret-erased.json"} {
		info, err := os.Stat(filepath.Join(SealedDir(repoDir), name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Fatalf("%s mode = %v", name, info.Mode().Perm())
		}
	}
	got, err := ReadSealed(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"auth:password:00ff", "kq3v9x2m41pf", "secret:9f8e7d6c", "secret:erased"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d records", len(got))
	}
	for i, r := range got {
		if r.Ref != wantOrder[i] {
			t.Fatalf("order = %v", refsOf(got))
		}
	}
	if !bytes.Equal(got[1].Payload, []byte("cipher-3")) || got[1].ExpiresAt == nil || !got[1].ExpiresAt.Equal(exp) || !got[1].UpdatedAt.Equal(recs[2].UpdatedAt) {
		t.Fatalf("record = %+v", got[1])
	}
	if got[0].ExpiresAt != nil {
		t.Fatal("an absent expiresAt came back set")
	}
	// A rewrite under the same ref replaces the file.
	recs[0].Payload = []byte("cipher-1b")
	if err := WriteSealed(repoDir, recs[0]); err != nil {
		t.Fatal(err)
	}
	got, err = ReadSealed(repoDir)
	if err != nil || len(got) != 4 || !bytes.Equal(got[2].Payload, []byte("cipher-1b")) {
		t.Fatalf("after rewrite: %v, %v", refsOf(got), err)
	}
	// Delete is idempotent.
	for range 2 {
		if err := DeleteSealed(repoDir, "secret:9f8e7d6c"); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteSealed(repoDir, "secret:neverthere"); err != nil {
		t.Fatal(err)
	}
	got, err = ReadSealed(repoDir)
	if err != nil || len(got) != 3 {
		t.Fatalf("after delete: %v, %v", refsOf(got), err)
	}
}

func refsOf(recs []SealedRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Ref)
	}
	return out
}

func TestReadSealedEmpty(t *testing.T) {
	repoDir := t.TempDir()
	got, err := ReadSealed(repoDir)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing dir: %v, %v", got, err)
	}
	if err := os.MkdirAll(SealedDir(repoDir), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSealed(repoDir, "secret:abc"); err != nil {
		t.Fatal(err)
	}
	// Half-written and unrelated files are ignored.
	for _, name := range []string{tmpPrefix + "123", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(SealedDir(repoDir), name), []byte("{"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	got, err = ReadSealed(repoDir)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty dir: %v, %v", got, err)
	}
}

func TestReadSealedRefusesABadFile(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
		want error
	}{
		{"renamed file", "secret-other.json", `{"ref":"secret:abc","recordKind":"k","recordId":"r","payload":"eA==","updatedAt":"2026-09-05T10:00:00Z"}`, ErrSealedName},
		{"colon kept in the name", "secret:abc.json", `{"ref":"secret:abc","recordKind":"k","recordId":"r","payload":"eA==","updatedAt":"2026-09-05T10:00:00Z"}`, ErrSealedName},
		{"ref outside the grammar", "bad.json", `{"ref":"../x","recordKind":"k","recordId":"r","payload":"eA==","updatedAt":"2026-09-05T10:00:00Z"}`, ErrSealedRef},
		{"no ref", "x.json", `{"recordKind":"k","recordId":"r","payload":"eA==","updatedAt":"2026-09-05T10:00:00Z"}`, ErrSealedRef},
		{"unknown key", "secret-abc.json", `{"ref":"secret:abc","recordKind":"k","recordId":"r","payload":"eA==","updatedAt":"2026-09-05T10:00:00Z","plaintext":"no"}`, nil},
		{"not JSON", "secret-abc.json", `{`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoDir := t.TempDir()
			if err := os.MkdirAll(SealedDir(repoDir), dirMode); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(SealedDir(repoDir), c.file), []byte(c.body), fileMode); err != nil {
				t.Fatal(err)
			}
			_, err := ReadSealed(repoDir)
			if err == nil {
				t.Fatal("read a file that must be refused")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !strings.Contains(err.Error(), c.file) {
				t.Fatalf("err = %v, want it to name %s", err, c.file)
			}
		})
	}
}

func TestSealedRefGrammar(t *testing.T) {
	good := []string{"secret:9f8e7d6c", "secret:erased", "auth:password:00ff", "auth:totp:00ff", "kq3v9x2m41pf", "a.b-c_d"}
	for _, ref := range good {
		if err := checkSealedRef(ref); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	bad := []string{
		"", ":", "secret:", ":abc", "a::b",
		"secret/abc", "../abc", `secret\abc`, "secret:a/b",
		".hidden", ".", "..", tmpPrefix + "x",
		"secret abc", "secret:ab\ncd", "sécret:abc",
		"secret:" + strings.Repeat("a", 300),
	}
	for _, ref := range bad {
		err := checkSealedRef(ref)
		if !errors.Is(err, ErrSealedRef) {
			t.Errorf("%q: err = %v, want ErrSealedRef", ref, err)
		}
		if err := WriteSealed(t.TempDir(), SealedRecord{Ref: ref, Payload: []byte("x")}); !errors.Is(err, ErrSealedRef) {
			t.Errorf("WriteSealed(%q): err = %v", ref, err)
		}
		if err := DeleteSealed(t.TempDir(), ref); !errors.Is(err, ErrSealedRef) {
			t.Errorf("DeleteSealed(%q): err = %v", ref, err)
		}
	}
}
