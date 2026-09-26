package engine

// THE FLOOR BINDS A COPY (decision record 0100).
//
// A bundle's `requiresAtLeast` names the least version of a required package
// the closure declares against (record 0070), and the loader compares it
// against the stored package version on every door. The comparison means
// something when the held package is a COPY of the required one: an imported
// sample's version line is the shipped sample's, so "people at 4 or later"
// says which shape the closure was verified against. It means nothing when the
// held package is one the repository declared by hand under the required
// name: `ada.example.com/people` at version 1 is the user's own vocabulary,
// its number is unrelated to the sample's, and admission holds the closure's
// pins (spelled in full, record 0098) against the kinds it actually declares.
//
// So a floor whose package is under the repository's own authority, held,
// and stamped with no origin is dropped from the bundle document before
// admission, and the landed bundle says what is true of it here: it requires
// that package by name. A floor on a copy (origin stamped), on a package the
// batch itself declares, on a package under another authority, or on one the
// repository does not hold is left for the loader to compare or refuse.

import (
	"context"
	"maps"

	"github.com/geoah/substrate/internal/vocabulary"
)

// relaxFloorsOnOwnPackages returns docs with every floor a hand-declared
// package of this repository would fail dropped. Documents are copied where
// they change; the caller's slice and maps are left as they were.
func (ds *dataset) relaxFloorsOnOwnPackages(ctx context.Context, docs []vocabulary.Document) ([]vocabulary.Document, error) {
	home := ds.info.Authority
	if home == "" {
		return docs, nil
	}
	declared := map[string]bool{}
	for _, d := range docs {
		if d.Kind == vocabulary.DocPackage || d.Kind == vocabulary.DocBundle {
			declared[d.DeclaredPackage()] = true
		}
	}
	reg := ds.registry()
	out := docs
	for i, d := range docs {
		if d.Kind != vocabulary.DocBundle {
			continue
		}
		floors, ok := d.Data["requiresAtLeast"].(map[string]any)
		if !ok || len(floors) == 0 {
			continue
		}
		var drop []string
		for _, pkg := range sortedKeys(floors) {
			authority, word := vocabulary.SplitPackageRef(pkg)
			if authority != home || word == "" || declared[pkg] {
				continue
			}
			if _, held := reg.PackageByName(pkg); !held {
				continue
			}
			stamp, err := ds.packageStamp(ctx, ds.db, pkg)
			if err != nil {
				return nil, err
			}
			if stamp.origin == "" {
				drop = append(drop, pkg)
			}
		}
		if len(drop) == 0 {
			continue
		}
		if len(out) == len(docs) && &out[0] == &docs[0] {
			out = append([]vocabulary.Document(nil), docs...)
		}
		data := maps.Clone(d.Data)
		kept := maps.Clone(floors)
		for _, pkg := range drop {
			delete(kept, pkg)
		}
		if len(kept) == 0 {
			delete(data, "requiresAtLeast")
		} else {
			data["requiresAtLeast"] = kept
		}
		nd := d
		nd.Data = data
		out[i] = nd
	}
	return out, nil
}
