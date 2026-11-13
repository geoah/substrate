package commands

// The owner's recovery export: GET /api/v1/export streamed to a file. It is
// the repository's archive as one file (decisions 0278 and 0279), a tar laid
// out as an archive store holding one archive of its own
// (`repositories/<id>/<archive>/...`), with its manifest, the point it holds,
// as its last entry (`repositories/<id>/manifests/<archive>/...`). The bytes are written as they arrive and read back through a tar
// reader on the way, so the command can refuse an archive the server cut
// short and print the point the complete one holds. Nothing here opens a
// sealed file: the archive's are ciphertext under a key it does not carry.

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/geoah/substrate/internal/changelogfile"
)

func (a *app) exportCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Download the repository's recovery export, its archive as one file",
		Long: `Download the repository's archive as one tar, as of one committed point.

The tar is laid out as an archive store holding one archive of its own,
under an id minted at the export: repositories/<id>/<archive>/ holding
blobs/ (the bytes of every stored attachment), sealed/, spool/ (webhook
requests answered and not yet recorded) and changelog/<dialect>/ (the history
in pieces, every line carrying its checksum), and, last,
repositories/<id>/manifests/<archive>/<epoch>-<hash>.json, the manifest,
whose head is the seq the archive holds every line up to. The server pins the
point in one read of the database and streams the history afterwards, so
writing goes on during the download.

The archive carries no host key. Its sealed files are ciphertext under the
repository's data key, which the manifest holds wrapped under the server's
SUBSTRATE_CREDENTIAL_KEY. To restore on a host with the same credential key,
extract the tar into the archive the server reads (SUBSTRATE_ARCHIVE, by
default <SUBSTRATE_DATA_ROOT>/archive) with the server stopped, and boot: a
repository the archive holds and the database does not is restored from it.

The file is named by the server (<authority>-<head>.tar) unless -o names one;
an existing file is never overwritten. -o - writes the archive to stdout and
the report to stderr. An archive the connection cut short is refused and
removed: the export is complete only when its manifest is its last entry.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := a.client()
			if err != nil {
				return err
			}
			resp, err := cl.send(cmd.Context(), http.MethodGet, pathExport, nil, nil)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			name := output
			if name == "" {
				name = attachmentName(resp.Header.Get("Content-Disposition"))
			}
			if name == "" {
				name = "export.tar"
			}
			report, err := a.saveExport(resp.Body, name)
			if err != nil {
				return err
			}
			out := a.out
			if name == "-" {
				out = a.errOut
			}
			fmt.Fprintf(out, "export of %s: %d bytes written to %s\n", report.manifest.Authority, report.bytes, name)
			fmt.Fprintf(out, "  point:     seq %d, checksum %s\n", report.manifest.Head, strings.TrimPrefix(report.manifest.HeadSum, "sha256:"))
			fmt.Fprintf(out, "  changelog: %d piece(s)\n", report.pieces)
			fmt.Fprintf(out, "  sealed:    %d file(s)\n", report.sealedFiles)
			fmt.Fprintf(out, "  blobs:     %d (%d bytes)\n", report.blobs, report.blobBytes)
			fmt.Fprintln(out, "  restore:   extract into a stopped server's archive (SUBSTRATE_ARCHIVE, by default <SUBSTRATE_DATA_ROOT>/archive) and boot")
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the archive here (default: the server's file name; - for stdout)")
	return cmd
}

// attachmentName is the file name a Content-Disposition header offers, or ""
// when it offers none or one that is not a plain file name.
func attachmentName(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	name := params["filename"]
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return ""
	}
	return name
}

// exportReport is what the archive said about itself on the way through.
type exportReport struct {
	// id is the repository's id, the directory the archive lays out under;
	// its name is the manifest's authority.
	id          string
	manifest    changelogfile.Manifest
	bytes       int64
	pieces      int
	sealedFiles int
	blobs       int
	blobBytes   int64
}

// saveExport writes the archive to name (a.out for "-") while reading it
// back, and removes a file it cannot vouch for. The file is created
// exclusively: an export is a fresh copy, never written over an older one.
func (a *app) saveExport(body io.Reader, name string) (exportReport, error) {
	out := a.out
	var f *os.File
	if name != "-" {
		var err error
		if f, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err != nil {
			if errors.Is(err, os.ErrExist) {
				return exportReport{}, fmt.Errorf("refusing to overwrite %s: an export is written to a new file (pass -o for another name)", name)
			}
			return exportReport{}, err
		}
		out = f
	}
	report, err := readExport(io.TeeReader(body, out))
	if f != nil {
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(name)
		}
	}
	return report, err
}

// readExport walks the archive: every entry must sit under repositories/,
// nothing may climb out of it, and the manifest must be the last entry, or
// the stream was cut before the server finished. The whole stream is read,
// tar trailer included, so a tee behind r receives every byte.
func readExport(r io.Reader) (exportReport, error) {
	counted := &countingReader{r: r}
	tr := tar.NewReader(counted)
	var report exportReport
	// manifest is set when the entry just read was the archive's manifest,
	// and cleared by any entry after it, so the archive is complete only when
	// that file is last.
	manifest := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return report, fmt.Errorf("the export is not a readable archive: %w", err)
		}
		name := strings.TrimSuffix(h.Name, "/")
		parts := strings.Split(name, "/")
		if parts[0] != changelogfile.RepositoriesDir || (len(parts) < 2 && h.Typeflag != tar.TypeDir) {
			return report, fmt.Errorf("the archive names %s outside %s/", h.Name, changelogfile.RepositoriesDir)
		}
		for _, p := range parts {
			if p == "" || p == "." || p == ".." {
				return report, fmt.Errorf("the archive names %s, which is not a plain path", h.Name)
			}
		}
		if len(parts) < 2 {
			continue
		}
		if report.id == "" {
			report.id = parts[1]
		} else if parts[1] != report.id {
			return report, fmt.Errorf("the archive holds two repositories, %s and %s", report.id, parts[1])
		}
		manifest = false
		if h.Typeflag == tar.TypeDir {
			continue
		}
		k, ok := changelogfile.ParseArchiveKey(name)
		switch {
		case !ok:
		case k.Manifests:
			if _, _, ok := changelogfile.ParseArchiveManifestName(k.Name); !ok {
				break
			}
			if h.Size > maxManifestBytes {
				return report, fmt.Errorf("the archive's manifest is %d bytes, more than the %d this client reads", h.Size, maxManifestBytes)
			}
			raw, err := io.ReadAll(io.LimitReader(tr, maxManifestBytes))
			if err != nil {
				return report, err
			}
			if !changelogfile.ManifestNamesBytes(k.Name, raw) {
				return report, fmt.Errorf("the archive's manifest %s is not the bytes its name says", k.Name)
			}
			if report.manifest, err = changelogfile.DecodeArchiveManifest(raw, parts[1]); err != nil {
				return report, fmt.Errorf("the archive's manifest does not read: %w", err)
			}
			if report.manifest.Archive != k.Archive {
				return report, fmt.Errorf("the archive's manifest %s names archive %s", k.Name, report.manifest.Archive)
			}
			manifest = true
		default:
			if _, _, _, _, ok := changelogfile.ParsePieceObjectName(k.Name); ok {
				report.pieces++
			} else if _, _, ok := changelogfile.ParseBlobObjectName(k.Name); ok {
				report.blobs++
				report.blobBytes += h.Size
			} else if path.Dir(k.Name) == changelogfile.SealedSubdir {
				report.sealedFiles++
			}
		}
		if _, err := io.Copy(io.Discard, tr); err != nil { //nolint:gosec // G110: the entry drains to io.Discard and nothing is held in memory
			return report, fmt.Errorf("the export ended inside %s: %w", h.Name, err)
		}
	}
	// The end-of-archive blocks: read so the tee writes them.
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return report, err
	}
	report.bytes = counted.n
	if !manifest {
		return report, errors.New("the export ended before its manifest, so it is incomplete: run it again")
	}
	return report, nil
}

// maxManifestBytes bounds the manifest the client reads whole. A manifest
// grows with the repository's sealed values, about 120 bytes each, and the
// blobs it names missing: 64 MiB is half a million sealed values, where a
// repository holds a few per connected account. The bound is against an
// archive that lies about the entry's size.
const maxManifestBytes = 64 << 20

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
