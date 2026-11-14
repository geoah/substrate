package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Every shipped sample copies its names into `~` and nothing else (decision
// records 0048 and 0259). Wherever a sample's authority occurs in what ships,
// the copy moves it because it is a name: a declaration's id, its
// `authority` or a name key, an actor's authority, a kind reference spelled
// in a function's source or an agent's prompt, or, in a record the closure
// carries, its kind, its id or a position the write reads as a name. An
// occurrence the copy would leave, and a change into anything but the name
// moved, fail. The rule over code texts is textual, and this holds it exact
// for what ships; nothing but the binary's own samples is ever copied.
func TestEveryShippedSampleCopiesItsNamesAndNothingElse(t *testing.T) {
	ctx := context.Background()
	c := loadCatalog(t)
	ds := newDataset(t)
	byID := map[string]*catalog.Bundle{}
	for _, b := range c.Bundles() {
		byID[b.ID] = b
	}
	taken := map[string]bool{}
	var take func(b *catalog.Bundle)
	take = func(b *catalog.Bundle) {
		if taken[b.ID] {
			return
		}
		taken[b.ID] = true
		for _, req := range b.Requires {
			if rb, ok := byID[req]; ok && rb.KeptBy == substrate.KeptByRepository {
				take(rb)
			}
		}
		if _, _, err := c.Import(ctx, substrate.ActorPerson, b.ID, ds); err != nil {
			t.Fatalf("copy %s: %v", b.ID, err)
		}
	}
	samples := 0
	for _, b := range c.Bundles() {
		if b.KeptBy != substrate.KeptByRepository {
			continue
		}
		samples++
		take(b)
		from := b.Authority
		shipped, data := b.ShippedDocs()

		// The declarations, as the copy moves them.
		copied, err := vocabulary.LocalizeAuthority(shipped, from, homeAuthority)
		if err != nil {
			t.Fatalf("copy %s: %v", b.ID, err)
		}
		var kept []map[string]any
		for _, d := range shipped {
			if d["kind"] == vocabulary.CoreKind(vocabulary.DocAuthority) && docID(d) == from {
				continue
			}
			kept = append(kept, d)
		}
		if len(kept) != len(copied) {
			t.Fatalf("%s: the copy holds %d documents, want the %d it ships beside its authority's", b.ID, len(copied), len(kept))
		}
		for i, d := range kept {
			kind, _ := d["kind"].(string)
			holdCopy(t, sampleCopy{from: from, doc: kind + " " + docID(d), kind: kind}, "", d, copied[i])
		}

		// The records it carries, as the install stored them.
		for _, d := range data {
			kind, _ := d["kind"].(string)
			k := vocabulary.LocalizeRef(kind, from)
			id := vocabulary.LocalizeID(k, docID(d), from)
			rec, err := ds.Get(ctx, k, id)
			if err != nil {
				t.Errorf("%s: the record %s %s is not at %s %s: %v", b.ID, kind, docID(d), k, id, err)
				continue
			}
			stored, _ := json.Marshal(rec.Properties)
			if mentions(string(stored), from) {
				t.Errorf("%s: the copy leaves %s in %s %s: %s", b.ID, from, k, id, stored)
			}
			props, _ := d["data"].(map[string]any)["properties"].(map[string]any)
			holdRecord(t, b.ID+" "+k+" "+id, from, nil, props, rec.Properties)
		}
	}
	if samples == 0 {
		t.Fatal("the catalog ships no sample")
	}
}

// sampleCopy is where holdCopy stands: the sample's authority, the document,
// its kind, and whether the value is a text its code reads.
type sampleCopy struct {
	from, doc, kind string
	code            bool
}

// holdCopy holds one shipped value to its copy.
func holdCopy(t *testing.T, at sampleCopy, path string, shipped, copied any) {
	t.Helper()
	switch s := shipped.(type) {
	case string:
		c, _ := copied.(string)
		if mentions(c, at.from) {
			t.Errorf("%s %s: the copy leaves %s in %q", at.doc, path, at.from, c)
			return
		}
		if c == s || movedName(at, path, s, c) {
			return
		}
		t.Errorf("%s %s: the copy changes %q into %q, which is no name moved", at.doc, path, s, c)
	case map[string]any:
		cm, ok := copied.(map[string]any)
		if !ok || len(cm) != len(s) {
			t.Errorf("%s %s: the copy is %v, want %d keys", at.doc, path, copied, len(s))
			return
		}
		for k, v := range s {
			ck := k
			if _, ok := cm[k]; !ok {
				ck = vocabulary.LocalizeRef(k, at.from)
				if _, ok := cm[ck]; !ok || !strings.HasPrefix(k, at.from+"/") {
					t.Errorf("%s %s: the key %q is missing from the copy", at.doc, path, k)
					continue
				}
			}
			inner := at
			inner.code = at.code || (path == ".data" && codeText(at.kind, k))
			holdCopy(t, inner, path+"."+k, v, cm[ck])
		}
	case []any:
		cl, ok := copied.([]any)
		if !ok || len(cl) != len(s) {
			t.Errorf("%s %s: the copy is %v, want %d items", at.doc, path, copied, len(s))
			return
		}
		for i := range s {
			holdCopy(t, at, fmt.Sprintf("%s[%d]", path, i), s[i], cl[i])
		}
	default:
		if !reflect.DeepEqual(shipped, copied) {
			t.Errorf("%s %s: the copy is %v, want %v", at.doc, path, copied, shipped)
		}
	}
}

// movedName reports whether c is s with its name moved: the authority
// itself, a name under it, an actor's authority, or the kind references
// spelled in a code text.
func movedName(at sampleCopy, path, s, c string) bool {
	switch {
	case at.code:
		return c == vocabulary.LocalizeKindRefs(s, at.from)
	case s == at.from:
		return c == vocabulary.Local
	case strings.HasPrefix(s, at.from+"/"):
		return c == vocabulary.Local+s[len(at.from):]
	case at.kind == vocabulary.CoreKind(vocabulary.DocActor) && path == ".metadata.id":
		return c == strings.Replace(s, ":"+at.from+":", ":"+homeAuthority+":", 1)
	}
	return false
}

// codeText reports whether a declaration's data key is a text its code reads
// by kind reference.
func codeText(kind, key string) bool {
	switch kind {
	case vocabulary.CoreKind(vocabulary.DocFunction):
		return key == "source" || key == "modules"
	case vocabulary.CoreKind(vocabulary.DocAgent):
		return key == "prompt"
	}
	return false
}

// holdRecord holds every shipped string of a carried record that mentions the
// sample's authority to what the install stored at its place: the record
// path or the name with the authority moved, a reference's under its `ref`.
func holdRecord(t *testing.T, doc, from string, path []any, shipped, stored any) {
	t.Helper()
	switch s := shipped.(type) {
	case string:
		if !mentions(s, from) {
			return
		}
		if m, ok := stored.(map[string]any); ok {
			stored = m[vocabulary.ReferenceValueKey]
		}
		if want := vocabulary.LocalizePath(s, from); stored != want {
			t.Errorf("%s %v: %q is stored as %v, want %q", doc, path, s, stored, want)
		}
	case map[string]any:
		sm, _ := stored.(map[string]any)
		for k, v := range s {
			holdRecord(t, doc, from, append(append([]any{}, path...), k), v, sm[k])
		}
	case []any:
		sl, _ := stored.([]any)
		for i, v := range s {
			var e any
			if i < len(sl) {
				e = sl[i]
			}
			holdRecord(t, doc, from, append(append([]any{}, path...), i), v, e)
		}
	}
}

// mentions reports whether s spells the authority as a whole name.
func mentions(s, authority string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9._-])` + regexp.QuoteMeta(authority) + `($|[^A-Za-z0-9._-])`).MatchString(s)
}

// docID is a document's metadata.id.
func docID(d map[string]any) string {
	m, _ := d["metadata"].(map[string]any)
	id, _ := m["id"].(string)
	return id
}
