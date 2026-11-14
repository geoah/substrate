package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The data root's layout: `<root>/repositories/<id>/` holds one repository,
// with its changelog, blobs and sealed records in the three subdirectories
// and the manifest at its top. The directory is named by the repository's id
// (decision 0261); one a release before ids wrote is named by its authority
// until the boot converts it.
const (
	RepositoriesDir = "repositories"
	ChangelogSubdir = "changelog"
	BlobsSubdir     = "blobs"
	SealedSubdir    = "sealed"
)

var (
	// ErrRepositoryAuthority is returned for a repository name that is not an
	// authority (vocabulary.ValidRepositoryAuthority): lowercase DNS labels
	// with at least one dot.
	ErrRepositoryAuthority = errors.New("changelogfile: not a repository authority")
	// ErrRepositoryKey is returned for a directory key that is neither a
	// repository id (vocabulary.ValidRepositoryID) nor, for a directory from
	// before ids, an authority. Either is always one path segment.
	ErrRepositoryKey = errors.New("changelogfile: not a repository id")
)

// checkRepositoryKey holds a directory's key to the grammar of a repository
// id, or of the authority a directory from before ids is named by. The same
// rule is checked in internal/blobbytes, where the key is a path segment for
// the same reason.
func checkRepositoryKey(key string) error {
	if !vocabulary.ValidRepositoryID(key) && !vocabulary.ValidRepositoryAuthority(key) {
		return fmt.Errorf("%w: %q", ErrRepositoryKey, key)
	}
	return nil
}

// checkRoot refuses a relative root: it would follow the process's working
// directory, and a store that moves when the server restarts from another
// directory has lost its repositories.
func checkRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("changelogfile: the data root %q must be an absolute path", root)
	}
	return nil
}

// RepoDir is the directory of the repository keyed id under root,
// `<root>/repositories/<id>`.
func RepoDir(root, id string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	if err := checkRepositoryKey(id); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(root), RepositoriesDir, id), nil
}

// ChangelogDir is the changelog directory of a repository directory.
func ChangelogDir(repoDir string) string { return filepath.Join(repoDir, ChangelogSubdir) }

// BlobsDir is the blob bytes directory of a repository directory.
func BlobsDir(repoDir string) string { return filepath.Join(repoDir, BlobsSubdir) }

// SealedDir is the sealed records directory of a repository directory.
func SealedDir(repoDir string) string { return filepath.Join(repoDir, SealedSubdir) }

// EnsureRepoDir creates the repository directory, mode 0700, and returns it.
// It holds what the server keeps of a repository outside Postgres: its blob
// bytes and its webhook spool (decision 0262). An existing directory is left
// alone.
func EnsureRepoDir(root, id string) (string, error) {
	dir, err := RepoDir(root, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", err
	}
	return dir, nil
}

// HasHistory reports whether a repository directory holds the history a
// release before the archive kept beside the blobs (a 0.x directory): a
// manifest, segment files or sealed files. A directory whose history the
// table adopted and whose removal did not finish (HistoryAdopted) holds none.
func HasHistory(repoDir string) bool {
	if HistoryAdopted(repoDir) {
		return false
	}
	for _, p := range []string{filepath.Join(repoDir, ManifestName), ChangelogDir(repoDir), SealedDir(repoDir)} {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

// HistoryState is HistoryAdopted and HasHistory for the boot's discovery,
// with the failure to read the directory: a directory the boot cannot read is
// no answer, never one with no history whose row is then taken as current.
func HistoryState(repoDir string) (adopted, history bool, err error) {
	if adopted, err = present(filepath.Join(repoDir, AdoptedManifestName)); err != nil || adopted {
		return adopted, false, err
	}
	for _, p := range []string{filepath.Join(repoDir, ManifestName), ChangelogDir(repoDir), SealedDir(repoDir)} {
		ok, err := present(p)
		if err != nil || ok {
			return false, ok, err
		}
	}
	return false, false, nil
}

func present(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// KeyingRecordsDir is where the boot's keying records which row a directory
// still named by its repository's authority is: `<root>/repositories/.keying/
// <authority>`, holding the id it gives that row and a newline. It is written
// before the row's transaction, beside the directories and not in one, so
// damage to the directory never reaches it, and it is removed once the
// directory is under its id (decision 0281). The dot keeps discovery
// (listDirs) passing over it.
const KeyingRecordsDir = ".keying"

func keyingRecordsDir(root string) string {
	return filepath.Join(filepath.Clean(root), RepositoriesDir, KeyingRecordsDir)
}

// WriteKeyingRecord records, durably, that the directory named name is the
// repository the keying gives id.
func WriteKeyingRecord(root, name, id string) error {
	if err := checkRoot(root); err != nil {
		return err
	}
	if !vocabulary.ValidRepositoryAuthority(name) {
		return fmt.Errorf("%w: %q", ErrRepositoryAuthority, name)
	}
	if !vocabulary.ValidRepositoryID(id) {
		return fmt.Errorf("%w: %q", ErrRepositoryKey, id)
	}
	dir := keyingRecordsDir(root)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(dir)); err != nil {
		return err
	}
	return writeFileAtomic(dir, name, []byte(id+"\n"))
}

// ReadKeyingRecords reads every keying record, by the name of the directory
// each ties to an id. One that does not read is the data root's failure, not
// one repository's.
func ReadKeyingRecords(root string) (map[string]string, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}
	dir := keyingRecordsDir(root)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(string(raw))
		if !vocabulary.ValidRepositoryAuthority(e.Name()) || !vocabulary.ValidRepositoryID(id) {
			return nil, fmt.Errorf("%w: the keying record %s names %q; move it out of the data root",
				ErrRepositoryKey, filepath.Join(dir, e.Name()), id)
		}
		out[e.Name()] = id
	}
	return out, nil
}

