package catalog

import (
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// KindRenames names every kind the closure declares with `movedFrom`: a
// rename, which a published package may not make (decision 0269). Other
// repositories hold records of a published kind under its name, and only the
// boot upgrade of the packages a server ships moves them, so a publisher
// deprecates the old kind and declares the new one beside it.
func (b *Bundle) KindRenames() []string {
	var out []string
	for _, d := range b.vocabularyDocs {
		if mstr(d, "kind") != vocabulary.CoreKind(vocabulary.DocKind) {
			continue
		}
		if from := mstr(mmap(d, "data"), "movedFrom"); from != "" {
			out = append(out, fmt.Sprintf("kind %s: movedFrom %s", docID(d), from))
		}
	}
	return out
}
