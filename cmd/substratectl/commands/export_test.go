package commands

import (
	"archive/tar"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
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

// exportID is the repository id the fixture archive is laid out under.
const exportID = "rnd-01jbx9m2qz7k4c5v8n3w6t1hrd"

// exportArchive builds an archive in the server's layout: a blob, a sealed
// file, two pieces of history and, when complete is set, the manifest last.
func exportArchive(t *testing.T, complete bool) []byte {
	t.Helper()
	return exportArchiveSealing(t, complete, 0)
}

// exportArchiveSealing is exportArchive whose manifest selects sealed more
// sealed values, so it grows as a repository's does.
func exportArchiveSealing(t *testing.T, complete bool, sealed int) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	const pass = "0123456789abcdef"
	archiveID := "a" + strings.Repeat("0", 15) + "1" + strings.Repeat("9", 8)
	root := "repositories/" + exportID + "/" + archiveID + "/"
	manifests := "repositories/" + exportID + "/manifests/" + archiveID + "/"
	write := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"repositories/", "repositories/" + exportID + "/", root, root + "changelog/", root + "changelog/1/", root + "blobs/", root + "sealed/", manifests} {
		if err := tw.WriteHeader(&tar.Header{Name: dir, Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	var cd, ab [32]byte
	copy(cd[:], bytes.Repeat([]byte{0xcd}, 32))
	copy(ab[:], bytes.Repeat([]byte{0xab}, 32))
	pieces := []changelogfile.ArchivePiece{
		{Name: changelogfile.PieceObjectName(1, 1, 40, pass, ab), Sum: changelogfile.SumText(ab), Size: 3},
		{Name: changelogfile.PieceObjectName(1, 41, 42, pass, ab), Sum: changelogfile.SumText(ab), Size: 3},
	}
	write(root+changelogfile.BlobObjectName("blob-sha256-"+strings.Repeat("a", 64), pass), "attachment bytes")
	write(root+changelogfile.SealedObjectName("secret:abc", cd, pass), `{"ref":"secret:abc"}`+"\n")
	for _, p := range pieces {
		write(root+p.Name, "{}\n")
	}
	if complete {
		selected := map[string]string{"secret:abc": changelogfile.SealedObjectName("secret:abc", cd, pass)}
		for i := range sealed {
			ref := fmt.Sprintf("secret:%032x", i)
			selected[ref] = changelogfile.SealedObjectName(ref, ab, pass)
		}
		m, err := changelogfile.EncodeArchiveManifest(changelogfile.Manifest{
			Format: changelogfile.ManifestFormatArchive, ID: exportID, Authority: "ada.example.com", Archive: archiveID, Pass: pass,
			ChangelogDialect: 1, VocabularyDialect: 3, Head: 42, HeadSum: "sha256:" + strings.Repeat("ab", 32),
			Epoch: 7, Pieces: pieces, Sealed: selected,
		})
		if err != nil {
			t.Fatal(err)
		}
		write(manifests+changelogfile.ArchiveManifestName(7, m), string(m))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The archive lands byte for byte under the server's file name, the request
// carried the bearer token and nothing else, and the report reads the point
// off the manifest.
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
		"changelog: 2 piece(s)",
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

// An archive the connection cut short has no manifest at its end: the command
// refuses it, names the file, and leaves nothing behind.
func TestExportRefusesAnArchiveThatEndedEarly(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.exportTar = exportArchive(t, false)
	target := filepath.Join(t.TempDir(), "cut.tar")
	_, _, err := h.run("export", "-o", target)
	if err == nil || !strings.Contains(err.Error(), "ended before its manifest") {
		t.Fatalf("an incomplete archive must be refused, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the refused archive was left on disk: %v", err)
	}
}

// A manifest's name anywhere but the repository's manifests/ is a blob or a
// stray, not the completion marker: an archive ending in one is still
// incomplete.
func TestExportAcceptsTheManifestOnlyUnderManifests(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range []string{"repositories/" + exportID + "/" + "a" + strings.Repeat("0", 15) + "1" + strings.Repeat("9", 8) + "/blobs/" + changelogfile.ArchiveManifestName(7, []byte("x"))} {
		body := `{"format":3,"head":7}`
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	if _, err := readExport(&buf); err == nil || !strings.Contains(err.Error(), "ended before its manifest") {
		t.Fatalf("a manifest's name outside manifests/ must not complete the archive, got %v", err)
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

// A manifest grows with the repository's sealed values; one past the old
// 1 MiB bound (twenty thousand of them) is a valid export the client keeps.
func TestExportKeepsAnArchiveWhoseManifestIsLarge(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.exportTar = exportArchiveSealing(t, true, 20000)
	h.fake.exportName = "ada.example.com-42.tar"
	dir := t.TempDir()
	t.Chdir(dir)
	out, _ := h.mustRun("export")
	got, err := os.ReadFile(filepath.Join(dir, "ada.example.com-42.tar"))
	if err != nil || !bytes.Equal(got, h.fake.exportTar) {
		t.Fatalf("the archive with a large manifest was not kept (%v)", err)
	}
	if !strings.Contains(out, "point:     seq 42") {
		t.Fatalf("report:\n%s", out)
	}
}
