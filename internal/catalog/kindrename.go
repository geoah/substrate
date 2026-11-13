package catalog

import (
	"fmt"
	"sort"

	"github.com/geoah/substrate/internal/vocabulary"
)

// KindRenames names every kind the closure declares with `movedFrom`: a
// rename, which a published package may not make (decision 0269). Other
// repositories hold records of a published kind under its name, and only the
// boot upgrade of the packages a server ships moves them, so a publisher
// deprecates the old kind and declares the new one beside it.
func (b *Bundle) KindRenames() []string {
	var out []string
	for _, r := range b.kindRenames() {
		out = append(out, fmt.Sprintf("kind %s: movedFrom %s", r[0], r[1]))
	}
	return out
}

// RenameRefusals is the refusal of each kind a publisher's closure renames,
// in the engine's words. A closure a publisher's document serves is refused
// it whether or not the repository holds the kind it names: the engine
// refuses a move only where the old kind is there to move (decision 0269).
func (b *Bundle) RenameRefusals() []string {
	var out []string
	for _, r := range b.kindRenames() {
		out = append(out, vocabulary.KindRenameRefusal(r[0], r[1]))
	}
	return out
}

// kindRenames is each kind the closure declares with `movedFrom`, and the
// kind it names.
func (b *Bundle) kindRenames() [][2]string {
	var out [][2]string
	for _, d := range b.vocabularyDocs {
		if mstr(d, "kind") != vocabulary.CoreKind(vocabulary.DocKind) {
			continue
		}
		if from := mstr(mmap(d, "data"), "movedFrom"); from != "" {
			out = append(out, [2]string{docID(d), from})
		}
	}
	return out
}

// RemovedKinds names every kind a release of a package published before
// (old) declares and next does not. A published kind stays: other
// repositories hold its records under its name, so a publisher marks it
// `deprecated: true` and keeps it declared, and moving it to another package
// or renaming the package leaves it declared here too (decision 0269). A
// property's removal or rename is another thing, and stays a narrowing for
// the owner to review.
func RemovedKinds(old, next *Bundle) []string {
	kept := map[string]bool{}
	for _, d := range next.vocabularyDocs {
		if mstr(d, "kind") == vocabulary.CoreKind(vocabulary.DocKind) {
			kept[docID(d)] = true
		}
	}
	var out []string
	for _, d := range old.vocabularyDocs {
		if mstr(d, "kind") == vocabulary.CoreKind(vocabulary.DocKind) && !kept[docID(d)] {
			out = append(out, docID(d))
		}
	}
	sort.Strings(out)
	return out
}
