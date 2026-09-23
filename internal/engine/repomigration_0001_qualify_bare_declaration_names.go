package engine

// Repository migration 1: every stored declaration names its kinds and traits
// in full.
//
// Decision record 0098 removed the bare-name shorthand from declarations: a
// `kind:` pin, a `trait:` pin, a `traits:` binding and a function's
// `writes`/`reads.kinds` entry are `<authority>/<package>/<name>`, and a
// bare word is refused at admission. A repository created before that holds
// its declarations as the binary before stored them, bare where the author
// wrote them bare, core's own `recordsplit.merge` included, and the loader
// this binary carries refuses every such row. This rewrites them, once, at
// the repository's first open, to the identity THAT binary resolved each
// word to, so what the repository meant is what it now says.
//
// The old rule is reproduced here from the stored rows alone, without the
// loader: a kind pin resolved in the declaring package first, then to the one
// kind anywhere carrying the word; a trait binding or pin in the declaring
// package first, then to core's, then to the one trait anywhere; a function's
// allowlist entry to the one kind anywhere. A word the old rule could not
// resolve (two candidates, or none) is left as it is: the old binary refused
// that package too, so it was parked then and parks now, and the reason names
// the word.
//
// The rewritten rows are projected the way the boot upgrade projects a
// shipped change (projectPackages): through the changelog, validated against
// a registry built from the rewritten documents, with every row's STORED
// version kept and an imported sample's origin digest recomputed over the
// rewritten rows, so the copy reads pristine afterwards rather than edited. A
// row whose spelling did not change is a no-op and appends nothing.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// migrateQualifyBareDeclarationNames is the migration's Run.
func migrateQualifyBareDeclarationNames(t *txn) (string, error) {
	ds := t.ds
	docs, bySource, err := ds.storedDocumentsBySource(t.ctx, nil)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "no stored vocabulary", nil
	}
	ix := indexDeclaredNames(docs)
	rewritten := map[string]vocabulary.Document{}
	changed := map[string]int{}
	for _, key := range sortedKeys(docs) {
		d := docs[key]
		nd, n := qualifyDocument(d, ix)
		if n == 0 {
			continue
		}
		rewritten[key] = nd
		changed[d.DeclaredPackage()] += n
	}
	if len(changed) == 0 {
		return "no bare name in any stored declaration", nil
	}

	// The registry the rewritten rows are validated against and projected
	// from: every stored package, rewritten, built per source the way the
	// open builds them. A package that still does not parse or admit keeps
	// its rows as they are: it was refused before this migration for a reason
	// of its own, and it parks with that reason, bare names included.
	reg := vocabulary.NewRegistry()
	var built []*vocabulary.Package
	leftBare := map[string]string{}
	for _, source := range []string{vocabulary.SourceBuiltin, vocabulary.SourcePublished, vocabulary.SourceInstalled} {
		names := bySource[source]
		if len(names) == 0 {
			continue
		}
		all := make([]vocabulary.Document, 0, len(docs))
		for _, key := range sortedKeys(docs) {
			d := docs[key]
			if !names[d.DeclaredPackage()] {
				continue
			}
			if nd, ok := rewritten[key]; ok {
				d = nd
			}
			all = append(all, d)
		}
		gs, err := vocabulary.BuildPackages(all, source)
		if err != nil {
			good, bad, cerr := buildPackagesSeparately(all, source)
			if cerr != nil {
				return "", fmt.Errorf("the seeded closure does not parse after the rewrite: %w", cerr)
			}
			gs = good
			for _, q := range bad {
				leftBare[q.name] = q.reason
			}
		}
		built = append(built, gs...)
	}
	_, inadmissible := admissibleInto(reg, built)
	for _, q := range inadmissible {
		if seededPackageName(q.name) {
			return "", fmt.Errorf("the seeded closure %s does not admit after the rewrite: %s", q.name, q.reason)
		}
		leftBare[q.name] = q.reason
	}
	for _, pkg := range sortedKeys(leftBare) {
		if changed[pkg] == 0 {
			continue
		}
		ds.svc.log.Warn("substrate: a repository migration left a package's bare names as they are, because the package does not admit for a reason of its own; it parks with that reason",
			"repository", logSafeID(ds.info.ID), "package", logSafeID(pkg), "reason", logSafeText(leftBare[pkg]))
		delete(changed, pkg)
	}

	// The projection resolves the meta-kind of each row it writes (core's
	// `kind` for a kind row) against the dataset's live registry, which at
	// this point in the open is empty: nothing has loaded the stored rows yet,
	// and this is what makes them loadable. The candidate stands in for the
	// duration; the load that follows the runner starts from the empty
	// registry it found.
	prev := ds.registry()
	ds.mu.Lock()
	ds.reg = reg
	ds.mu.Unlock()
	defer func() {
		ds.mu.Lock()
		ds.reg = prev
		ds.mu.Unlock()
	}()

	// Every row keeps the version the store gave it: the API moves a row
	// ahead of its package, and a projection that let the package's version
	// win would move it back.
	stored, err := ds.storedDeclarations(t.ctx)
	if err != nil {
		return "", err
	}
	versions := make(map[string]int64, len(stored))
	for key, s := range stored {
		versions[key] = s.version
	}
	var names int
	for _, pkg := range sortedKeys(changed) {
		if _, ok := reg.PackageByName(pkg); !ok {
			continue
		}
		opts := projectOpts{versions: versions}
		// An imported sample carries its origin stamp; the rewrite changes
		// the rows the digest is over, so it is stamped again over the
		// rewritten rows and the copy reads pristine rather than edited.
		row, err := t.loadRow(eref{Kind: kindPackage, ID: pkg}, false)
		if err != nil {
			return "", err
		}
		if row != nil {
			if origin, _ := row.Props[propPackageOrigin].(string); origin != "" {
				opts.origin = origin
				opts.originVersion, _ = vocabulary.VersionValue(row.Props[propPackageOriginVersion])
			}
		}
		if _, err := t.projectPackages(reg, map[string]bool{pkg: true}, opts); err != nil {
			return "", fmt.Errorf("project the rewritten %s: %w", pkg, err)
		}
		names += changed[pkg]
	}
	return fmt.Sprintf("wrote the full identity into %d bare names across %d packages", names, len(changed)), nil
}

