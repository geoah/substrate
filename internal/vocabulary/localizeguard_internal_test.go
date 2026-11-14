package vocabulary

// A localized declaration spells the repository's name at no reference
// position, in any form the loader accepts there, and is unchanged at every
// other position, whatever a value there looks like (decision record 0259,
// positions.go).

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

const madeUp = "made-up.example.org"

// EVERY KEY THE LOADER ACCEPTS IS CLASSED. The dialect's key sets, as the
// golden records them, are each a declaration's data the table classes key
// by key, or a set notDeclarationData names; a set or a key the table does
// not class fails here, and so does a key the property sets class two ways.
func TestEveryDialectKeyIsClassed(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "dialect.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Keys map[string][]string `json:"keys"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for set, keys := range golden.Keys {
		if notDeclarationData[set] {
			continue
		}
		classes, ok := declarationPositions[set]
		if !ok {
			t.Errorf("the key set %s is not classed: add it to declarationPositions, or to notDeclarationData if it is no declaration's data", set)
			continue
		}
		for _, k := range keys {
			if _, ok := classes[k]; !ok {
				t.Errorf("%s.%s is not classed: say whether it names something (atRef) or not", set, k)
			}
		}
		for k := range classes {
			if !slices.Contains(keys, k) {
				t.Errorf("%s.%s is classed and the dialect does not hold it: take it out", set, k)
			}
		}
	}
	for set := range declarationPositions {
		if _, ok := golden.Keys[set]; !ok {
			t.Errorf("declarationPositions classes %s, which the dialect does not hold", set)
		}
	}
	seen := map[string]keyClass{}
	for _, set := range propertyKeySets {
		for k, c := range declarationPositions[set] {
			if prev, ok := seen[k]; ok && prev != c {
				t.Errorf("the property key %s is classed %v in one property key set and %v in %s", k, prev, c, set)
			}
			seen[k] = c
		}
	}
}

// namedAtReferences lists where v, an object of the key set keys, still
// spells name at a reference position.
func namedAtReferences(keys string, v any, name, at string) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for i, x := range t {
			out = append(out, namedAtReferences(keys, x, name, fmt.Sprintf("%s[%d]", at, i))...)
		}
	case map[string]any:
		for k, x := range t {
			c, ok := classOf(keys, k)
			if !ok {
				continue
			}
			where := at + "." + k
			switch c.pos {
			case posRef, posAuthority:
				if raw, _ := json.Marshal(x); strings.Contains(string(raw), name) {
					out = append(out, fmt.Sprintf("%s: %s", where, raw))
				}
			case posPackages:
				m, _ := x.(map[string]any)
				for pkg := range m {
					if strings.Contains(pkg, name) {
						out = append(out, fmt.Sprintf("%s: the key %q", where, pkg))
					}
				}
			case posObject:
				out = append(out, namedAtReferences(c.keys, x, name, where)...)
			case posNamed:
				m, _ := x.(map[string]any)
				for n, y := range m {
					out = append(out, namedAtReferences(c.keys, y, name, where+"."+n)...)
				}
			}
		}
	}
	return out
}

// heldUnder is v with every name under pkg (`<authority>/<package>`) moved
// under madeUp, wherever it is spelled, and an `authority` of the package's
// own made madeUp: the package as a repository would hold it had it declared
// it under its own name.
func heldUnder(v any, authority, pkg string) any {
	switch t := v.(type) {
	case string:
		local := madeUp + "/" + strings.TrimPrefix(pkg, authority+"/")
		if t == pkg {
			return local
		}
		return strings.ReplaceAll(t, pkg+"/", local+"/")
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "authority" && val == authority {
				out[k] = madeUp
				continue
			}
			out[heldUnder(k, authority, pkg).(string)] = heldUnder(val, authority, pkg)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = heldUnder(val, authority, pkg)
		}
		return out
	}
	return v
}

