package vocabulary

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The manifest envelope. Every schema resource — an authority, a package, a
// kind, a trait, a custom property type, an actor — is one document in a `---`
// separated stream, and every document wears the same four keys. The unit is
// the document, not the file: any .yaml under the schema tree may hold any
// documents, and the loader buckets them by the PACKAGE they declare into
// (`data.authority` and `data.package`).

// AuthorityPublisher is the authority the substrate's own vocabulary publishes
// under, and PackageCore the package its meta-kinds live in: a schema manifest
// is itself a record of a core kind, whatever package the document describes.
// A repository may never claim an authority under the publisher's
// (engine.validRepositoryAuthority), so nothing a user declares can be
// mistaken for shipped vocabulary.
const (
	AuthorityPublisher = "substrate.reamde.dev"
	PackageCore        = AuthorityPublisher + "/core"
)

// The manifest document kinds — the local names under PackageCore. The
// envelope carries the full reference ("substrate.reamde.dev/core/kind"), so a
// manifest names its own kind exactly as any other record does.
const (
	DocAuthority     = "authority"
	DocPackage       = "package"
	DocKind          = "kind"
	DocTrait         = "trait"
	DocPropertyType  = "propertytype"
	DocRecordMapping = "recordmapping"
	DocFunction      = "function"
	DocAgent         = "agent"
	DocActor         = "actor"
	DocBundle        = "bundle"
)

var schemaDocumentKinds = map[string]bool{
	DocAuthority: true, DocPackage: true, DocKind: true, DocTrait: true,
	DocPropertyType: true, DocRecordMapping: true, DocFunction: true,
	DocAgent: true,
	DocActor: true, DocBundle: true,
}

// VocabularyDocumentKind reports whether a local name is one of the schema kinds —
// the documents the loader admits and the batch apply verb carries.
func VocabularyDocumentKind(short string) bool { return schemaDocumentKinds[short] }

// DeclaredAuthority is the authority a schema document declares INTO:
// `data.authority` for every kind but the authority header, whose own id is
// the authority.
func (d Document) DeclaredAuthority() string {
	if d.Kind == DocAuthority {
		return d.ID
	}
	return mstr(d.Data, "authority")
}

// DeclaredPackage is the GROUP a schema document belongs to, and the key
// everything downstream buckets, versions, owns and quarantines by
// (decision 0047): "<authority>/<package>" for a member declaration and for
// the package header, whose own id is exactly that; the bare authority for an
// authority header, which owns no members. A document naming no authority, or
// a member naming no package, answers "" and is refused by name at the door
// that asked.
func (d Document) DeclaredPackage() string {
	switch d.Kind {
	case DocAuthority, DocPackage:
		return d.ID
	}
	authority, pkg := mstr(d.Data, "authority"), mstr(d.Data, "package")
	if authority == "" || pkg == "" {
		return ""
	}
	return PackageRef(authority, pkg)
}

var envelopeKeys = map[string]bool{
	"kind": true, "metadata": true, "data": true,
	// status is server-set and ignored on input, so `get -o yaml` output is
	// directly apply-able.
	"status": true,
}

// deletedEnvelopeKeys name their replacement: a document carrying one is
// refused with the key that took its job, never quietly ignored.
var deletedEnvelopeKeys = map[string]string{
	"apiVersion": "kind (the version left the envelope)",
	"group":      "kind (one kind reference names the authority and the name)",
	"type":       "kind",
	"spec":       "data",
}

var metadataKeys = map[string]bool{"id": true, "labels": true, "annotations": true}

// Document is one manifest: the envelope, parsed, plus the verbatim text it
// was declared in.
type Document struct {
	// Kind is the LOCAL name of the envelope's kind reference — "kind"
	// for `kind: substrate.reamde.dev/core/kind`. A manifest document is always
	// a core kind; the authority being DECLARED INTO is `data.authority`.
	Kind string
	// ID is metadata.id: the resource's identity (FORMAT.md §2).
	ID          string
	Labels      map[string]string
	Annotations map[string]string
	Data        map[string]any

	// Source is the document's own text, comments included. A manifest that
	// arrived as a map (an installed connector payload) has no original text,
	// so its source is the document marshaled back to YAML.
	Source string
}

