package engine

// PUBLISHING (decision records 0289 and 0291). A repository publishes one of
// its own packages under an authority whose DNS its owner controls: the
// package leaves the local namespace for good and is served from here to
// any server that installs it.
//
// THE PUBLISH IS ONE VOCABULARY BATCH, in one transaction, as ordinary
// writes (decision record 0078): the package's declarations land under the
// authority, the other packages of the repository's own that name it follow,
// the package in `~` is removed whole, and every live record of its kinds is
// carried to the kind of the same name under the authority (move.go: same id,
// same properties, managers and annotations, every reference at it
// repointed, the old row tombstoned). The first release is written in the
// same transaction. A crash leaves the repository as it was or published,
// never between, and history holds the package's old lines under `~` and the
// move after them.
//
// It is not the kind move a publisher may not make (decision record 0269):
// that renames a kind other repositories hold records of. Before the publish
// no other repository holds any, because a package in `~` is nobody's but
// this repository's, and after it the published names are permanent here
// too: the package can be released again, and never published again under
// another name.
//
// A RELEASE is a record of core's `release` kind: the closure's bytes, its
// digest and its version, never changed and never removed. The authority
// document this server serves lists every release, and a release is the
// package's declarations exactly as this repository holds them at that
// version, with no record of its data.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// kindRelease is one version of a package this repository publishes.
const kindRelease = "substrate.reamde.dev/core/release"

// publishing is a publish staged: the batch that performs it and what it
// moves, named as it will be.
type publishing struct {
	from, to string
	kinds    []string
	others   []string
	batch    vocabularyBatch
}

// stagePublish checks a publish and builds its batch. Every refusal here
// changes nothing.
func (ds *dataset) stagePublish(ctx context.Context, in substrate.PublishInput) (*publishing, error) {
	reg := ds.registry()
	from := vocabulary.LocalizeRef(in.Package, ds.names()...)
	authority, name := vocabulary.SplitPackageRef(from)
	if authority != vocabulary.Local || !vocabulary.ValidPackage(name) {
		return nil, fmt.Errorf("%w: %s is not a package of this repository's own: only a package in ~ is published", substrate.ErrValidation, in.Package)
	}
	if _, ok := reg.PackageByName(from); !ok {
		return nil, fmt.Errorf("%w: this repository has no package %s", substrate.ErrNotFound, from)
	}
	to := strings.ToLower(strings.TrimSpace(in.Authority))
	if to == vocabulary.Local || !vocabulary.ValidKindAuthority(to) || !strings.Contains(to, ".") {
		return nil, fmt.Errorf("%w: %q is not an authority a package is published under: a domain name its owner controls, such as ada.example.com", substrate.ErrValidation, in.Authority)
	}
	// The project's own name, and every name beneath it, reach a repository
	// from the binary (decision record 0292), as no repository's name may
	// sit there either (validRepositoryAuthority).
	if to == publisherAuthority || strings.HasSuffix(to, "."+publisherAuthority) {
		return nil, fmt.Errorf("%w: %s is the project's name: only the substrate's own binary publishes under it", substrate.ErrForbidden, to)
	}
	published := to + "/" + name
	// By the authority's own list: before the publish the repository's own
	// name is a spelling of `~`, so a lookup by name would find the package
	// being published.
	if slices.Contains(reg.PackagesOf(to), published) {
		return nil, fmt.Errorf("%w: this repository already holds %s", substrate.ErrConflict, published)
	}
	locals := map[string]bool{}
	for _, pkg := range reg.PackagesOf(vocabulary.Local) {
		locals[pkg] = true
	}
	rows, err := ds.vocabularyDocumentRows(ctx, ds.db, locals)
	if err != nil {
		return nil, err
	}
	docs := make([]map[string]any, 0, len(rows))
	for _, key := range sortedKeys(rows) {
		docs = append(docs, documentEnvelope(rows[key]))
	}
	// The names that spell `~` now say which names are the package's.
	moved, problems, err := vocabulary.PublishDocuments(docs, name, to, ds.names())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	p := &publishing{from: from, to: published}
	bundled := false
	for _, d := range moved {
		pkg := mapString(d, "data", "authority") + "/" + mapString(d, "data", "package")
		if pkg != published {
			p.others = append(p.others, pkg)
		}
		if d["kind"] == vocabulary.CoreKind(vocabulary.DocBundle) && pkg == published {
			bundled = true
		}
	}
	g, _ := reg.PackageByName(from)
	publish := map[string]string{}
	for _, k := range g.KindOrder {
		publish[g.Kinds[k].Identity] = published + "/" + k
		p.kinds = append(p.kinds, published+"/"+k)
	}
	if !bundled {
		// A bundle's closure is the package: it lists every declaration in
		// it but the header and itself.
		var installs []any
		for _, d := range moved {
			kind := mapString(d, "kind")
			if mapString(d, "data", "authority")+"/"+mapString(d, "data", "package") != published ||
				kind == vocabulary.CoreKind(vocabulary.DocPackage) {
				continue
			}
			installs = append(installs, mapString(d, "metadata", "id"))
		}
		moved = append(moved, map[string]any{
			"kind":     vocabulary.CoreKind(vocabulary.DocBundle),
			"metadata": map[string]any{"id": published},
			"data":     map[string]any{"authority": to, "package": name, "installs": installs},
		})
	}
	parsed, err := vocabulary.ParseDocuments(moved)
	if err != nil {
		return nil, err
	}
	sort.Strings(p.others)
	p.others = slices.Compact(p.others)
	held, err := packageConfiguration(ctx, ds.db, from)
	if err != nil {
		return nil, err
	}
	if len(held) > 0 {
		return nil, configurationRefusal(from, held)
	}
	p.batch = vocabularyBatch{
		docs:            parsed,
		replacePackages: []string{from},
		publish:         publish,
		publishTo:       published,
		// Under the batch's barrier: the package holds no settings or secrets
		// of its own, and a trigger that calls or watches it follows it
		// before the dropped-callable guard counts the triggers.
		beforeGuards: func(t *txn) error {
			held, err := packageConfiguration(t.ctx, t.tx, from)
			if err != nil {
				return err
			}
			if len(held) > 0 {
				return configurationRefusal(from, held)
			}
			return t.repointTriggers(from, published)
		},
	}
	return p, nil
}

