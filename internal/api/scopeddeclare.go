package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// What an app's `declare:~/<package>` scope applies (decision 0203): its
// package's package, bundle and kind documents, a bundle that only lists the
// closure, and a batch that adds to what the repository holds and never
// takes anything away. The engine's admission runs after these, unchanged.

// declarableBundleKeys are the bundle keys a declare scope may send: the
// closure listing and its preconditions. `modules` is stored code every
// function of the package imports, `oauth2` makes the package a provider
// whose endpoints the app would choose, and `inputs` binds records the owner
// configures; each is the owner's to declare.
var declarableBundleKeys = map[string]bool{
	"authority": true, "package": true, "description": true,
	"requires": true, "requiresAtLeast": true, "installs": true,
}

// undeclarableKindKeys are the kind keys a declare scope may not send:
// `indices` builds Postgres indexes on the owner's database at apply.
var undeclarableKindKeys = map[string]string{
	"indices": "builds indexes on the owner's database",
}

// keepsPackage reports whether the grant holds a declare scope.
func (s *scopedDataset) keepsPackage() bool {
	return slices.ContainsFunc(s.grant.Scopes(), func(raw string) bool { return strings.HasPrefix(raw, string(scope.Declare)+":") })
}

// declarationRowKind reports whether kind is one of the declaration kinds a
// declare scope reads its own package's rows of.
func declarationRowKind(kind string) bool {
	switch kind {
	case vocabulary.CoreKind(vocabulary.DocPackage), vocabulary.CoreKind(vocabulary.DocBundle), vocabulary.CoreKind(vocabulary.DocKind):
		return true
	}
	return false
}

// declaredPackage is the package a declaration row of kind with id belongs to.
func declaredPackage(kind, id string) string {
	if kind == vocabulary.CoreKind(vocabulary.DocKind) {
		return vocabulary.KindPackage(id)
	}
	return id
}