// declaredNames indexes the kinds and traits a stored closure declares, by
// identity and by bare word: what the old resolution rule looked names up in.
type declaredNames struct {
	kinds, traits         map[string]bool
	kindsByWord, traitsBy map[string][]string
}

func indexDeclaredNames(docs map[string]vocabulary.Document) declaredNames {
	ix := declaredNames{
		kinds: map[string]bool{}, traits: map[string]bool{},
		kindsByWord: map[string][]string{}, traitsBy: map[string][]string{},
	}
	for _, key := range sortedKeys(docs) {
		d := docs[key]
		switch d.Kind {
		case vocabulary.DocKind:
			ix.kinds[d.ID] = true
			ix.kindsByWord[vocabulary.KindName(d.ID)] = append(ix.kindsByWord[vocabulary.KindName(d.ID)], d.ID)
		case vocabulary.DocTrait:
			ix.traits[d.ID] = true
			ix.traitsBy[vocabulary.KindName(d.ID)] = append(ix.traitsBy[vocabulary.KindName(d.ID)], d.ID)
		}
	}
	for _, m := range []map[string][]string{ix.kindsByWord, ix.traitsBy} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	return ix
}

// kindPin is the old `kind:` rule: the declaring package's own kind, else the
// one kind anywhere carrying the word. "" where the old loader refused.
func (ix declaredNames) kindPin(pkg, word string) string {
	if ix.kinds[pkg+"/"+word] {
		return pkg + "/" + word
	}
	return ix.uniqueKind(word)
}