// ParseStream splits a `---` separated YAML stream into envelope-validated
// documents, each carrying its own verbatim text.
func ParseStream(data []byte) ([]Document, error) {
	var out []Document
	var problems []string
	chunks, err := splitDocuments(data)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		text := trimBlankEdges(chunk)
		var raw map[string]any
		if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
			problems = append(problems, fmt.Sprintf("parse yaml: %v", err))
			continue
		}
		if len(raw) == 0 {
			// A comment-only or empty document carries nothing to load.
			continue
		}
		doc, errs := documentFrom(raw, text)
		problems = append(problems, errs...)
		if len(errs) == 0 {
			out = append(out, doc)
		}
	}
	if len(problems) > 0 {
		return nil, validationError(problems)
	}
	return out, nil
}

// DocumentFromMap turns one already-decoded manifest — a connector's
// installed payload is a JSON list of them — into a Document. It has no
// original text, so its source is rendered from the map, deterministically,
// which keeps the boot-time projection no-op suppressed.
func DocumentFromMap(raw map[string]any) (Document, error) {
	doc, problems := documentFrom(raw, "")
	if len(problems) > 0 {
		return Document{}, validationError(problems)
	}
	return doc, nil
}

func documentFrom(raw map[string]any, text string) (Document, []string) {
	var problems []string
	errf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	ref := mstr(raw, "kind")
	authority, pkg, local := SplitKindRef(ref)
	d := Document{
		Kind:   local,
		Data:   mmap(raw, "data"),
		Source: text,
	}
	for k := range raw {
		if envelopeKeys[k] {
			continue
		}
		if replacement, deleted := deletedEnvelopeKeys[k]; deleted {
			errf("manifest: key %q is deleted — use %s", k, replacement)
			continue
		}
		errf("manifest: unknown key %q (the envelope is kind/metadata/data/status)", k)
	}
	switch {
	case ref == "":
		errf("manifest: kind is required — a kind reference like %q", CoreKind(DocKind))
	case authority+"/"+pkg != PackageCore:
		errf("manifest: kind %q: schema manifests are records of the %s kinds", ref, PackageCore)
	case !schemaDocumentKinds[local]:
		errf("manifest: unknown kind %q", ref)
	}
	meta := mmap(raw, "metadata")
	for k := range meta {
		if metadataKeys[k] {
			continue
		}
		if k == "name" {
			errf("%s: metadata.name is deleted — use metadata.id", d.Kind)
			continue
		}
		errf("%s: metadata: unknown key %q", d.Kind, k)
	}
	d.ID = mstr(meta, "id")
	if d.ID == "" {
		errf("%s: metadata.id is required", d.Kind)
	}
	d.Labels = metaStrings(meta, "labels", d.Kind, &problems)
	d.Annotations = metaStrings(meta, "annotations", d.Kind, &problems)
	if _, ok := raw["data"]; !ok {
		errf("%s %s: data is required", d.Kind, d.ID)
	}
	if d.Source == "" {
		d.Source = renderDocument(raw)
	}
	return d, problems
}

