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

// EnsureRepoDir creates the repository directory and its three
// subdirectories, mode 0700, and returns the repository directory. Existing
// directories are left alone.
func EnsureRepoDir(root, id string) (string, error) {
	dir, err := RepoDir(root, id)
	if err != nil {
		return "", err
	}
	for _, d := range []string{dir, ChangelogDir(dir), BlobsDir(dir), SealedDir(dir)} {
		if err := os.MkdirAll(d, dirMode); err != nil {
			return "", err
		}
	}
	return dir, nil
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