// RemoveKeyingRecord removes the keying record of name, durably; a name with
// none is left as it is.
func RemoveKeyingRecord(root, name string) error {
	if err := checkRoot(root); err != nil {
		return err
	}
	dir := keyingRecordsDir(root)
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDir(dir)
}

// AdoptedManifestName is a 0.x directory's manifest once the table holds
// every line of its history: RemoveHistory sets the manifest aside under this
// name before it removes anything, so a removal cut short leaves a directory
// the next boot knows to finish removing, never one it reads as history.
const AdoptedManifestName = ManifestName + ".adopted"

// The steps of RemoveHistory a caller's stop function is told of, for a test
// that stops it there.
const (
	RemoveHistorySetAside = "after the manifest was set aside"
	RemoveHistoryPartly   = "after the first of the history's files was removed"
)

// HistoryAdopted reports whether RemoveHistory began on the directory and did
// not finish.
func HistoryAdopted(repoDir string) bool {
	_, err := os.Lstat(filepath.Join(repoDir, AdoptedManifestName))
	return err == nil
}

// SetAsideHistory sets a directory's history aside, once the table holds
// every line of it: its manifest renamed, made durable, so from then on the
// directory is no history (HistoryAdopted) and no boot reads it again. stop,
// when not nil, is called after the rename (RemoveHistorySetAside).
func SetAsideHistory(repoDir string, stop func(stage string) error) error {
	manifest, adopted := filepath.Join(repoDir, ManifestName), filepath.Join(repoDir, AdoptedManifestName)
	if err := os.Rename(manifest, adopted); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDir(repoDir); err != nil {
		return err
	}
	if stop == nil {
		return nil
	}
	return stop(RemoveHistorySetAside)
}

// RemoveHistory removes what HasHistory finds, once the table holds every
// line of it. The manifest is set aside first, in one rename made durable:
// from then on the directory is no history, and a removal cut short anywhere
// after is finished by the next boot (HistoryAdopted) rather than read as a
// history missing its first segments. The set-aside manifest goes last. The
// blobs and the spool stay. stop, when not nil, is called after each named
// step (RemoveHistorySetAside, RemoveHistoryPartly) and ends the removal
// when it returns an error.
func RemoveHistory(repoDir string, stop func(stage string) error) error {
	if stop == nil {
		stop = func(string) error { return nil }
	}
	if !HistoryAdopted(repoDir) {
		if err := SetAsideHistory(repoDir, stop); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return err
	}
	first := true
	for _, e := range entries {
		name := e.Name()
		if name != ChangelogSubdir && name != SealedSubdir && !strings.HasPrefix(name, ChangelogSubdir+".") {
			continue
		}
		path := filepath.Join(repoDir, name)
		if first && e.IsDir() {
			// One file at a time for the first directory, so a stop after
			// its first file leaves the history part removed, as a crash
			// inside the removal would.
			files, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			for _, f := range files {
				if err := os.RemoveAll(filepath.Join(path, f.Name())); err != nil {
					return err
				}
				if first {
					first = false
					if err := stop(RemoveHistoryPartly); err != nil {
						return err
					}
				}
			}
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	if err := syncDir(repoDir); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(repoDir, AdoptedManifestName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(repoDir)
}

// ListRepositoryDirs returns the keys of the repository directories under
// root, in name order: ids, and the authorities of directories from before
// ids, which the boot converts. A missing `repositories/` lists as empty;
// files and dot-prefixed directories (a filesystem's `.snapshot`, a sync
// tool's state) are ignored, because no key starts with a dot; any other
// directory whose name is not a key is refused, because a directory the boot
// check silently skipped would be a repository that never imports.
func ListRepositoryDirs(root string) ([]string, error) {
	names, err := listDirs(root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := checkRepositoryKey(name); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// listDirs is the raw walk of `<root>/repositories/`: every directory that is
// not dot-prefixed, in name order, unchecked.
func listDirs(root string) ([]string, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, RepositoriesDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}