// uniqueKind is the old rule for a function's allowlist entry: the one kind
// anywhere carrying the word, with no preference for the declaring package.
func (ix declaredNames) uniqueKind(word string) string {
	if c := ix.kindsByWord[word]; len(c) == 1 {
		return c[0]
	}
	return ""
}

// trait is the old binding and pin rule: the declaring package's own trait,
// else core's, else the one trait anywhere carrying the word.
func (ix declaredNames) trait(pkg, word string) string {
	if ix.traits[pkg+"/"+word] {
		return pkg + "/" + word
	}
	if core := vocabulary.PackageCore + "/" + word; ix.traits[core] {
		return core
	}
	if c := ix.traitsBy[word]; len(c) == 1 {
		return c[0]
	}
	return ""
}

// reBareBinding is a `traits:` entry whose trait is a bare word: the word,
// then the optional variant and remap the entry carries after it.
var reBareBinding = regexp.MustCompile(`^([a-z][a-zA-Z0-9]*)(\(.*\))?$`)

// qualifyDocument rewrites one document's bare names to the identities the
// old rule resolved them to, on a copy, and counts what it rewrote. A
// document with nothing to rewrite comes back as it was, with zero.
func qualifyDocument(d vocabulary.Document, ix declaredNames) (vocabulary.Document, int) {
	if d.Kind != vocabulary.DocKind && d.Kind != vocabulary.DocFunction {
		return d, 0
	}
	data, err := jsonSafe(d.Data)
	if err != nil {
		return d, 0
	}
	pkg := d.DeclaredPackage()
	var n int
	switch d.Kind {
	case vocabulary.DocKind:
		n += qualifyProperties(mapOf(data["properties"]), pkg, ix)
		if list, ok := data["traits"].([]any); ok {
			for i, v := range list {
				s, ok := v.(string)
				if !ok || strings.Contains(s, "/") {
					continue
				}
				m := reBareBinding.FindStringSubmatch(strings.TrimSpace(s))
				if m == nil {
					continue
				}
				if full := ix.trait(pkg, m[1]); full != "" {
					list[i] = full + m[2]
					n++
				}
			}
		}
	case vocabulary.DocFunction:
		perms := mapOf(data["permissions"])
		n += qualifyAllowlist(perms, "writes", ix)
		n += qualifyAllowlist(mapOf(perms["reads"]), "kinds", ix)
	}
	if n == 0 {
		return d, 0
	}
	d.Data = data
	return d, n
}

// qualifyProperties walks a kind's properties to every admitted depth: a
// reference's `kind:` and `trait:` pins, and the same inside an object's
// `fields:`.
func qualifyProperties(props map[string]any, pkg string, ix declaredNames) int {
	var n int
	for _, name := range sortedKeys(props) {
		p := mapOf(props[name])
		if p == nil {
			continue
		}
		if p["type"] == string(vocabulary.DatatypeReference) {
			if word, ok := p["kind"].(string); ok && word != "" && word != vocabulary.ToAny && !strings.Contains(word, "/") {
				if full := ix.kindPin(pkg, word); full != "" {
					p["kind"] = full
					n++
				}
			}
			if word, ok := p["trait"].(string); ok && word != "" && !strings.Contains(word, "/") {
				if full := ix.trait(pkg, word); full != "" {
					p["trait"] = full
					n++
				}
			}
		}
		n += qualifyProperties(mapOf(p["fields"]), pkg, ix)
	}
	return n
}

// qualifyAllowlist rewrites the bare entries of one kind allowlist; a glob and
// a full identity pass through.
func qualifyAllowlist(m map[string]any, key string, ix declaredNames) int {
	list, ok := m[key].([]any)
	if !ok {
		return 0
	}
	var n int
	for i, v := range list {
		s, ok := v.(string)
		if !ok || strings.Contains(s, "/") || vocabulary.IsTypeGlob(s) {
			continue
		}
		if full := ix.uniqueKind(s); full != "" {
			list[i] = full
			n++
		}
	}
	return n
}

// mapOf is v as a map, or nil.
func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
