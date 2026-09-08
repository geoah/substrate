package commands

import (
	"archive/tar"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// handleExport streams the seeded archive under the seeded file name, the
// way the server does: `application/x-tar`, an attachment.
func (f *fakeSubstrate) handleExport(w http.ResponseWriter, r *http.Request) {
	f.noteRequest(r)
	w.Header().Set("Content-Type", "application/x-tar")
	if f.exportName != "" {
		w.Header().Set("Content-Disposition", `attachment; filename=`+f.exportName)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.exportTar)
}

// exportArchive builds an archive in the server's layout: the manifest, one
// finished segment with its sidecar, the active one, a sealed file, a blob
// and, when complete is set, snapshot.json last.
func exportArchive(t *testing.T, complete bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	root := "repositories/ada.example.com/"
	write := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: root + name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"repositories/", root, root + "changelog/", root + "blobs/", root + "sealed/"} {
		if err := tw.WriteHeader(&tar.Header{Name: dir, Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	write("repository.json", `{"format":2,"username":"ada","authority":"ada.example.com"}`+"\n")
	write("changelog/000000000000001.ndjson", "{}\n{}\n")
	write("changelog/000000000000001.ndjson.sha256", strings.Repeat("0", 64)+"\n")
	write("changelog/000000000000003.ndjson", "{}\n")
	write("sealed/secret-abc.json", `{"ref":"secret:abc"}`+"\n")
	write("blobs/blob-sha256-"+strings.Repeat("a", 64), "attachment bytes")
	if complete {
		write("snapshot.json", `{"format":1,"takenAt":"2026-09-08T10:00:00Z","head":42,"headHash":"`+
			strings.Repeat("ab", 32)+`","sealedFiles":1,"blobStore":"fs","blobLocation":"","blobs":["blob-sha256-`+strings.Repeat("a", 64)+`"]}`+"\n")
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The archive lands byte for byte under the server's file name, the request
// carried the bearer token and nothing else, and the report reads the point
// off snapshot.json.
func TestExportWritesTheArchiveAndPrintsThePoint(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.exportTar = exportArchive(t, true)
	h.fake.exportName = "ada.example.com-42.tar"
	dir := t.TempDir()
	t.Chdir(dir)

	out, _ := h.mustRun("export")
	if h.lastRequest() != "GET /api/v1/export" {
		t.Fatalf("request = %q", h.lastRequest())
	}
	if h.fake.lastAuth != "Bearer substrate_tok_geoah_test" {
		t.Fatalf("Authorization = %q, want the context's bearer token", h.fake.lastAuth)
	}
	got, err := os.ReadFile(filepath.Join(dir, "ada.example.com-42.tar"))
	if err != nil {
		t.Fatalf("the archive was not written under the server's name: %v", err)
	}
	if !bytes.Equal(got, h.fake.exportTar) {
		t.Fatalf("the written archive is not the one the server sent (%d bytes, want %d)", len(got), len(h.fake.exportTar))
	}
	for _, want := range []string{
		"export of ada.example.com: " + strconv.Itoa(len(got)) + " bytes written to ada.example.com-42.tar",
		"point:     seq 42, checksum " + strings.Repeat("ab", 32),
		"changelog: 2 segment(s)",
		"sealed:    1 file(s)",
		"blobs:     1 (16 bytes)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	// A second run refuses the file that is already there.
	if _, _, err := h.run("export"); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("an existing file must be refused, got %v", err)
	}
	// -o names the file; -o - streams it to stdout with the report on stderr.
	h.mustRun("export", "-o", filepath.Join(dir, "named.tar"))
	if _, err := os.Stat(filepath.Join(dir, "named.tar")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := h.mustRun("export", "-o", "-")
	if stdout != string(h.fake.exportTar) {
		t.Fatalf("-o - did not stream the archive to stdout (%d bytes)", len(stdout))
	}
	if !strings.Contains(stderr, "seq 42") {
		t.Fatalf("-o - must put the report on stderr:\n%s", stderr)
	}
}

// An archive the connection cut short has no snapshot.json at its end: the
// command refuses it, names the file, and leaves nothing behind.
func TestExportRefusesAnArchiveThatEndedEarly(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.exportTar = exportArchive(t, false)
	target := filepath.Join(t.TempDir(), "cut.tar")
	_, _, err := h.run("export", "-o", target)
	if err == nil || !strings.Contains(err.Error(), "ended before snapshot.json") {
		t.Fatalf("an incomplete archive must be refused, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the refused archive was left on disk: %v", err)
	}
}

// A snapshot.json anywhere but the layout's root is a blob or a stray, not
// the completion marker: an archive ending in one is still incomplete.
func TestExportAcceptsSnapshotOnlyAtTheLayoutRoot(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range []string{"repositories/ada.example.com/repository.json", "repositories/ada.example.com/blobs/snapshot.json"} {
		body := `{"format":1,"head":7}`
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	if _, err := readExport(&buf); err == nil || !strings.Contains(err.Error(), "ended before snapshot.json") {
		t.Fatalf("a nested snapshot.json must not complete the archive, got %v", err)
	}
}

// An entry that climbs out of repositories/ is refused before it is trusted.
func TestExportRefusesAnArchiveThatClimbsOut(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "repositories/../etc/passwd", Mode: 0o600, Size: 0, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	if _, err := readExport(&buf); err == nil || !strings.Contains(err.Error(), "not a plain path") {
		t.Fatalf("a climbing entry must be refused, got %v", err)
	}
	if name := attachmentName(`attachment; filename="../x.tar"`); name != "" {
		t.Fatalf("a climbing file name must be dropped, got %q", name)
	}
}
