// Package kinds carries the shipped vocabulary and nothing else: one directory
// per authority, named for the authority, holding that authority's manifests as
// files. A deploy is a git diff.
//
// The tree is embedded whole, and the two views of it are the two roles a
// directory can play. [Seed] is the substrate's own machinery — the authority
// repository creation writes into a new repository's changelog. [Bundles] is
// everything else: the vocabulary a repository imports and the bundles a
// console lists as available.
//
// The split is by NAME rather than by directory so that adding an authority is
// adding a directory. A layout that nested the two roles would let a new bundle
// land in the wrong half and go unnoticed; here the only way to be the seed is
// to be the seed.
package kinds

import (
	"embed"
	"io/fs"
)

// SeedAuthority is the one authority that is the seed.
const SeedAuthority = "core.substrate.reamde.dev"

// The pattern is part of the contract: an authority directory it misses is a
// vocabulary that silently stops existing in production while every other test
// still passes. kinds_test.go holds it to the tree on disk.
//
//go:embed all:*.substrate.reamde.dev
var files embed.FS

// Seed is the seed authority alone, as a filesystem whose root holds that one
// directory — the shape the schema loader walks.
func Seed() fs.FS {
	return authorities(func(name string) bool { return name == SeedAuthority })
}

// Bundles is every authority that is not the seed, as a filesystem whose root
// holds one directory per bundle closure — the shape the catalog reads.
func Bundles() fs.FS {
	return authorities(func(name string) bool { return name != SeedAuthority })
}

// All is the whole tree, seed and bundles together.
func All() fs.FS { return files }

func authorities(keep func(string) bool) fs.FS { return rootFilterFS{keep: keep} }

// rootFilterFS narrows what the ROOT lists; every path below it reads through
// untouched. Filtering only the root is what makes the two views cheap: a
// caller that already holds a path never pays for the predicate, and a walk
// that never enters a directory never sees it.
type rootFilterFS struct {
	keep func(string) bool
}

func (f rootFilterFS) Open(name string) (fs.File, error) { return files.Open(name) }

func (f rootFilterFS) ReadFile(name string) ([]byte, error) { return files.ReadFile(name) }

func (f rootFilterFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := files.ReadDir(name)
	if err != nil || name != "." {
		return entries, err
	}
	kept := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if f.keep(e.Name()) {
			kept = append(kept, e)
		}
	}
	return kept, nil
}
