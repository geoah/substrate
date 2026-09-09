package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The data root's layout: `<root>/repositories/<authority>/` holds one
// repository, with its changelog, blobs and sealed records in the three
// subdirectories and the manifest at its top. The repository's authority
// (decision 0046) is its id, so the directory is named by it.
const (
	RepositoriesDir = "repositories"
	ChangelogSubdir = "changelog"
	BlobsSubdir     = "blobs"
	SealedSubdir    = "sealed"
)

// ErrRepositoryAuthority is returned for a repository id that is not an
// authority (vocabulary.ValidRepositoryAuthority): lowercase DNS labels with
// at least one dot, so it is always one path segment under the root.
var ErrRepositoryAuthority = errors.New("changelogfile: not a repository authority")

// checkRepositoryAuthority holds a repository id to the grammar the engine
// admits at registration. The same rule is checked in internal/blobbytes,
// where the id is a path segment for the same reason.
func checkRepositoryAuthority(id string) error {
	if !vocabulary.ValidRepositoryAuthority(id) {
		return fmt.Errorf("%w: %q", ErrRepositoryAuthority, id)
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

// RepoDir is the directory of the repository whose authority is id under
// root, `<root>/repositories/<authority>`.
func RepoDir(root, id string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	if err := checkRepositoryAuthority(id); err != nil {
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

// ListRepositoryDirs returns the authorities of the repository directories
// under root, in name order. A missing `repositories/` lists as empty; files
// and dot-prefixed directories (a filesystem's `.snapshot`, a sync tool's
// state) are ignored, because no authority starts with a dot; any other
// directory whose name is not an authority is refused, because a directory
// the boot check silently skipped would be a repository that never imports.
func ListRepositoryDirs(root string) ([]string, error) {
	names, err := listDirs(root)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := checkRepositoryAuthority(name); err != nil {
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