// configurationRefusal refuses publishing pkg while it holds settings or
// secrets of its own.
func configurationRefusal(pkg string, held []string) error {
	return &substrate.ValidationError{Problems: []string{fmt.Sprintf(
		"%s holds its own settings or secrets (%s), and this build does not publish a package that does: delete them first",
		pkg, strings.Join(held, ", "))}}
}

// packageConfiguration names the settings and secrets of pkg, which are
// records named under the package rather than records of its kinds.
func packageConfiguration(ctx context.Context, q dbx, pkg string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind, id FROM records
		WHERE kind IN ($1, $2) AND deleted_at IS NULL AND starts_with(id, $3) ORDER BY kind, id`,
		vocabulary.CoreKind("setting"), vocabulary.CoreKind("secret"), pkg+"/")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, err
		}
		out = append(out, vocabulary.RecordPath(kind, id))
	}
	return out, rows.Err()
}

// repointTriggers moves every live trigger that calls a function or an agent
// of the package from, or watches one of its kinds, to the same under to.
func (t *txn) repointTriggers(from, to string) error {
	ids, err := t.liveIDs(typeTrigger)
	if err != nil {
		return err
	}
	for _, id := range ids {
		row, err := t.loadRow(eref{Kind: typeTrigger, ID: id}, true)
		if err != nil || row == nil {
			return err
		}
		patch := map[string]any{}
		if callable, ok := row.Props["callable"]; ok {
			if moved, changed := publishedDeclarationRefs(callable, from, to); changed {
				patch["callable"] = moved
			}
		}
		if source, ok := row.Props["source"]; ok {
			if moved, changed := publishedSelector(source, "record/kinds", from, to); changed {
				patch["source"] = moved
			}
		}
		if len(patch) == 0 {
			continue
		}
		if _, err := t.patch(eref{Kind: typeTrigger, ID: id}, substrate.PatchInput{Properties: patch}); err != nil {
			return fmt.Errorf("substrate/engine: publish: trigger %s: %w", id, err)
		}
	}
	return nil
}

// repointDeclarationRefs moves every live record but a trigger that refers to
// a declaration of the package from (a record view's kind, a collection
// view's) to the same declaration under to, and every change policy whose
// selector names one of its kinds. It runs after the projection, so the
// declarations it points at exist.
func (t *txn) repointDeclarationRefs(from, to string) error {
	kinds := make([]string, 0, len(vocabularyRecordKinds))
	for k := range vocabularyRecordKinds {
		kinds = append(kinds, k)
	}
	rows, err := t.query(`SELECT DISTINCT r.src_kind, r.src FROM refs r
		JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE x.deleted_at IS NULL AND r.src_kind <> $1 AND r.dst_kind = ANY($2::text[])
		  AND (r.dst = $3 OR starts_with(r.dst, $4))
		ORDER BY r.src_kind, r.src`, typeTrigger, kinds, from, from+"/")
	if err != nil {
		return err
	}
	var sources []eref
	for rows.Next() {
		var src eref
		if err := rows.Scan(&src.Kind, &src.ID); err != nil {
			_ = rows.Close()
			return err
		}
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	policies, err := t.liveIDs(vocabulary.KindRecordPatchPolicy)
	if err != nil {
		return err
	}
	for _, id := range policies {
		sources = append(sources, eref{Kind: vocabulary.KindRecordPatchPolicy, ID: id})
	}
	for _, src := range sources {
		row, err := t.loadRow(src, true)
		if err != nil || row == nil || row.DeletedAt != nil {
			if err != nil {
				return err
			}
			continue
		}
		patch := map[string]any{}
		for name, v := range row.Props {
			if moved, changed := publishedDeclarationRefs(v, from, to); changed {
				patch[name] = moved
			}
		}
		if src.Kind == vocabulary.KindRecordPatchPolicy {
			if selector, ok := row.Props["selector"]; ok {
				if moved, changed := publishedSelector(selector, "kinds", from, to); changed {
					patch["selector"] = moved
				}
			}
		}
		if len(patch) == 0 {
			continue
		}
		if _, err := t.patch(src, substrate.PatchInput{Properties: patch}); err != nil {
			return fmt.Errorf("substrate/engine: publish: %s: %w", vocabulary.RecordPath(src.Kind, src.ID), err)
		}
	}
	return nil
}

// publishedDeclarationRefs is v with every reference value (`{ref: …}`) at a
// declaration of the package from moved to the one of the same name under
// to, and whether anything moved.
func publishedDeclarationRefs(v any, from, to string) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		out, changed := make(map[string]any, len(x)), false
		for k, val := range x {
			if path, ok := val.(string); ok && k == vocabulary.ReferenceValueKey {
				if kind, id, ok := vocabulary.SplitRecordPath(path); ok && vocabularyRecordKinds[kind] != "" &&
					(id == from || strings.HasPrefix(id, from+"/")) {
					out[k], changed = vocabulary.RecordPath(kind, to+strings.TrimPrefix(id, from)), true
					continue
				}
			}
			moved, c := publishedDeclarationRefs(val, from, to)
			out[k], changed = moved, changed || c
		}
		return out, changed
	case []any:
		out, changed := make([]any, len(x)), false
		for i, val := range x {
			moved, c := publishedDeclarationRefs(val, from, to)
			out[i], changed = moved, changed || c
		}
		return out, changed
	}
	return v, false
}

// publishedSelector is a selector object with the kinds at path (keys
// joined by "/") moved from the package from to the same kinds under to, and
// whether any moved.
func publishedSelector(v any, path, from, to string) (any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return v, false
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		out[k] = val
	}
	key, rest, nested := strings.Cut(path, "/")
	if nested {
		moved, changed := publishedSelector(m[key], rest, from, to)
		out[key] = moved
		return out, changed
	}
	list, ok := m[key].([]any)
	if !ok {
		return v, false
	}
	_, name := vocabulary.SplitPackageRef(from)
	authority, _ := vocabulary.SplitPackageRef(to)
	moved, changed := make([]any, len(list)), false
	for i, k := range list {
		moved[i] = k
		if s, ok := k.(string); ok {
			if p := vocabulary.PublishedRef(s, name, authority); p != s {
				moved[i], changed = p, true
			}
		}
	}
	out[key] = moved
	return out, changed
}

// repointScopes moves every scope of an app's token over the local package
// name to the authority to, so an app the owner approved for `~/recipes`
// reaches the same kinds once they are published.
func (t *txn) repointScopes(name, to string) error {
	rows, err := t.query(`SELECT id, props->'scopes' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props ? 'scopes' ORDER BY id`, kindToken)
	if err != nil {
		return err
	}
	moved := map[string][]any{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			_ = rows.Close()
			return err
		}
		var scopes []string
		if err := json.Unmarshal(raw, &scopes); err != nil {
			continue
		}
		next, changed := make([]any, len(scopes)), false
		for i, s := range scopes {
			next[i] = s
			parsed, err := scope.Parse(s)
			if err != nil {
				continue
			}
			if trait, ok := parsed.Trait(); ok {
				if p := vocabulary.PublishedRef(trait, name, to); p != trait {
					parsed.Pattern = scope.TraitPattern(p)
					next[i], changed = parsed.String(), true
				}
				continue
			}
			if p := vocabulary.PublishedRef(parsed.Pattern, name, to); p != parsed.Pattern {
				parsed.Pattern = p
				next[i], changed = parsed.String(), true
			}
		}
		if changed {
			moved[id] = next
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range sortedKeys(moved) {
		if _, err := t.patch(eref{Kind: kindToken, ID: id}, substrate.PatchInput{
			Properties: map[string]any{"scopes": moved[id]},
		}); err != nil {
			return fmt.Errorf("substrate/engine: publish: the scopes of token %s: %w", id, err)
		}
	}
	return nil
}

// PlanPublish answers what publishing in.Package under in.Authority would
// do, counted in one read snapshot, and what refuses it now. It writes
// nothing.
func (ds *dataset) PlanPublish(ctx context.Context, actor substrate.Actor, in substrate.PublishInput) (substrate.PublishPlan, error) {
	p, err := ds.stagePublish(ctx, in)
	if err != nil {
		return substrate.PublishPlan{}, err
	}
	out := p.plan()
	st, err := ds.stageVocabularyBatch(ctx, ds.registry(), &actor, p.batch)
	if err != nil {
		return out, err
	}
	q, done, err := ds.previewReader(ctx)
	if err != nil {
		return out, err
	}
	defer done()
	// Every callable the publish removes is the package's own, and the
	// publish repoints each trigger that calls one before its guard counts
	// them (repointTriggers in the batch's beforeGuards), so the preview,
	// which writes nothing, does not count them as stranded. The
	// transaction's guard still runs after the repoint.
	st.droppedCallables = nil
	blockers, err := st.guards(q)
	if err != nil {
		return out, err
	}
	planned, lines, err := planMoves(q, st.candidate, st.moves)
	if err != nil {
		return out, err
	}
	out.Blockers = append(append(append(out.Blockers, blockers...), lines...), moveGuards(ds.registry(), carrying(planned.moves))...)
	for _, m := range planned.moves {
		out.Records += int64(len(m.ids))
	}
	out.References = int64(len(planned.repoints))
	return out, nil
}

func (p *publishing) plan() substrate.PublishPlan {
	return substrate.PublishPlan{
		Package: p.from, Published: p.to,
		Kinds: append([]string{}, p.kinds...), Packages: append([]string{}, p.others...),
		Blockers: []string{},
	}
}

// Publish moves in.Package to in.Authority and writes its first release, in
// one transaction. The caller has proved the authority (the API reads its
// `_substrate` record): the engine holds what the repository decides alone.
func (ds *dataset) Publish(ctx context.Context, actor substrate.Actor, in substrate.PublishInput) (substrate.PublishPlan, error) {
	// The declarations are read and the batch applied under one hold of the
	// vocabulary write lock, which every declaration write takes, so no edit
	// commits between the copy the publish stages and the batch that
	// replaces the package with it.
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	p, err := ds.stagePublish(ctx, in)
	if err != nil {
		return substrate.PublishPlan{}, err
	}
	out := p.plan()
	var release substrate.Release
	p.batch.onMoves = func(planned movesPlanned) {
		for _, m := range planned.moves {
			out.Records += int64(len(m.ids))
		}
		out.References = int64(len(planned.repoints))
	}
	_, name := vocabulary.SplitPackageRef(p.from)
	authority, _ := vocabulary.SplitPackageRef(p.to)
	p.batch.extra = func(t *txn) error {
		if err := t.repointDeclarationRefs(p.from, p.to); err != nil {
			return err
		}
		if err := t.repointScopes(name, authority); err != nil {
			return err
		}
		var err error
		release, err = t.writeRelease(p.to)
		return err
	}
	if _, err := ds.applyVocabularyBatchLocked(ctx, actor, p.batch); err != nil {
		return out, err
	}
	out.Release = &release
	return out, nil
}

// ReleasePackage writes the next release of a package this repository
// publishes: the package's declarations as it holds them now, at the
// package's next version, refused when nothing changed since the last one.
func (ds *dataset) ReleasePackage(ctx context.Context, actor substrate.Actor, pkg string) (substrate.Release, error) {
	// What changed is judged and the release written under one hold of the
	// vocabulary write lock, as the publish is.
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	releases, err := ds.releasesOf(ctx, pkg)
	if err != nil {
		return substrate.Release{}, err
	}
	if len(releases) == 0 {
		return substrate.Release{}, fmt.Errorf("%w: this repository does not publish %s", substrate.ErrNotFound, pkg)
	}
	last := releases[len(releases)-1]
	rows, err := ds.packageDocumentRows(ctx, ds.db, pkg)
	if err != nil {
		return substrate.Release{}, err
	}
	docs, header := releaseDocuments(rows, pkg)
	if header == nil {
		return substrate.Release{}, fmt.Errorf("%w: %s has no package header", substrate.ErrNotFound, pkg)
	}
	current, err := closureBytes(docs)
	if err != nil {
		return substrate.Release{}, err
	}
	now, err := releaseDigest(pkg, current)
	if err != nil {
		return substrate.Release{}, err
	}
	held, err := ds.ReleaseClosure(ctx, pkg, last.Version)
	if err != nil {
		return substrate.Release{}, err
	}
	then, err := releaseDigest(pkg, held)
	if err != nil {
		return substrate.Release{}, err
	}
	if now == then {
		return substrate.Release{}, fmt.Errorf("%w: %s has not changed since version %d, its last release", substrate.ErrConflict, pkg, last.Version)
	}
	// The release is the package header's version, raised past every
	// release before it, so an installed copy's upgrade reads it as newer.
	version := max(headerVersion(*header), last.Version+1)
	next := documentEnvelope(*header)
	data := next["data"].(map[string]any)
	data["version"] = version
	parsed, err := vocabulary.ParseDocuments([]map[string]any{next})
	if err != nil {
		return substrate.Release{}, err
	}
	var release substrate.Release
	_, err = ds.applyVocabularyBatchLocked(ctx, actor, vocabularyBatch{
		docs: parsed,
		extra: func(t *txn) error {
			var err error
			release, err = t.writeRelease(pkg)
			return err
		},
	})
	return release, err
}

// writeRelease writes the release of pkg as this transaction holds its
// declarations, at its header's version.
func (t *txn) writeRelease(pkg string) (substrate.Release, error) {
	rows, err := t.ds.packageDocumentRows(t.ctx, t.tx, pkg)
	if err != nil {
		return substrate.Release{}, err
	}
	docs, header := releaseDocuments(rows, pkg)
	if header == nil {
		return substrate.Release{}, fmt.Errorf("substrate/engine: release %s: no package header", pkg)
	}
	closure, err := closureBytes(docs)
	if err != nil {
		return substrate.Release{}, err
	}
	sum := sha256.Sum256(closure)
	r := substrate.Release{
		Package: pkg, Version: headerVersion(*header), SHA256: hex.EncodeToString(sum[:]),
		ReleasedAt: t.now,
	}
	if d, ok := vocabulary.VersionValue(header.Data["dialect"]); ok {
		r.Dialect = d
	}
	id := releaseID(pkg, r.Version)
	held, err := t.loadRow(eref{Kind: kindRelease, ID: id}, true)
	if err != nil {
		return substrate.Release{}, err
	}
	if held != nil {
		return substrate.Release{}, fmt.Errorf("%w: %s version %d is released already", substrate.ErrConflict, pkg, r.Version)
	}
	props := map[string]any{
		"package": pkg, "version": r.Version, "sha256": r.SHA256, "closure": string(closure),
	}
	if r.Dialect > 0 {
		props["dialect"] = r.Dialect
	}
	if _, err := t.put(substrate.PutInput{Kind: kindRelease, ID: id, Properties: props}); err != nil {
		return substrate.Release{}, fmt.Errorf("substrate/engine: release %s version %d: %w", pkg, r.Version, err)
	}
	return r, nil
}

// releaseDocuments are the documents of pkg a release carries, in the order
// its closure lists them, and the package header among them.
func releaseDocuments(rows map[string]vocabulary.Document, pkg string) ([]vocabulary.Document, *vocabulary.Document) {
	docs := make([]vocabulary.Document, 0, len(rows))
	for _, key := range sortedKeys(rows) {
		docs = append(docs, rows[key])
	}
	sort.SliceStable(docs, func(i, j int) bool {
		if docs[i].Kind != docs[j].Kind {
			return docs[i].Kind < docs[j].Kind
		}
		return docs[i].ID < docs[j].ID
	})
	var header *vocabulary.Document
	for i := range docs {
		if docs[i].Kind == vocabulary.DocPackage && docs[i].ID == pkg {
			header = &docs[i]
		}
	}
	return docs, header
}

func headerVersion(d vocabulary.Document) int64 {
	v, _ := vocabulary.VersionValue(d.Data["version"])
	return max(v, 1)
}

// closureBytes is the YAML stream a release serves: each document as an
// envelope, keys in order, so the bytes are a function of the declarations.
func closureBytes(docs []vocabulary.Document) ([]byte, error) {
	var b bytes.Buffer
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		raw, err := yaml.Marshal(documentEnvelope(d))
		if err != nil {
			return nil, fmt.Errorf("substrate/engine: closure %s: %w", d.ID, err)
		}
		b.Write(raw)
	}
	return b.Bytes(), nil
}

// documentEnvelope is a stored declaration as the document that declares it,
// in the shapes a decoded document has (a list is []any, a number a float),
// so the walks that read a document by its grammar read it whole.
func documentEnvelope(d vocabulary.Document) map[string]any {
	meta := map[string]any{"id": d.ID}
	if len(d.Labels) > 0 {
		meta["labels"] = d.Labels
	}
	if len(d.Annotations) > 0 {
		meta["annotations"] = d.Annotations
	}
	env := map[string]any{"kind": vocabulary.CoreKind(d.Kind), "metadata": meta, "data": d.Data}
	raw, err := json.Marshal(env)
	if err != nil {
		return env
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return env
	}
	return out
}

func releaseID(pkg string, version int64) string {
	return vocabulary.DerivedMintedID("release", pkg+"@"+strconv.FormatInt(version, 10))
}

// releasesOf lists the releases of pkg, oldest first.
func (ds *dataset) releasesOf(ctx context.Context, pkg string) ([]substrate.Release, error) {
	all, err := ds.Releases(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []substrate.Release
	for _, r := range all {
		if r.Package == pkg {
			out = append(out, r)
		}
	}
	return out, nil
}

// Releases lists every release of the packages this repository publishes
// under authority, oldest first within each package; every release when
// authority is empty.
func (ds *dataset) Releases(ctx context.Context, authority string) ([]substrate.Release, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT props->>'package', (props->>'version')::bigint, props->>'sha256',
		       COALESCE((props->>'dialect')::bigint, 0), created_at
		FROM records WHERE kind = $1 AND deleted_at IS NULL
		ORDER BY props->>'package', (props->>'version')::bigint`, kindRelease)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.Release
	for rows.Next() {
		var r substrate.Release
		var at time.Time
		if err := rows.Scan(&r.Package, &r.Version, &r.SHA256, &r.Dialect, &at); err != nil {
			return nil, err
		}
		r.ReleasedAt = at.UTC()
		if a, _ := vocabulary.SplitPackageRef(r.Package); authority == "" || a == authority {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// ReleaseClosure is the closure of one release, byte for byte.
func (ds *dataset) ReleaseClosure(ctx context.Context, pkg string, version int64) ([]byte, error) {
	var closure string
	err := ds.db.QueryRowContext(ctx, `SELECT props->>'closure' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		kindRelease, releaseID(pkg, version)).Scan(&closure)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s has no release %d here", substrate.ErrNotFound, pkg, version)
	}
	return []byte(closure), err
}

// releaseDigest is closureDigest over a closure's bytes, versions left out,
// to tell whether a package changed since a release: both sides are read
// back from the same YAML, so only what the declarations say can differ.
func releaseDigest(pkg string, raw []byte) (string, error) {
	var docs []map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		docs = append(docs, m)
	}
	parsed, err := vocabulary.ParseDocuments(docs)
	if err != nil {
		return "", err
	}
	return closureDigest(pkg, parsed)
}

// mapString is the string at a path of nested maps, or "".
func mapString(m map[string]any, path ...string) string {
	var v any = m
	for _, k := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return ""
		}
		v = mm[k]
	}
	s, _ := v.(string)
	return s
}
