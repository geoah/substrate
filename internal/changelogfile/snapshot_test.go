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

func sampleSnapshot() Snapshot {
	var sum [32]byte
	for i := range sum {
		sum[i] = byte(i)
	}
	return Snapshot{
		Format: SnapshotFormat, TakenAt: time.Date(2026, 9, 8, 10, 0, 0, 123456000, time.UTC),
		Head: 42, HeadHash: sum, SealedFiles: 3,
		BlobStore: "s3", BlobLocation: "s3://bucket/prefix/ada.example.com/",
		Blobs: []string{"blob-sha256-" + strings.Repeat("a", 64)},
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir, err := EnsureRepoDir(root, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a directory with no snapshot must read as not-exist, got %v", err)
	}
	s := sampleSnapshot()
	if err := WriteSnapshot(dir, s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, SnapshotName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"format": 1`, `"takenAt": "2026-09-08T10:00:00.123456Z"`, `"head": 42`,
		`"headHash": "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"`, `"sealedFiles": 3`,
		`"blobStore": "s3"`, `"blobLocation": "s3://bucket/prefix/ada.example.com/"`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("snapshot lacks %s:\n%s", want, raw)
		}
	}
	got, err := ReadSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != s.Head || got.HeadHash != s.HeadHash || !got.TakenAt.Equal(s.TakenAt) || got.SealedFiles != 3 ||
		got.BlobStore != "s3" || got.BlobLocation != s.BlobLocation || len(got.Blobs) != 1 || got.Blobs[0] != s.Blobs[0] {
		t.Fatalf("round trip changed the snapshot:\n%+v\n%+v", s, got)
	}
}

// An empty blob list is written as a list, so a reader never meets null; a
// snapshot in another format, or with a key this format does not have, is
// refused rather than read partially.
func TestSnapshotRefusesAnotherFormatAndUnknownKeys(t *testing.T) {
	root := t.TempDir()
	dir, err := EnsureRepoDir(root, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := sampleSnapshot()
	s.Blobs = nil
	if err := WriteSnapshot(dir, s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, SnapshotName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"blobs": []`)) {
		t.Fatalf("an empty blob list must be written as []:\n%s", raw)
	}
	s.Format = 2
	if err := WriteSnapshot(dir, s); !errors.Is(err, ErrSnapshotFormat) {
		t.Fatalf("writing format 2 must be refused, got %v", err)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, SnapshotName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(strings.Replace(string(raw), `"format": 1`, `"format": 2`, 1))
	if _, err := ReadSnapshot(dir); !errors.Is(err, ErrSnapshotFormat) {
		t.Fatalf("format 2 must be refused, got %v", err)
	}
	write(strings.Replace(string(raw), `"head": 42`, `"head": 42, "generation": "x"`, 1))
	if _, err := ReadSnapshot(dir); err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("an unknown key must be refused by name, got %v", err)
	}
}
