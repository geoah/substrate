// Package samples carries the shipped SAMPLE packages: vocabulary a repository
// copies rather than vocabulary the substrate runs. One directory per package,
// all authored under one authority (samples.substrate.reamde.dev), with that
// authority's own manifest beside them.
//
// It is a second tree rather than a corner of kinds/ because the two are
// different promises. The kinds/ tree is the substrate's own: the core package
// every repository is seeded with, and the provider packages whose publisher
// ships their migrations. A sample is a starting point — people, tasks,
// calendar — meant to be read, installed and then owned by the repository that
// installed it (docs/plans/providers-and-samples.md has where that is going).
// The catalog serves both today, from two roots.
package samples

import (
	"embed"
	"io/fs"
)

// Authority is the one authority every shipped sample is authored under.
const Authority = "samples.substrate.reamde.dev"

// The pattern is part of the contract: a package directory it misses is a
// sample that silently stops existing in production while every other test
// still passes. kinds_test.go holds it to the tree on disk.
//
//go:embed all:authority.yaml all:calendar all:commerce all:firecrawl all:fitness
//go:embed all:food all:health all:journal all:llm all:messaging all:notes
//go:embed all:pebble all:people all:places all:routines all:scheduling
//go:embed all:tasks all:web
var files embed.FS

// Samples is the whole sample tree, as a filesystem whose root holds one
// directory per package — the shape the catalog reads.
func Samples() fs.FS { return files }
