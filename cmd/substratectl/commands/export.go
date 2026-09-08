package commands

// The owner's recovery export: GET /api/v1/export streamed to a file. The
// archive is a tar laid out as a data root (`repositories/<authority>/...`)
// with snapshot.json, the file that records the point it holds, as its last
// entry. The bytes are written as they arrive and read back through a tar
// reader on the way, so the command can refuse an archive the server cut
// short and print the point the complete one holds. Nothing here opens a
// sealed file: the archive's are ciphertext under a key it does not carry.

import (
	"archive/tar"
	"encoding/hex"
	"encoding/json"
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
		Short: "Download the repository's recovery export, a tar of its directory",
		Long: `Download the repository as a tar of its directory, as of one committed point.

The archive is laid out as a data root: repositories/<authority>/ holding
repository.json, changelog/ (every finished segment with its .sha256 sidecar
and the active segment cut at the point), sealed/, blobs/ (the bytes of every
stored attachment) and, last, snapshot.json, which records the head seq and
checksum the archive holds. The server pins the point under its writer lock
and streams the files afterwards, so writing goes on during the download.

The archive carries no host key. Its sealed files are ciphertext under the
repository's data key, which repository.json holds wrapped under the server's
SUBSTRATE_CREDENTIAL_KEY and the recoverykey record holds wrapped to your
recovery key. To restore on a host with the same credential key, extract the
archive under its SUBSTRATE_DATA_ROOT with the server stopped and boot; on a
host with another key, extract it anywhere, run 'repository rewrap' with your
recovery key, then move it under the data root and boot. 'repository verify'
prints the recorded point after either.

The file is named by the server (<authority>-<head>.tar) unless -o names one;
an existing file is never overwritten. -o - writes the archive to stdout and
the report to stderr. An archive the connection cut short is refused and
removed: the export is complete only when snapshot.json is its last entry.`,
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
			fmt.Fprintf(out, "export of %s: %d bytes written to %s\n", report.authority, report.bytes, name)
			fmt.Fprintf(out, "  point:     seq %d, checksum %s\n", report.snapshot.Head, hex.EncodeToString(report.snapshot.HeadHash[:]))
			fmt.Fprintf(out, "  changelog: %d segment(s)\n", report.segments)
			fmt.Fprintf(out, "  sealed:    %d file(s)\n", report.sealedFiles)
			fmt.Fprintf(out, "  blobs:     %d (%d bytes)\n", report.blobs, report.blobBytes)
			fmt.Fprintln(out, "  restore:   extract under a stopped server's SUBSTRATE_DATA_ROOT and boot; `repository verify` prints the point")
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
	authority   string
	snapshot    changelogfile.Snapshot
	bytes       int64
	segments    int
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
// nothing may climb out of it, and snapshot.json must be the last entry, or
// the stream was cut before the server finished. The whole stream is read,
// tar trailer included, so a tee behind r receives every byte.
func readExport(r io.Reader) (exportReport, error) {
	counted := &countingReader{r: r}
	tr := tar.NewReader(counted)
	var report exportReport
	var last string
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
		if report.authority == "" {
			report.authority = parts[1]
		} else if parts[1] != report.authority {
			return report, fmt.Errorf("the archive holds two repositories, %s and %s", report.authority, parts[1])
		}
		last = name
		if h.Typeflag == tar.TypeDir {
			continue
		}
		dir, file := path.Dir(name), path.Base(name)
		switch {
		case len(parts) == 3 && file == changelogfile.SnapshotName:
			raw, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return report, err
			}
			if err := json.Unmarshal(raw, &report.snapshot); err != nil {
				return report, fmt.Errorf("the archive's %s does not read: %w", changelogfile.SnapshotName, err)
			}
		case path.Base(dir) == changelogfile.ChangelogSubdir && strings.HasSuffix(file, ".ndjson"):
			report.segments++
		case path.Base(dir) == changelogfile.SealedSubdir:
			report.sealedFiles++
		case path.Base(dir) == changelogfile.BlobsSubdir:
			report.blobs++
			report.blobBytes += h.Size
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return report, fmt.Errorf("the export ended inside %s: %w", h.Name, err)
		}
	}
	// The end-of-archive blocks: read so the tee writes them.
	if _, err := io.Copy(io.Discard, counted); err != nil {
		return report, err
	}
	report.bytes = counted.n
	if path.Base(last) != changelogfile.SnapshotName {
		return report, fmt.Errorf("the export ended before %s, so it is incomplete: run it again", changelogfile.SnapshotName)
	}
	return report, nil
}

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