// EVERY SHIPPED DECLARATION, held under a repository's own name and
// localized, spells that name at no reference position. The core package is
// the substrate's own and is never a repository's.
func TestNoShippedDeclarationKeepsItsAuthorityAtAReference(t *testing.T) {
	checked := 0
	for _, fsys := range []fs.FS{kinds.All(), samples.Samples()} {
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".yaml" {
				return err
			}
			raw, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			docs, err := ParseStream(raw)
			if err != nil {
				return nil // a bundle's delivery wiring, not a declaration
			}
			for _, doc := range docs {
				authority, _ := doc.Data["authority"].(string)
				pkg, _ := doc.Data["package"].(string)
				kind := CoreKind(doc.Kind)
				root, declared := declarationRoots[kind]
				if !declared || authority == "" || pkg == "" || authority+"/"+pkg == PackageCore {
					continue
				}
				held := heldUnder(doc.Data, authority, authority+"/"+pkg)
				local, err := LocalizeDeclaration(kind, held, madeUp)
				if err != nil {
					t.Errorf("%s %s: %v", path, doc.ID, err)
					continue
				}
				left := namedAtReferences(root, local, madeUp, "data")
				sort.Strings(left)
				for _, l := range left {
					t.Errorf("%s %s keeps the name at a reference: %s", path, doc.ID, l)
				}
				checked++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no shipped declaration was checked")
	}
}

// classedKeys is every key of the table at one of pos, as key set and key.
func classedKeys(pos ...position) [][2]string {
	var out [][2]string
	for set, classes := range declarationPositions {
		for k, c := range classes {
			if slices.Contains(pos, c.pos) {
				out = append(out, [2]string{set, k})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0]+"."+out[i][1] < out[j][0]+"."+out[j][1] })
	return out
}

// EVERY FORM AT EVERY REFERENCE POSITION. A name, a glob, a trait pattern, a
// record path and a reference value's object (by name and by record path,
// with a link property beside it), alone and in a list, at every key the
// table classes as a reference, comes back under `~`, its link property as
// it was.
func TestEveryFormOfAReferenceIsLocalized(t *testing.T) {
	h := madeUp
	forms := []any{
		h + "/tasks/task",
		h + "/tasks/*",
		h + "/tasks/dated(start)",
		CoreKind(DocKind) + "/" + h + "/tasks/task",
		map[string]any{"ref": h + "/tasks/task"},
		map[string]any{"ref": CoreKind(DocKind) + "/" + h + "/tasks/task"},
		map[string]any{"ref": CoreKind(DocAgent) + "/" + h + "/tasks/helper", "background": true},
	}
	w := declarationWalker{homes: []string{h}}
	for _, at := range classedKeys(posRef, posAuthority) {
		for i, form := range forms {
			for _, value := range []any{form, []any{form, form}} {
				got, err := w.object(at[0], map[string]any{at[1]: value})
				if err != nil {
					t.Fatalf("%s.%s, form %d: %v", at[0], at[1], i, err)
				}
				for _, l := range namedAtReferences(at[0], got, h, at[0]) {
					t.Errorf("form %d keeps the name: %s", i, l)
				}
				if m, ok := form.(map[string]any); ok && m["background"] == true {
					entry := got.(map[string]any)[at[1]]
					if list, ok := entry.([]any); ok {
						entry = list[0]
					}
					if entry.(map[string]any)["background"] != true {
						t.Errorf("%s.%s, form %d: the link property went", at[0], at[1], i)
					}
				}
			}
		}
	}
}

// THE MIRROR: NOTHING ELSE MOVES. The repository's name followed by a slash
// and more, a URL with and without a scheme, at every key the table does not
// class as a reference (an agent's model and models, a function's network, a
// default, an enum's values, a description), comes back unchanged.
func TestNoValueOutsideAReferenceIsLocalized(t *testing.T) {
	h := madeUp
	values := []any{
		h + "/models/custom",
		h + "/tasks/task",
		"https://" + h + "/tasks/task",
		h + ":443/api",
		[]any{h + "/a/b", "see " + h + "/a/b"},
		map[string]any{"ref": h + "/tasks/task"},
	}
	w := declarationWalker{homes: []string{h}}
	for _, at := range classedKeys(posWord, posText) {
		for i, v := range values {
			in := map[string]any{at[1]: v}
			got, err := w.object(at[0], in)
			if err != nil {
				t.Fatalf("%s.%s, value %d: %v", at[0], at[1], i, err)
			}
			if !reflect.DeepEqual(got, in) {
				t.Errorf("%s.%s moved %v to %v", at[0], at[1], v, got.(map[string]any)[at[1]])
			}
		}
	}
	agent, err := LocalizeDeclaration(CoreKind(DocAgent), map[string]any{
		"authority": h, "package": "lab", "model": h + "/models/custom", "models": []any{h + "/models/other"},
		"subagents": []any{h + "/lab/helper"},
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	m := agent.(map[string]any)
	if m["model"] != h+"/models/custom" || !reflect.DeepEqual(m["models"], []any{h + "/models/other"}) ||
		m["authority"] != Local || !reflect.DeepEqual(m["subagents"], []any{"~/lab/helper"}) {
		t.Errorf("an agent localized to %v", m)
	}
}
