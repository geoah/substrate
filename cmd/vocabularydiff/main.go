// vocabularydiff diffs two shipped trees at the document level and refuses a
// changed declaration whose effective version did not move forward. It is the
// CI half of the upgrade contract: the boot upgrade (engine seed.go) and the
// bundle upgrade preview (engine PlanBundleUpgrade) both key on `version`, so
// a definition that changes under an unmoved version is an upgrade no
// repository will ever receive. The rule it enforces, per declaration:
//
//   - definition changed (data compared minus `version`): the effective
//     version must be NEWER in head, through the one comparator
//     (vocabulary.CompareVersions). A kind may bump its own `data.version`;
//     everything else moves with its PACKAGE's.
//   - definition unchanged: the effective version must not move BACKWARD,
//     because a repository already holding the newer version would simply
//     keep it (never a downgrade) and the tree would stop converging.
//   - declaration deleted while its package remains: the package version
//     must bump, so the whole-package replace that prunes it reads as an
//     upgrade. A directory deleted whole is a package leaving the catalog and
//     needs nothing.
//   - a DATA document changed, added or removed (triggers.yaml, the delivery
//     wiring): the package version must bump. Data documents carry no version
//     of their own, and the install upserts them along with the closure — so
//     without a bump the wiring change is one no repository is ever offered.
//   - a `retired:` entry (decision 0055) the base carries must stay in head,
//     and head may not declare a kind, property, enum value or state a
//     retirement names. The boot upgrade refuses both at every repository's
//     open; this refuses them before the tree ships.
//
// Comment-only edits decode to identical data and pass free.
//
// A tree is walked to whatever depth it nests its packages: kinds/ holds
// <authority>/<package>/ and samples/ holds <package>/, and a package
// directory is any directory holding manifests.
//
// Usage: vocabularydiff <base-dir> <head-dir>
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: vocabularydiff <base-dir> <head-dir>")
		os.Exit(2)
	}
	base, err := loadTree(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "vocabularydiff: base tree: %v\n", err)
		os.Exit(2)
	}
	head, err := loadTree(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "vocabularydiff: head tree: %v\n", err)
		os.Exit(2)
	}
	violations := diffTrees(base, head)
	if len(violations) == 0 {
		return
	}
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	fmt.Fprintf(os.Stderr, "\nvocabularydiff: %d violation(s). A changed declaration ships a changed version: bump the kind's own `data.version`, or the package's `data.version` in its bundle.yaml (see CLAUDE.md).\n", len(violations))
	os.Exit(1)
}

// decl is one schema document as the diff sees it: its manifest kind (the
// short name: "package", "kind", "trait", ...), id, the package it declares
// into, the file it came from, and its raw data map.
type decl struct {
	kind string
	id   string
	pkg  string
	file string
	data map[string]any
}

func declKey(kind, id string) string { return kind + "\x00" + id }

// tree is one shipped tree, parsed: every declaration keyed by (kind, id),
// plus each package's declared version.
type tree struct {
	decls map[string]decl
	// packageVersion is the package document's data.version, defaulted
	// exactly as the loader defaults it.
	packageVersion map[string]int64
	// dirs is the set of package directories present, so a deletion can tell
	// "declaration removed" from "package removed whole".
	dirs map[string]bool
	// dataDocs is each directory's delivery wiring (the non-declaration
	// documents), keyed by directory then by document, so a wiring change can
	// demand the package bump that carries it to a repository.
	dataDocs map[string]map[string]any
	// dirPackage names the package a directory declares, so a wiring change
	// can find the version that carries it.
	dirPackage map[string]string
}

