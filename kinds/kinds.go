// Package kinds carries the shipped vocabulary and nothing else: one directory
// per authority, named for the authority, holding one directory per PACKAGE it
// publishes and the authority's own manifest beside them. A deploy is a git
// diff.
//
// The tree is embedded whole, and the two views of it are the two roles an
// authority can play. [Seed] is the substrate's own machinery — the packages
// repository creation writes into a new repository's changelog.
// [Bundles] is everything else: the provider packages a repository installs and
// a console lists as available. The shipped SAMPLES are a second tree with its
// own embed (the samples package), because a sample is vocabulary to copy
// rather than vocabulary the substrate runs.
//
// The split is by NAME: adding an authority is adding a directory, and adding
// a package under the seed authority is adding a directory that is either in
// [SeedPackages] or carries a bundle document, which kinds_test.go holds.
// [Bundles] also serves the seed authority's catalogues (record 0192), the
// well-known packages a repository installs when it wants them.
package kinds

import (
	"embed"
	"io/fs"
	"strings"
)

// SeedAuthority is the substrate's own authority. The SEED is a list of its
// packages, not the whole authority (record 0192): core, llm (0077), console
// (0166) and observations are written into every repository, and every other
// package under it is a well-known catalogue the catalog serves, installed when
// a repository wants it.
const (
	SeedAuthority           = "substrate.reamde.dev"
	SeedCorePackage         = SeedAuthority + "/core"
	SeedLLMPackage          = SeedAuthority + "/llm"
	SeedConsolePackage      = SeedAuthority + "/console"
	SeedObservationsPackage = SeedAuthority + "/observations"
)

// SeedPackages is every package the binary seeds. A directory under the seed
// authority that is not here is a catalogue, and must carry a bundle document.
var SeedPackages = []string{SeedCorePackage, SeedLLMPackage, SeedConsolePackage, SeedObservationsPackage}

func seeded(dir string) bool {
	for _, p := range SeedPackages {
		if p == dir {
			return true
		}
	}
	return false
}

// The pattern is part of the contract: an authority directory it misses is a
// vocabulary that silently stops existing in production while every other test
// still passes. kinds_test.go holds it to the tree on disk.
//
//go:embed all:substrate.reamde.dev all:providers.substrate.reamde.dev
var files embed.FS

// Seed is the seeded packages and their authority's manifest, as a filesystem
// whose root holds the seed authority's one directory, the shape the schema
// loader walks.
func Seed() fs.FS {
	return rootFilterFS{keep: func(name string) bool {
		if !strings.Contains(name, "/") {
			return name == SeedAuthority
		}
		return !isDir(name) || seeded(name)
	}}
}

// Bundles is everything the catalog serves: every other authority whole, and
// the seed authority's catalogues with the manifest beside them, one directory
// per authority with one directory per package inside it.
func Bundles() fs.FS {
	return rootFilterFS{keep: func(name string) bool {
		if !strings.Contains(name, "/") || !strings.HasPrefix(name, SeedAuthority+"/") {
			return true
		}
		return !isDir(name) || !seeded(name)
	}}
}

func isDir(name string) bool {
	info, err := fs.Stat(files, name)
	return err == nil && info.IsDir()
}

// All is the whole tree, seed and bundles together.
func All() fs.FS { return files }

// rootFilterFS narrows what the root and the seed authority's directory list,
// by the path of each entry; every path below them reads through untouched. A
// caller that already holds a path never pays for the predicate, and a walk
// that never enters a directory never sees it.
type rootFilterFS struct {
	keep func(string) bool
}

func (f rootFilterFS) Open(name string) (fs.File, error) { return files.Open(name) }

func (f rootFilterFS) ReadFile(name string) ([]byte, error) { return files.ReadFile(name) }

func (f rootFilterFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := files.ReadDir(name)
	if err != nil || (name != "." && name != SeedAuthority) {
		return entries, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		full := e.Name()
		if name != "." {
			full = name + "/" + e.Name()
		}
		if f.keep(full) {
			kept = append(kept, e)
		}
	}
	return kept, nil
}