// declarable is the declare scope's rule over one batch's documents, each
// read alone.
func (s *scopedDataset) declarable(docs []map[string]any) error {
	if len(docs) == 0 {
		return errScope("the vocabulary")
	}
	home := s.inner.Repository().Authority
	for _, doc := range docs {
		kind, _ := doc["kind"].(string)
		meta, _ := doc["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		data, _ := doc["data"].(map[string]any)
		var pkg string
		switch kind {
		case vocabulary.CoreKind(vocabulary.DocPackage):
			pkg = id
		case vocabulary.CoreKind(vocabulary.DocBundle):
			pkg = id
			for _, key := range declaredKeys(data) {
				if !declarableBundleKeys[key] {
					return fmt.Errorf("%w: an app's token declares a bundle that lists its kinds, and %s carries data.%s, which is the owner's to declare", substrate.ErrForbidden, id, key)
				}
			}
		case vocabulary.CoreKind(vocabulary.DocKind):
			pkg = vocabulary.KindPackage(id)
			for _, key := range declaredKeys(data) {
				if why, refused := undeclarableKindKeys[key]; refused {
					return fmt.Errorf("%w: an app's token may not declare data.%s on %s: it %s", substrate.ErrForbidden, key, id, why)
				}
			}
			if path := embedded(data["properties"], "properties"); path != "" {
				return fmt.Errorf("%w: an app's token may not declare %s.embed on %s: embedding spends the owner's provider budget", substrate.ErrForbidden, path, id)
			}
		default:
			return fmt.Errorf("%w: an app's token declares packages, bundles and kinds, not %s", substrate.ErrForbidden, kind)
		}
		if !s.grant.DeclaresIn(pkg, home) {
			return fmt.Errorf("%w: an app's token does not declare %s/%s: it holds no declare scope for the package", substrate.ErrForbidden, kind, id)
		}
	}
	return nil
}

// embedded is the path of the first property under props that turns `embed`
// on, "" when none does.
func embedded(props any, path string) string {
	m, _ := props.(map[string]any)
	for _, name := range declaredKeys(m) {
		p, _ := m[name].(map[string]any)
		at := path + "." + name
		if on, _ := p["embed"].(bool); on {
			return at
		}
		if inner := embedded(p["fields"], at+".fields"); inner != "" {
			return inner
		}
	}
	return ""
}

// widensOnly refuses a batch that would take away anything the repository
// holds in the packages it declares into: a declaration, a property or field,
// an enum value, a state, a transition or a trait, whoever added it. A
// declare scope only ever adds; a narrowing, or dropping what the owner
// added, is the owner's to apply.
//
// A bundle document makes the batch replace its package wholesale, so it is
// admitted only for a package the repository does not hold, or whose stored
// bundle's closure (the exact set of the package's declarations, which the
// loader holds `installs:` to) the incoming one lists in full.
//
// The check reads the stored package before the engine applies the batch, in
// a separate step: an owner's edit landing between the two is not seen here.
// The vocabulary apply takes no expected version to close that window.
func (s *scopedDataset) widensOnly(ctx context.Context, docs []map[string]any) error {
	stored, err := s.inner.Kinds(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]substrate.KindInfo, len(stored))
	held := map[string][]string{}
	for _, k := range stored {
		byID[k.Identity] = k
		pkg := vocabulary.PackageRef(k.Authority, k.Package)
		held[pkg] = append(held[pkg], k.Identity)
	}
	var problems []string
	for _, doc := range docs {
		kind, _ := doc["kind"].(string)
		meta, _ := doc["metadata"].(map[string]any)
		id, _ := meta["id"].(string)
		data, _ := doc["data"].(map[string]any)
		switch kind {
		case vocabulary.CoreKind(vocabulary.DocBundle):
			p, err := s.bundleKeeps(ctx, id, data, held[id])
			if err != nil {
				return err
			}
			problems = append(problems, p...)
		case vocabulary.CoreKind(vocabulary.DocKind):
			if k, ok := byID[id]; ok {
				problems = append(problems, dropped(id, k.Definition, data)...)
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: an app's token only adds to a package, and this apply would remove what the repository holds: %s; the owner applies a change that removes anything",
			substrate.ErrConflict, strings.Join(problems, "; "))
	}
	return nil
}

// bundleKeeps checks a replacing bundle against the stored package: every
// declaration the stored closure lists must be listed again.
func (s *scopedDataset) bundleKeeps(ctx context.Context, pkg string, data map[string]any, kinds []string) ([]string, error) {
	rec, err := s.inner.Get(ctx, vocabulary.CoreKind(vocabulary.DocBundle), pkg)
	switch {
	case errors.Is(err, substrate.ErrNotFound) || (err == nil && rec.DeletedAt != nil):
		if len(kinds) == 0 {
			return nil, nil
		}
		// Declarations without a closure listing: nothing says what a
		// replacement would prune beyond the kinds, so it is not attempted.
		sort.Strings(kinds)
		return []string{fmt.Sprintf("%s holds %s without a bundle, and a bundle document would replace it whole", pkg, strings.Join(kinds, ", "))}, nil
	case err != nil:
		return nil, err
	}
	incoming := map[string]bool{}
	for _, v := range declaredList(data["installs"]) {
		incoming[fmt.Sprint(v)] = true
	}
	var missing []string
	for _, v := range declaredList(rec.Properties["installs"]) {
		if id := fmt.Sprint(v); !incoming[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}
	sort.Strings(missing)
	return []string{fmt.Sprintf("%s holds %s, which this bundle does not list", pkg, strings.Join(missing, ", "))}, nil
}

// dropped lists what a kind document leaves out of the stored declaration.
func dropped(id string, stored, incoming map[string]any) []string {
	var out []string
	out = append(out, droppedProperties(id, "properties", declaredObject(stored["properties"]), declaredObject(incoming["properties"]))...)
	if gone := missingStrings(declaredList(stored["traits"]), declaredList(incoming["traits"])); len(gone) > 0 {
		out = append(out, fmt.Sprintf("%s: traits %s", id, strings.Join(gone, ", ")))
	}
	return out
}

func droppedProperties(id, path string, stored, incoming map[string]any) []string {
	var out []string
	for _, name := range declaredKeys(stored) {
		at := path + "." + name
		in, ok := incoming[name].(map[string]any)
		if !ok {
			out = append(out, fmt.Sprintf("%s: %s", id, at))
			continue
		}
		was := declaredObject(stored[name])
		if gone := missingStrings(enumValues(was["values"]), enumValues(in["values"])); len(gone) > 0 {
			out = append(out, fmt.Sprintf("%s: %s values %s", id, at, strings.Join(gone, ", ")))
		}
		if gone := missingStrings(declaredList(was["states"]), declaredList(in["states"])); len(gone) > 0 {
			out = append(out, fmt.Sprintf("%s: %s states %s", id, at, strings.Join(gone, ", ")))
		}
		if gone := missingStrings(transitions(was["transitions"]), transitions(in["transitions"])); len(gone) > 0 {
			out = append(out, fmt.Sprintf("%s: %s transitions %s", id, at, strings.Join(gone, ", ")))
		}
		out = append(out, droppedProperties(id, at+".fields", declaredObject(was["fields"]), declaredObject(in["fields"]))...)
	}
	return out
}

func declaredObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func declaredList(v any) []any {
	l, _ := v.([]any)
	return l
}

// enumValues reads an enum's values, each an entry with a `value` or a bare
// string.
func enumValues(v any) []any {
	var out []any
	for _, e := range declaredList(v) {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m["value"])
			continue
		}
		out = append(out, e)
	}
	return out
}

func transitions(v any) []any {
	var out []any
	for _, e := range declaredList(v) {
		m := declaredObject(e)
		out = append(out, fmt.Sprintf("%v->%v", m["from"], m["to"]))
	}
	return out
}

// missingStrings lists what stored holds that incoming does not, sorted.
func missingStrings(stored, incoming []any) []string {
	has := map[string]bool{}
	for _, v := range incoming {
		has[fmt.Sprint(v)] = true
	}
	var out []string
	for _, v := range stored {
		if s := fmt.Sprint(v); !has[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func declaredKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