// metaStrings reads a labels/annotations block: namespaced keys, string
// values (FORMAT.md §1).
func metaStrings(meta map[string]any, key, typ string, problems *[]string) map[string]string {
	raw := mmap(meta, key)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if !ValidMetaKey(k) {
			*problems = append(*problems, fmt.Sprintf(
				"%s: metadata.%s: %q must be a namespaced key (\"<actor>/<name>\")", typ, key, k))
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	return out
}

// renderDocument marshals a manifest map back to YAML: the best available
// "original" for a document that arrived on the wire. Deterministic — yaml.v3
// sorts map keys — so re-projecting an installed manifest writes nothing.
func renderDocument(raw map[string]any) string {
	out, err := yaml.Marshal(raw)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// splitDocuments cuts a stream on its `---` separators. Slicing the text
// rather than asking the YAML parser for node positions is what makes the
// verbatim capture exact: a document's block is every line between two
// separators, leading comments included.
func splitDocuments(data []byte) ([]string, error) {
	lines := splitLines(data)
	var docs []string
	var cur []string
	for i, l := range lines {
		t := strings.TrimRight(l, " \t")
		if t == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		if strings.HasPrefix(t, "--- ") {
			// YAML permits content on the separator line, but the verbatim
			// slicer cannot capture it as a document of its own — refusing it
			// keeps the loader fail-loud instead of silently dropping a
			// manifest.
			return nil, fmt.Errorf("line %d: a `---` separator must stand alone; move %q to the next line", i+1, strings.TrimPrefix(t, "--- "))
		}
		cur = append(cur, t)
	}
	return append(docs, strings.Join(cur, "\n")), nil
}

// trimBlankEdges drops blank lines at both ends of a document, keeping the
// comments that say what the manifest is FOR.
func trimBlankEdges(s string) string {
	lines := strings.Split(s, "\n")
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// splitLines splits on newlines, tolerating CRLF and a missing final newline.
func splitLines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// PackageManifest renders a package document — the closure header a run-time
// install builds in code, with the constructors below.
func PackageManifest(pkg string, version int64) map[string]any {
	if version == 0 {
		version = DefaultVersion
	}
	authority, name := SplitPackageRef(pkg)
	return map[string]any{
		"kind":     CoreKind(DocPackage),
		"metadata": map[string]any{"id": pkg},
		"data": map[string]any{
			"authority": authority, "package": name, "version": version,
		},
	}
}

// ActorManifest renders an actor document.
func ActorManifest(pkg, actor string) map[string]any {
	authority, name := SplitPackageRef(pkg)
	return map[string]any{
		"kind":     CoreKind(DocActor),
		"metadata": map[string]any{"id": actor},
		"data":     map[string]any{"authority": authority, "package": name},
	}
}

// KindManifest renders a kind document: the identity, the authority and the
// package are derived from the package identity and the names block, so a
// caller cannot spell them inconsistently.
func KindManifest(pkg string, names, data map[string]any) map[string]any {
	authority, name := SplitPackageRef(pkg)
	full := map[string]any{"authority": authority, "package": name, "names": names}
	for _, k := range sortedKeys(data) {
		full[k] = data[k]
	}
	return map[string]any{
		"kind":     CoreKind(DocKind),
		"metadata": map[string]any{"id": pkg + "/" + fmt.Sprint(names["singular"])},
		"data":     full,
	}
}

// MappingManifest renders a recordmapping document: the identity
// derives from the name and the authority so a caller cannot spell them
// inconsistently. data carries from/to/property and the optional match and map.
func MappingManifest(pkg, name string, data map[string]any) map[string]any {
	authority, pkgName := SplitPackageRef(pkg)
	full := map[string]any{"authority": authority, "package": pkgName}
	for _, k := range sortedKeys(data) {
		full[k] = data[k]
	}
	return map[string]any{
		"kind":     CoreKind(DocRecordMapping),
		"metadata": map[string]any{"id": pkg + "/" + name},
		"data":     full,
	}
}

// FunctionManifest renders a function document: the
// identity derives from the name and the authority so a caller cannot spell them
// inconsistently. data carries description/runtime/source/capabilities and
// the optional input/output schemas — never a subscription, which lives on
// trigger records.
func FunctionManifest(pkg, name string, data map[string]any) map[string]any {
	authority, pkgName := SplitPackageRef(pkg)
	full := map[string]any{"authority": authority, "package": pkgName}
	for _, k := range sortedKeys(data) {
		full[k] = data[k]
	}
	return map[string]any{
		"kind":     CoreKind(DocFunction),
		"metadata": map[string]any{"id": pkg + "/" + name},
		"data":     full,
	}
}

// BundleManifest renders a bundle document: its id IS the package it is named
// for, so a caller cannot spell the two inconsistently. data carries
// description/inputs/installs.
func BundleManifest(pkg string, data map[string]any) map[string]any {
	authority, name := SplitPackageRef(pkg)
	full := map[string]any{"authority": authority, "package": name}
	for _, k := range sortedKeys(data) {
		full[k] = data[k]
	}
	return map[string]any{
		"kind":     CoreKind(DocBundle),
		"metadata": map[string]any{"id": pkg},
		"data":     full,
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
