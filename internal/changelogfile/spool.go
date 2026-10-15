package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SpoolSubdir holds the webhook requests the door answered and the drain has
// not yet recorded in the changelog (decision 0167). It sits in the
// repository directory so every copy of the directory (a plain copy, a
// snapshot, an export) carries the requests with it.
const SpoolSubdir = "spool"

// spoolSuffix ends every spooled request's file name. A name without it (a
// half-written `.incoming-` file, a file the drain set aside as unreadable)
// is not a request.
const spoolSuffix = ".webhook"

// UnreadableSpoolSuffix is appended to a spooled request the drain could not
// open, so the drain moves past it and an operator still finds the bytes.
const UnreadableSpoolSuffix = ".unreadable"

// reSpoolName is `<20-digit ordinal>-<fire id>.webhook`. The ordinal is the
// receipt time in Unix nanoseconds, made strictly increasing by the writer,
// so name order is arrival order.
var reSpoolName = regexp.MustCompile(`^([0-9]{20})-([A-Za-z0-9_.-]{1,64})\.webhook$`)

// ErrSpoolName is returned for a name that is not a spooled request's.
var ErrSpoolName = errors.New("changelogfile: not a spooled webhook file name")

// SpoolDir is the spool directory of a repository directory.
func SpoolDir(repoDir string) string { return filepath.Join(repoDir, SpoolSubdir) }

// SpoolFileName is the file a request received at ordinal under fireID is
// spooled to.
func SpoolFileName(ordinal int64, fireID string) string {
	return fmt.Sprintf("%020d-%s%s", ordinal, fireID, spoolSuffix)
}

// ParseSpoolFileName splits a spooled request's file name into its ordinal
// and fire id.
func ParseSpoolFileName(name string) (int64, string, error) {
	m := reSpoolName.FindStringSubmatch(name)
	if m == nil {
		return 0, "", fmt.Errorf("%w: %q", ErrSpoolName, name)
	}
	ordinal, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %q", ErrSpoolName, name)
	}
	return ordinal, m[2], nil
}

// SpoolOrdinalTime is the receipt time an ordinal encodes.
func SpoolOrdinalTime(ordinal int64) time.Time { return time.Unix(0, ordinal).UTC() }

// SpoolFile is one spooled request on disk.
type SpoolFile struct {
	Name string
	Size int64
}

// WriteSpoolFile writes one spooled request durably: a temporary file,
// fsynced, renamed to name, and the directory fsynced, so the request is on
// disk under its name when this returns.
func WriteSpoolFile(repoDir, name string, data []byte) error {
	if _, _, err := ParseSpoolFileName(name); err != nil {
		return err
	}
	dir := SpoolDir(repoDir)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	return writeFileAtomic(dir, name, data)
}

// ReadSpoolFile reads one spooled request's bytes.
func ReadSpoolFile(repoDir, name string) ([]byte, error) {
	if _, _, err := ParseSpoolFileName(name); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(SpoolDir(repoDir), name))
}

// RemoveSpoolFile deletes one spooled request and fsyncs the directory, so
// the deletion is durable when this returns. A file already gone is not an
// error.
func RemoveSpoolFile(repoDir, name string) error {
	if _, _, err := ParseSpoolFileName(name); err != nil {
		return err
	}
	dir := SpoolDir(repoDir)
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDir(dir)
}

// SetSpoolFileAside renames a spooled request the drain cannot open to
// `<name>.unreadable`, which no listing returns, and fsyncs the directory.
func SetSpoolFileAside(repoDir, name string) error {
	if _, _, err := ParseSpoolFileName(name); err != nil {
		return err
	}
	dir := SpoolDir(repoDir)
	if err := os.Rename(filepath.Join(dir, name), filepath.Join(dir, name+UnreadableSpoolSuffix)); err != nil {
		return err
	}
	return syncDir(dir)
}

// SpoolFiles lists the spooled requests in name order, which is arrival
// order. A missing directory lists as empty; a half-written file, a file set
// aside and anything else not named as a request is skipped.
func SpoolFiles(repoDir string) ([]SpoolFile, error) {
	entries, err := os.ReadDir(SpoolDir(repoDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SpoolFile
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, _, err := ParseSpoolFileName(e.Name()); err != nil {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, SpoolFile{Name: e.Name(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RemoveSpoolTemps deletes the half-written files a stop left in the spool
// directory: a request whose rename never happened was never answered.
func RemoveSpoolTemps(repoDir string) error {
	dir := SpoolDir(repoDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
