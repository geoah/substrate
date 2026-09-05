package vocabulary

import "fmt"

// A SUGGESTED MAPPING is a mapping document whose source kind lives in another
// package: the shape a SAMPLE ships to project a provider's mirror onto a kind
// of its own (decision records 0048 and 0049). Nothing in a declaration marks
// one (a dialect key is reserved by name, never invented for a case, record
// 0020), so it is read off the document: the declaring package owns
// `to`, and `from` names a package this repository may not hold.
//
// The two functions below are the doc-level half both doors and the test
// helpers share. Whether the repository HOLDS the named package is the
// caller's question, asked of a dataset or a registry; these only read and
// prune decoded documents.

// SuggestedMapping is one such document: its id, both kind references, and the
// PACKAGE `from` lives in, which is what has to be installed for the mapping
// to resolve.
type SuggestedMapping struct {
	ID      string
	From    string
	To      string
	Package string
}

// SuggestedMappings lists the mapping documents whose source kind lives
// outside the package that declares them, in document order. A mapping that
// owns both ends is not one: it resolves against its own closure and lands
// with it, always.
func SuggestedMappings(docs []map[string]any) []SuggestedMapping {
	var out []SuggestedMapping
	for _, d := range docs {
		if KindName(mstr(d, "kind")) != DocRecordMapping {
			continue
		}
		data := mmap(d, "data")
		authority, pkg := mstr(data, "authority"), mstr(data, "package")
		from := ReferentID(data["from"], CoreKind(DocKind))
		if authority == "" || pkg == "" || from == "" ||
			KindPackage(from) == PackageRef(authority, pkg) {
			continue
		}
		out = append(out, SuggestedMapping{
			ID:      mstr(mmap(d, "metadata"), "id"),
			From:    from,
			To:      ReferentID(data["to"], CoreKind(DocKind)),
			Package: KindPackage(from),
		})
	}
	return out
}

// WithoutMappings returns docs with the named MAPPING documents removed and
// every bundle document's `installs:` pruned to match. Both halves are
// required: `installs:` must name exactly the package's declared members, both
// directions (bundle.go), so dropping a document without its entry is a
// closure the loader refuses.
//
// It copies what it changes and leaves the originals alone: the catalog holds
// one parsed copy of each shipped closure and serves every repository from it.
func WithoutMappings(docs []map[string]any, drop map[string]bool) []map[string]any {
	if len(drop) == 0 {
		return docs
	}
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		kind := KindName(mstr(d, "kind"))
		if kind == DocRecordMapping && drop[mstr(mmap(d, "metadata"), "id")] {
			continue
		}
		if kind != DocBundle {
			out = append(out, d)
			continue
		}
		out = append(out, prunedInstalls(d, drop))
	}
	return out
}

// prunedInstalls copies one bundle document with the dropped ids gone from its
// `installs:` list. The copy is two maps deep, the document and its `data`,
// which is exactly what changes; everything below is shared, unmodified.
func prunedInstalls(d map[string]any, drop map[string]bool) map[string]any {
	data := mmap(d, "data")
	kept := make([]any, 0, len(mslice(data, "installs")))
	for _, iv := range mslice(data, "installs") {
		if drop[fmt.Sprint(iv)] {
			continue
		}
		kept = append(kept, iv)
	}
	newData := make(map[string]any, len(data))
	for k, v := range data {
		newData[k] = v
	}
	newData["installs"] = kept
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = v
	}
	out["data"] = newData
	return out
}