func loadTree(root string) (*tree, error) {
	t := &tree{
		decls:          map[string]decl{},
		packageVersion: map[string]int64{},
		dirs:           map[string]bool{},
		dataDocs:       map[string]map[string]any{},
		dirPackage:     map[string]string{},
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".yaml" {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		t.dirs[path.Dir(rel)] = true
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := t.loadFile(rel, raw); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (t *tree) loadFile(rel string, raw []byte) error {
	dir := path.Dir(rel)
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(m) == 0 {
			continue
		}
		ref, _ := m["kind"].(string)
		name := vocabulary.KindName(ref)
		if vocabulary.KindPackage(ref) != vocabulary.PackageCore || !vocabulary.VocabularyDocumentKind(name) {
			// A data document: the delivery wiring the install upserts beside
			// the closure. It carries no version, so it is diffed WHOLE and
			// held to its directory's package version.
			meta, _ := m["metadata"].(map[string]any)
			id, _ := meta["id"].(string)
			if t.dataDocs[dir] == nil {
				t.dataDocs[dir] = map[string]any{}
			}
			t.dataDocs[dir][ref+"\x00"+id] = m["data"]
			continue
		}
		meta, _ := m["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		if id == "" {
			return fmt.Errorf("%s document without metadata.id", name)
		}
		data, _ := m["data"].(map[string]any)
		pkg := ""
		if authority, _ := data["authority"].(string); authority != "" {
			if p, _ := data["package"].(string); p != "" {
				pkg = vocabulary.PackageRef(authority, p)
			}
		}
		switch name {
		case vocabulary.DocPackage:
			pkg = id
			version, _ := vocabulary.VersionValue(data["version"])
			if version == 0 {
				version = vocabulary.DefaultVersion
			}
			t.packageVersion[id] = version
			t.dirPackage[dir] = id
		case vocabulary.DocAuthority:
			// An authority row owns packages and carries no closure version,
			// so it is diffed like any declaration under its own id.
			pkg = id
		}
		t.decls[declKey(name, id)] = decl{kind: name, id: id, pkg: pkg, file: rel, data: data}
	}
	return nil
}

// effectiveVersion is the version a declaration would project with: a kind's
// own data.version where it declares one, else its package's, exactly as the
// engine's projection defaults it (packageDeclarations).
func (t *tree) effectiveVersion(d decl) int64 {
	switch d.kind {
	case vocabulary.DocKind, vocabulary.DocPackage, vocabulary.DocAuthority:
		// Each carries a version of its own: a kind may pin one, and a header
		// row IS one.
		if v, _ := vocabulary.VersionValue(d.data["version"]); v != 0 {
			return v
		}
	}
	if v, ok := t.packageVersion[d.pkg]; ok {
		return v
	}
	return vocabulary.DefaultVersion
}

func diffTrees(base, head *tree) []string {
	var out []string
	keys := make([]string, 0, len(base.decls))
	for k := range base.decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := base.decls[k]
		h, exists := head.decls[k]
		if !exists {
			if !head.dirs[path.Dir(b.file)] {
				continue // the package left the tree whole
			}
			bv, hv := base.packageVersion[b.pkg], head.packageVersion[b.pkg]
			if hv == 0 || vocabulary.CompareVersions(hv, bv) > 0 {
				continue // the prune rides a package bump (or the package left too)
			}
			out = append(out, fmt.Sprintf("%s: %s %s was removed but package %s stays at %d; bump the package version so the prune is an upgrade",
				b.file, b.kind, b.id, b.pkg, bv))
			continue
		}
		bv, hv := base.effectiveVersion(b), head.effectiveVersion(h)
		switch {
		case !reflect.DeepEqual(minusVersion(b.data), minusVersion(h.data)):
			if vocabulary.CompareVersions(hv, bv) <= 0 {
				out = append(out, fmt.Sprintf("%s: %s %s changed but its version stays at %d; bump it past %d",
					h.file, h.kind, h.id, hv, bv))
			}
		case vocabulary.CompareVersions(hv, bv) < 0:
			out = append(out, fmt.Sprintf("%s: %s %s moved backward from %d to %d; a repository already at %d keeps it (never a downgrade), so the tree stops converging",
				h.file, h.kind, h.id, bv, hv, bv))
		}
	}
	out = append(out, dataDocViolations(base, head)...)
	return append(out, retirementViolations(base, head)...)
}

// retirementViolations holds the tree to decision 0055: a `retired:` entry the
// base tree carries stays in head, and no declaration in head names a retired
// kind, property, enum value or state. The boot upgrade refuses both at the
// open of every repository; this refuses them before the tree ships, where
// the author can still choose another name.
func retirementViolations(base, head *tree) []string {
	var out []string
	keys := make([]string, 0, len(head.decls))
	for k := range head.decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h := head.decls[k]
		b, inBase := base.decls[k]
		switch h.kind {
		case vocabulary.DocPackage:
			var stored []string
			if inBase {
				stored = anyStrings(retiredBlock(b.data)["kinds"])
			}
			current := anyStrings(retiredBlock(h.data)["kinds"])
			for _, name := range missing(stored, current) {
				out = append(out, fmt.Sprintf("%s: package %s drops retired kind name %q; a retirement is permanent", h.file, h.id, name))
			}
			for _, name := range union(stored, current) {
				if kd, declared := head.decls[declKey(vocabulary.DocKind, h.id+"/"+name)]; declared {
					out = append(out, fmt.Sprintf("%s: kind %s is retired in package %s; a retired name is never declared again", kd.file, kd.id, h.id))
				}
			}
		case vocabulary.DocKind:
			var stored map[string]any
			if inBase {
				stored = retiredBlock(b.data)
			}
			current := retiredBlock(h.data)
			props, _ := h.data["properties"].(map[string]any)
			for _, name := range missing(anyStrings(stored["properties"]), anyStrings(current["properties"])) {
				out = append(out, fmt.Sprintf("%s: kind %s drops retired property %q; a retirement is permanent", h.file, h.id, name))
			}
			for _, name := range union(anyStrings(stored["properties"]), anyStrings(current["properties"])) {
				if _, declared := props[name]; declared {
					out = append(out, fmt.Sprintf("%s: kind %s declares retired property %q; a retired name is never declared again", h.file, h.id, name))
				}
			}
			for _, key := range []string{"values", "states"} {
				sm, _ := stored[key].(map[string]any)
				cm, _ := current[key].(map[string]any)
				for _, pname := range union(mapKeys(sm), mapKeys(cm)) {
					for _, v := range missing(anyStrings(sm[pname]), anyStrings(cm[pname])) {
						out = append(out, fmt.Sprintf("%s: kind %s drops retired %s %q of property %q; a retirement is permanent", h.file, h.id, key, v, pname))
					}
					p, _ := props[pname].(map[string]any)
					live := declaredWords(p[key])
					for _, v := range union(anyStrings(sm[pname]), anyStrings(cm[pname])) {
						if live[v] {
							out = append(out, fmt.Sprintf("%s: kind %s declares retired %s %q on property %q; a retired name is never declared again", h.file, h.id, key, v, pname))
						}
					}
				}
			}
		}
	}
	return out
}

