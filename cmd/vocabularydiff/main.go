// vocabularydiff diffs two kinds/ trees at the document level and refuses a
// changed declaration whose effective version did not move forward. It is the
// CI half of the upgrade contract: the boot upgrade (engine seed.go) and the
// bundle upgrade preview (engine PlanBundleUpgrade) both key on `version`, so
// a definition that changes under an unmoved version is an upgrade no
// repository will ever receive. The rule it enforces, per declaration:
//
//   - definition changed (data compared minus `version`): the effective
//     version must be NEWER in head, through the one comparator
//     (vocabulary.CompareVersions). A kind may bump its own `data.version`;
//     everything else moves with its authority's.
//   - definition unchanged: the effective version must not move BACKWARD,
//     because a repository already holding the newer version would simply
//     keep it (never a downgrade) and the tree would stop converging.
//   - declaration deleted while its authority remains: the authority version
//     must bump, so the whole-authority replace that prunes it reads as an
//     upgrade. A directory deleted whole is a bundle leaving the catalog and
//     needs nothing.
//   - a DATA document changed, added or removed (triggers.yaml, the delivery
//     wiring): the authority version must bump. Data documents carry no
//     version of their own, and the install upserts them along with the
//     closure — so without a bump the wiring change is one no repository is
//     ever offered.
//
// Comment-only edits decode to identical data and pass free.
//
// Usage: vocabularydiff <base-kinds-dir> <head-kinds-dir>
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: vocabularydiff <base-kinds-dir> <head-kinds-dir>")
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
	fmt.Fprintf(os.Stderr, "\nvocabularydiff: %d violation(s). A changed declaration ships a changed version: bump the kind's own `data.version`, or the authority's `data.version` in its bundle.yaml (see AGENTS.md).\n", len(violations))
	os.Exit(1)
}

// decl is one schema document as the diff sees it: its manifest kind (the
// short name: "authority", "kind", "trait", ...), id, declaring authority,
// the file it came from, and its raw data map.
type decl struct {
	kind      string
	id        string
	authority string
	file      string
	data      map[string]any
}

func declKey(kind, id string) string { return kind + "\x00" + id }

// tree is one kinds/ directory, parsed: every declaration keyed by
// (kind, id), plus each authority directory's declared version.
type tree struct {
	decls map[string]decl
	// authorityVersion is the authority document's data.version, defaulted
	// exactly as the loader defaults it.
	authorityVersion map[string]int64
	// dirs is the set of authority directories present, so a deletion can
	// tell "declaration removed" from "bundle removed whole".
	dirs map[string]bool
	// dataDocs is each directory's delivery wiring (the non-declaration
	// documents), keyed by directory then by document, so a wiring change
	// can demand the authority bump that carries it to a repository.
	dataDocs map[string]map[string]any
}

func loadTree(root string) (*tree, error) {
	t := &tree{
		decls:            map[string]decl{},
		authorityVersion: map[string]int64{},
		dirs:             map[string]bool{},
		dataDocs:         map[string]map[string]any{},
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.dirs[e.Name()] = true
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			rel := filepath.Join(e.Name(), f.Name())
			raw, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				return nil, err
			}
			if err := t.loadFile(rel, raw); err != nil {
				return nil, fmt.Errorf("%s: %w", rel, err)
			}
		}
	}
	return t, nil
}

func (t *tree) loadFile(rel string, raw []byte) error {
	dir := strings.SplitN(rel, string(filepath.Separator), 2)[0]
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
		refAuthority, name := vocabulary.SplitKindRef(ref)
		if refAuthority != vocabulary.AuthorityCore || !vocabulary.VocabularyDocumentKind(name) {
			// A data document: the delivery wiring the install upserts beside
			// the closure. It carries no version, so it is diffed WHOLE and
			// held to its directory's authority version.
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
		authority, _ := data["authority"].(string)
		if name == vocabulary.DocAuthority {
			authority = id
			version, _ := vocabulary.VersionValue(data["version"])
			if version == 0 {
				version = vocabulary.DefaultVersion
			}
			t.authorityVersion[id] = version
		}
		t.decls[declKey(name, id)] = decl{kind: name, id: id, authority: authority, file: rel, data: data}
	}
	return nil
}

// effectiveVersion is the version a declaration would project with: a kind's
// own data.version where it declares one, else its authority's, exactly as
// the engine's projection defaults it (authorityDeclarations).
func (t *tree) effectiveVersion(d decl) int64 {
	if d.kind == vocabulary.DocKind || d.kind == vocabulary.DocAuthority {
		if v, _ := vocabulary.VersionValue(d.data["version"]); v != 0 {
			return v
		}
	}
	if v, ok := t.authorityVersion[d.authority]; ok {
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
			dir := strings.SplitN(b.file, string(filepath.Separator), 2)[0]
			if !head.dirs[dir] {
				continue // the bundle left the tree whole
			}
			bv, hv := base.authorityVersion[b.authority], head.authorityVersion[b.authority]
			if hv == 0 || vocabulary.CompareVersions(hv, bv) > 0 {
				continue // the prune rides an authority bump (or the authority left too)
			}
			out = append(out, fmt.Sprintf("%s: %s %s was removed but authority %s stays at %d; bump the authority version so the prune is an upgrade",
				b.file, b.kind, b.id, b.authority, bv))
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
	return append(out, dataDocViolations(base, head)...)
}

// dataDocViolations holds a directory's DELIVERY WIRING to its authority
// version. A trigger carries no version of its own and the install upserts it
// with the closure, so a wiring change reaches a repository only as part of an
// upgrade the authority version announces.
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
			continue // the bundle left the tree whole
		}
		if reflect.DeepEqual(base.dataDocs[dir], head.dataDocs[dir]) {
			continue
		}
		// The directory IS the authority for the shipped tree (one authority
		// per directory), so its bump is the one that carries the wiring.
		bv, hv := base.authorityVersion[dir], head.authorityVersion[dir]
		if hv == 0 || vocabulary.CompareVersions(hv, bv) > 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s: the delivery wiring changed but authority %s stays at %d; bump the authority version, or no repository is ever offered the change",
			dir, dir, bv))
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
