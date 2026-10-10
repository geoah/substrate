package vocabulary

import (
	"errors"
	"sort"

	"github.com/geoah/substrate/internal/substrate"
)

// ParseDocuments parses raw envelope maps into documents, every document's
// problems collected into one ValidationError in input order. The engine's
// vocabulary doors and `substratectl validate` both parse through it, so a
// document the server refuses for its envelope is refused offline with the
// same lines.
func ParseDocuments(raw []map[string]any) ([]Document, error) {
	var docs []Document
	var problems []string
	for _, r := range raw {
		d, err := DocumentFromMap(r)
		if err != nil {
			var ve *substrate.ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, ve.Problems...)
				continue
			}
			return nil, err
		}
		docs = append(docs, d)
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	return docs, nil
}

// BuildEachPackage builds docs one package at a time: the documents are
// grouped by DeclaredPackage and each group goes through BuildPackages on its
// own, in package-name order, with the origin sourceOf names for it (the one
// rule keyed on the origin, `runtime: host`, reads it). Every group's problems
// are collected into one ValidationError: each group's sorted, as
// BuildPackages sorts them, and the groups in name order.
//
// The engine's vocabulary admission and `substratectl validate` both build
// through it, so the problems a package's declarations carry, and their
// order, are the same offline as in the server's 422.
func BuildEachPackage(docs []Document, sourceOf func(pkg string) string) ([]*Package, error) {
	byPackage := map[string][]Document{}
	for _, d := range docs {
		g := d.DeclaredPackage()
		byPackage[g] = append(byPackage[g], d)
	}
	names := make([]string, 0, len(byPackage))
	for g := range byPackage {
		names = append(names, g)
	}
	sort.Strings(names)
	var built []*Package
	var problems []string
	for _, g := range names {
		gs, err := BuildPackages(byPackage[g], sourceOf(g))
		if err != nil {
			var ve *substrate.ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, ve.Problems...)
				continue
			}
			return nil, err
		}
		built = append(built, gs...)
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	return built, nil
}
