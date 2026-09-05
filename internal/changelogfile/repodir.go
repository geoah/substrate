package changelogfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The data root's layout: `<root>/repositories/<id>/` holds one repository,
// with its changelog, blobs and sealed records in the three subdirectories
// and the manifest at its top.
const (
	RepositoriesDir = "repositories"
	changelogSubdir = "changelog"
	blobsSubdir     = "blobs"
	sealedSubdir    = "sealed"
)

// ErrRepositoryID is returned for an id that is not one path segment of the
// record id alphabet: it would name a directory outside the root.
var ErrRepositoryID = errors.New("changelogfile: not a repository id")

// reRepositoryID matches a repository id, the same grammar internal/blobbytes
// checks: one path segment of the id alphabet. `.` and `..` match the class
// and are refused by name.
var reRepositoryID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func checkRepositoryID(id string) error {
	if !reRepositoryID.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("%w: %q", ErrRepositoryID, id)
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

// RepoDir is the directory of repository id under root,
// `<root>/repositories/<id>`.
func RepoDir(root, id string) (string, error) {
	if err := checkRoot(root); err != nil {
		return "", err
	}
	if err := checkRepositoryID(id); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(root), RepositoriesDir, id), nil
}

// ChangelogDir is the changelog directory of a repository directory.
func ChangelogDir(repoDir string) string { return filepath.Join(repoDir, changelogSubdir) }

// BlobsDir is the blob bytes directory of a repository directory.
func BlobsDir(repoDir string) string { return filepath.Join(repoDir, blobsSubdir) }

// SealedDir is the sealed records directory of a repository directory.
func SealedDir(repoDir string) string { return filepath.Join(repoDir, sealedSubdir) }

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

// ListRepositoryDirs returns the ids of the repository directories under
// root, in name order. A missing `repositories/` lists as empty; files and
// dot-prefixed directories (a filesystem's `.snapshot`, a sync tool's state)
// are ignored, because no repository id starts with a dot; any other directory
// whose name is not a repository id is refused, because a directory the boot
// check silently skipped would be a repository that never imports.
func ListRepositoryDirs(root string) ([]string, error) {
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
	var ids []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if err := checkRepositoryID(e.Name()); err != nil {
			return nil, err
		}
		ids = append(ids, e.Name())
	}
	return ids, nil
}
