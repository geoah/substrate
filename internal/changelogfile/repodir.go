package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// ErrLegacyRepositoryDir is returned for a directory under repositories/
// whose name is neither an authority nor a repository id of the shape written
// before the authority became the id.
var ErrLegacyRepositoryDir = errors.New("changelogfile: not a repository directory")

// reLegacyRepositoryID is the grammar the directory name had before the
// authority became the id: one path segment of the record id alphabet. It
// exists so a directory written by that binary can be found and renamed.
var reLegacyRepositoryID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

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
// A directory named by a pre-authority id is such a refusal too: the caller
// renames it first (ListLegacyRepositoryDirs, RenameRepoDir).
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

// ListLegacyRepositoryDirs returns the names of the directories under root
// that a binary from before the authority became the id wrote: names of the
// old id grammar that are not authorities, in name order. A name that is
// neither is refused with ErrLegacyRepositoryDir. Files and dot-prefixed
// directories are ignored as in ListRepositoryDirs.
func ListLegacyRepositoryDirs(root string) ([]string, error) {
	names, err := listDirs(root)
	if err != nil {
		return nil, err
	}
	var legacy []string
	for _, name := range names {
		if vocabulary.ValidRepositoryAuthority(name) {
			continue
		}
		if !reLegacyRepositoryID.MatchString(name) || name == "." || name == ".." {
			return nil, fmt.Errorf("%w: %q", ErrLegacyRepositoryDir, name)
		}
		legacy = append(legacy, name)
	}
	return legacy, nil
}

// LegacyRepoDir is the directory a pre-authority binary named by the random
// id it minted, `<root>/repositories/<id>`, for the boot check that moves it
// under its authority. The id is held to the old grammar, so it is one path
// segment.
func LegacyRepoDir(root, id string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	if !reLegacyRepositoryID.MatchString(id) || id == "." || id == ".." {
		return "", fmt.Errorf("%w: %q", ErrLegacyRepositoryDir, id)
	}
	return filepath.Join(filepath.Clean(root), RepositoriesDir, id), nil
}

// RenameRepoDir renames the directory `<root>/repositories/<from>` to the
// repository directory of authority to. from is held to the old id grammar
// and to to the authority grammar; a target that already exists is refused,
// because two directories claiming one authority is a question for an
// operator, not a rename.
func RenameRepoDir(root, from, to string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	src, err := LegacyRepoDir(root, from)
	if err != nil {
		return "", err
	}
	dst, err := RepoDir(root, to)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("changelogfile: rename %s to %s: the target already exists", from, to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return dst, nil
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