// retiredBlock reads a declaration's `retired:` map, empty when absent.
func retiredBlock(data map[string]any) map[string]any {
	m, _ := data["retired"].(map[string]any)
	return m
}

// declaredWords reads an enum's `values:` or a machine's `states:` as a set: a
// value is a bare word or a `{value, label}` mapping.
func declaredWords(v any) map[string]bool {
	out := map[string]bool{}
	items, _ := v.([]any)
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out[fmt.Sprint(m["value"])] = true
			continue
		}
		out[fmt.Sprint(item)] = true
	}
	return out
}

func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// missing lists the members of stored that current no longer carries.
func missing(stored, current []string) []string {
	keep := map[string]bool{}
	for _, s := range current {
		keep[s] = true
	}
	var out []string
	for _, s := range stored {
		if !keep[s] {
			out = append(out, s)
		}
	}
	return out
}

// union lists a's members then b's additions, without duplicates.
func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// dataDocViolations holds a directory's DELIVERY WIRING to its package
// version. A trigger carries no version of its own and the install upserts it
// with the closure, so a wiring change reaches a repository only as part of an
// upgrade the package version announces.
func dataDocViolations(base, head *tree) []string {
	var out []string
	dirs := make([]string, 0, len(base.dataDocs))
	for dir := range base.dataDocs {
		dirs = append(dirs, dir)
	}
	for dir := range head.dataDocs {
		if base.dataDocs[dir] == nil {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if !head.dirs[dir] {
			continue // the package left the tree whole
		}
		if reflect.DeepEqual(base.dataDocs[dir], head.dataDocs[dir]) {
			continue
		}
		// One package per directory, so its bump is the one that carries the
		// wiring.
		pkg := head.dirPackage[dir]
		if pkg == "" {
			pkg = base.dirPackage[dir]
		}
		bv, hv := base.packageVersion[pkg], head.packageVersion[pkg]
		if pkg == "" || hv == 0 || vocabulary.CompareVersions(hv, bv) > 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s: the delivery wiring changed but package %s stays at %d; bump the package version, or no repository is ever offered the change",
			dir, pkg, bv))
	}
	return out
}

// minusVersion is the declaration's data with the version key removed: the
// version is the statement ABOUT the change, not part of it.
func minusVersion(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		if k == "version" {
			continue
		}
		out[k] = v
	}
	return out
}
