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
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
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
	texts    []string
	rename   *publishRename
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
	if err := ds.refusePublishName(ctx, to); err != nil {
		return nil, err
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
	p := &publishing{from: from, to: published, texts: vocabulary.PublishTexts(docs, name, to, ds.names())}
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
		data := map[string]any{"authority": to, "package": name, "installs": installs}
		// What another server must hold before the package compiles there:
		// every published package it names. The binary's own seeded
		// packages are in every repository already.
		var own []map[string]any
		for _, d := range moved {
			if mapString(d, "data", "authority")+"/"+mapString(d, "data", "package") == published {
				own = append(own, d)
			}
		}
		var requires []any
		for _, pkg := range vocabulary.PackagesNamedIn(own) {
			if _, seeded := ds.svc.base.PackageByName(pkg); pkg != published && !seeded {
				requires = append(requires, pkg)
			}
		}
		if len(requires) > 0 {
			data["requires"] = requires
		}
		moved = append(moved, map[string]any{
			"kind":     vocabulary.CoreKind(vocabulary.DocBundle),
			"metadata": map[string]any{"id": published},
			"data":     data,
		})
	}
	parsed, err := vocabulary.ParseDocuments(moved)
	if err != nil {
		return nil, err
	}
	sort.Strings(p.others)
	p.others = slices.Compact(p.others)
	p.rename = &publishRename{
		from: from, to: published, pkg: name, authority: to,
		spellings: ds.names(), kinds: publish,
	}
	p.batch = vocabularyBatch{
		docs:            parsed,
		replacePackages: []string{from},
		publish:         publish,
		publishTo:       published,
		// Under the batch's barrier, before the dropped-callable guard
		// counts the triggers: a trigger that calls or watches the package
		// follows it (publishnames.go).
		beforeGuards: func(t *txn) error {
			return t.publishRecordNames(p.rename, true)
		},
	}
	return p, nil
}

// refusePublishName refuses, before anything is staged, a publish under a
// name the repository may not publish under (decision record 0289): one it
// answers to as a former name, which the 90 days it holds it for are for
// coming back to, and which a published name, never spelling `~` again,
// would cut short; and one another repository on this server answers to,
// now or as a former name in force. Its own name is 0289's case and goes
// ahead. These are early refusals, and they take no lock: what holds is the
// fold's rule, that a name under which the repository holds a package that
// is not `~`'s spells no `~` (Registry.Spellings), so a rename to the name
// landing beside the publish, in either order, leaves the name naming the
// published package.
func (ds *dataset) refusePublishName(ctx context.Context, to string) error {
	svc := ds.svc
	stored, err := svc.readNames(ctx, ds.info.ID)
	if err != nil {
		return err
	}
	now := svc.namesNow()
	for _, f := range stored.former {
		if f.name == to && now.Before(f.until()) {
			return fmt.Errorf("%w: %s is a former name of this repository, which answers to it until %s: rename the repository back to it to publish under it, or publish under it once it lapses",
				substrate.ErrConflict, to, f.until().UTC().Format(time.RFC3339))
		}
	}
	holder, err := svc.repositoryByNameOn(ctx, svc.maint, to)
	switch {
	case errors.Is(err, substrate.ErrNotFound):
	case err != nil:
		return err
	case holder.ID != ds.info.ID:
		return fmt.Errorf("%w: another repository on this server answers to %s, so a package of this one is not published under it", substrate.ErrConflict, to)
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

// PlanPublish answers what publishing in.Package under in.Authority would
// do and what refuses it now, from the publish itself: its whole batch runs
// and its transaction rolls back (vocabularyBatch.rollback), so a preview
// with no blocker is a publish that goes through, and one with a blocker
// names what the confirm would be refused for. It writes nothing.
func (ds *dataset) PlanPublish(ctx context.Context, actor substrate.Actor, in substrate.PublishInput) (substrate.PublishPlan, error) {
	return ds.publish(ctx, actor, in, true)
}

// Publish moves in.Package to in.Authority and writes its first release, in
// one transaction. The caller has proved the authority (the API reads its
// `_substrate` record): the engine holds what the repository decides alone.
func (ds *dataset) Publish(ctx context.Context, actor substrate.Actor, in substrate.PublishInput) (substrate.PublishPlan, error) {
	return ds.publish(ctx, actor, in, false)
}

// publish is the publish, or its preview: the same batch, committed or
// rolled back. Its declarations are read and the batch applied under one
// hold of the vocabulary write lock, which every declaration write takes,
// so no edit commits between the copy the publish stages and the batch that
// replaces the package with it.
func (ds *dataset) publish(ctx context.Context, actor substrate.Actor, in substrate.PublishInput, preview bool) (substrate.PublishPlan, error) {
	if err := ds.mayPublish(ctx, actor); err != nil {
		return substrate.PublishPlan{}, err
	}
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	p, err := ds.stagePublish(ctx, in)
	if err != nil {
		return substrate.PublishPlan{}, err
	}
	out := p.plan()
	if prove := substrate.PublishProofFrom(ctx); prove != nil && !preview {
		if err := prove(p.rename.authority); err != nil {
			return out, err
		}
	}
	var own []vocabulary.Document
	for _, d := range p.batch.docs {
		if d.DeclaredPackage() == p.to {
			own = append(own, d)
		}
	}
	deps, err := ds.resolveRequirements(ctx, own)
	if err != nil {
		return out, err
	}
	var release substrate.Release
	p.batch.onMoves = func(planned movesPlanned) {
		for _, m := range planned.moves {
			out.Records += int64(len(m.ids))
		}
		out.References = int64(len(planned.repoints))
	}
	p.batch.extra = func(t *txn) error {
		if err := t.publishRecordNames(p.rename, false); err != nil {
			return err
		}
		var err error
		release, _, err = t.writeRelease(p.to, deps)
		return err
	}
	p.batch.rollback = preview
	_, err = ds.applyVocabularyBatchLocked(ctx, actor, p.batch)
	// The records whose author's text the rename left naming the package,
	// beside the declarations' prompts.
	out.Texts = append(out.Texts, p.rename.texts...)
	sort.Strings(out.Texts)
	out.Texts = slices.Compact(out.Texts)
	if err != nil {
		if preview && refusal(err) {
			out.Blockers = append(out.Blockers, refusalLines(err)...)
			return out, nil
		}
		return out, err
	}
	if !preview {
		out.Release = &release
	}
	return out, nil
}

func (p *publishing) plan() substrate.PublishPlan {
	return substrate.PublishPlan{
		Package: p.from, Published: p.to,
		Kinds: append([]string{}, p.kinds...), Packages: append([]string{}, p.others...),
		Blockers: []string{}, Texts: append([]string{}, p.texts...),
	}
}

// mayPublish refuses a publish or a release, and their previews, to any
// hand but a person who holds `admin`: it writes the package's declarations
// under a name of its own, rewrites what names it (tokens' scopes,
// triggers, policies, views), and serves what it writes in public. The
// route is an admin's too; this holds every other door to the same.
func (ds *dataset) mayPublish(ctx context.Context, actor substrate.Actor) error {
	person := ds.personActorFor(ctx, actor)
	if substrate.ActorTypeOf(person) != substrate.ActorTypePerson {
		return fmt.Errorf("%w: %w: publishing a package is a person's: %s is not one", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(actor))
	}
	if _, scoped := substrate.ScopesFrom(ctx); scoped {
		return fmt.Errorf("%w: %w: an app's token does not publish a package", substrate.ErrForbidden, substrate.ErrNotPermitted)
	}
	roles, err := ds.rolesIn(ctx, ds.db, person)
	if err != nil {
		return err
	}
	if !roles.Admin() {
		return fmt.Errorf("%w: %w: publishing a package is an admin's: it gives it a name another server installs it by, for good", substrate.ErrForbidden, substrate.ErrNotPermitted)
	}
	return nil
}

// refusal reports whether err refuses what was asked, as a blocker a
// preview names, rather than failing to answer.
func refusal(err error) bool {
	var invalid *substrate.ValidationError
	return errors.As(err, &invalid) || errors.Is(err, substrate.ErrValidation) || errors.Is(err, substrate.ErrGuard) ||
		errors.Is(err, substrate.ErrConflict) || errors.Is(err, substrate.ErrForbidden) || errors.Is(err, substrate.ErrNotFound)
}

// refusalLines are the lines a refusal names: a validation's problems, or
// a guard's lines, or the error whole.
func refusalLines(err error) []string {
	var invalid *substrate.ValidationError
	if errors.As(err, &invalid) && len(invalid.Problems) > 0 {
		return append([]string(nil), invalid.Problems...)
	}
	msg := err.Error()
	if errors.Is(err, substrate.ErrGuard) {
		msg = strings.TrimPrefix(msg, substrate.ErrGuard.Error()+": ")
		return strings.Split(msg, "; ")
	}
	return []string{msg}
}

// ReleasePackage answers the next release of a package this repository
// publishes, and writes it when in.Confirm says so: the package's
// declarations as it holds them now, at the package's next version,
// refused when nothing changed since the last one. Its preview is the
// release itself rolled back, as a publish's is.
func (ds *dataset) ReleasePackage(ctx context.Context, actor substrate.Actor, in substrate.ReleaseInput) (substrate.ReleasePlan, error) {
	pkg := in.Package
	out := substrate.ReleasePlan{Package: pkg, Declarations: []string{}, Blockers: []string{}}
	if err := ds.mayPublish(ctx, actor); err != nil {
		return out, err
	}
	refuse := func(err error) (substrate.ReleasePlan, error) {
		if !in.Confirm && refusal(err) {
			out.Blockers = append(out.Blockers, refusalLines(err)...)
			return out, nil
		}
		return out, err
	}
	// What changed is judged and the release written under one hold of the
	// vocabulary write lock, as the publish is.
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()
	releases, err := ds.releasesOf(ctx, pkg)
	if err != nil {
		return out, err
	}
	if len(releases) == 0 {
		return out, fmt.Errorf("%w: this repository does not publish %s", substrate.ErrNotFound, pkg)
	}
	last := releases[len(releases)-1]
	out.Previous = last.Version
	if prove := substrate.PublishProofFrom(ctx); prove != nil && in.Confirm {
		authority, _ := vocabulary.SplitPackageRef(pkg)
		if err := prove(authority); err != nil {
			return out, err
		}
	}
	rows, err := ds.packageDocumentRows(ctx, ds.db, pkg)
	if err != nil {
		return out, err
	}
	docs, header := releaseDocuments(rows, pkg)
	if header == nil {
		return out, fmt.Errorf("%w: %s has no package header", substrate.ErrNotFound, pkg)
	}
	current, err := closureBytes(docs)
	if err != nil {
		return out, err
	}
	now, err := releaseDigest(pkg, current)
	if err != nil {
		return out, err
	}
	held, err := ds.ReleaseClosure(ctx, pkg, last.Version)
	if err != nil {
		return out, err
	}
	then, err := releaseDigest(pkg, held)
	if err != nil {
		return out, err
	}
	// The release is the package header's version, raised past every
	// release before it, so an installed copy's upgrade reads it as newer.
	out.Version = max(headerVersion(*header), last.Version+1)
	if now == then {
		return refuse(fmt.Errorf("%w: %s has not changed since version %d, its last release", substrate.ErrConflict, pkg, last.Version))
	}
	next := documentEnvelope(*header)
	data := next["data"].(map[string]any)
	data["version"] = out.Version
	parsed, err := vocabulary.ParseDocuments([]map[string]any{next})
	if err != nil {
		return out, err
	}
	deps, err := ds.resolveRequirements(ctx, docs)
	if err != nil {
		return out, err
	}
	var release substrate.Release
	_, err = ds.applyVocabularyBatchLocked(ctx, actor, vocabularyBatch{
		docs:     parsed,
		rollback: !in.Confirm,
		extra: func(t *txn) error {
			var err error
			release, out.Declarations, err = t.writeRelease(pkg, deps)
			return err
		},
	})
	if err != nil {
		return refuse(err)
	}
	out.SHA256 = release.SHA256
	if in.Confirm {
		out.Release = &release
	}
	return out, nil
}

// writeRelease writes the release of pkg as this transaction holds its
// declarations, at its header's version, once an installing server's
// admission takes the closure it will serve (admitRelease), and answers it
// with the declarations it holds.
func (t *txn) writeRelease(pkg string, deps *releaseRequirements) (substrate.Release, []string, error) {
	rows, err := t.ds.packageDocumentRows(t.ctx, t.tx, pkg)
	if err != nil {
		return substrate.Release{}, nil, err
	}
	docs, header := releaseDocuments(rows, pkg)
	if header == nil {
		return substrate.Release{}, nil, fmt.Errorf("substrate/engine: release %s: no package header", pkg)
	}
	declarations := make([]string, 0, len(docs))
	for _, d := range docs {
		declarations = append(declarations, vocabulary.RecordPath(vocabulary.CoreKind(d.Kind), d.ID))
	}
	closure, err := closureBytes(docs)
	if err != nil {
		return substrate.Release{}, declarations, err
	}
	sum := sha256.Sum256(closure)
	r := substrate.Release{
		Package: pkg, Version: headerVersion(*header), SHA256: hex.EncodeToString(sum[:]),
		ReleasedAt: t.now,
	}
	if d, ok := vocabulary.VersionValue(header.Data["dialect"]); ok {
		r.Dialect = d
	}
	if err := t.admitRelease(pkg, r.Version, closure, deps); err != nil {
		return r, declarations, err
	}
	id := releaseID(pkg, r.Version)
	held, err := t.loadRow(eref{Kind: kindRelease, ID: id}, true)
	if err != nil {
		return r, declarations, err
	}
	if held != nil {
		return r, declarations, fmt.Errorf("%w: %s version %d is released already", substrate.ErrConflict, pkg, r.Version)
	}
	props := map[string]any{
		"package": pkg, "version": r.Version, "sha256": r.SHA256, "closure": string(closure),
	}
	if r.Dialect > 0 {
		props["dialect"] = r.Dialect
	}
	// A release is a system kind the generic surface never writes: the
	// engine's hand writes it, for the person the publish or the release is
	// held to (mayPublish).
	prev := t.internal
	t.internal = true
	defer func() { t.internal = prev }()
	if _, err := t.put(substrate.PutInput{Kind: kindRelease, ID: id, Properties: props}); err != nil {
		return r, declarations, fmt.Errorf("substrate/engine: release %s version %d: %w", pkg, r.Version, err)
	}
	return r, declarations, nil
}

// admitRelease runs an installing server's admission on a release's
// closure, the bytes it will serve, as a dry run (decision record 0291).
// What it runs, and what of the install path it does not and why, is
// releaseAdmission's table:
//   - the catalog's admission of a closure a document serves (its boundary,
//     a kind it renames, a name in `~`: catalog.AdmitClosure);
//   - what a published package's declarations are held to
//     (admitPublishedDocuments, validateOutsideCode);
//   - the compile of its declarations into a registry that holds only what
//     a fresh repository holds, the binary's own vocabulary, with no name of
//     this repository's, and the packages it requires as an installing
//     server receives them (resolveRequirements, compileCandidate), each
//     as the installer applies it (catalog.AdmitSuggested);
//   - the declared units against a fresh repository's unit graph, the
//     seed's (checkDeclaredUnits' own rule, declaredUnitProblems).
//
// It writes nothing.
func (t *txn) admitRelease(pkg string, version int64, closure []byte, deps *releaseRequirements) error {
	refuse := func(err error) error {
		return fmt.Errorf("%w: %s version %d, as another server installs it: %w", substrate.ErrValidation, pkg, version, err)
	}
	vocab, _, err := catalog.AdmitClosure(closure, pkg, version)
	if err != nil {
		return refuse(err)
	}
	docs, err := vocabulary.ParseDocuments(vocab)
	if err != nil {
		return refuse(err)
	}
	if err := admitPublishedDocuments(docs); err != nil {
		return refuse(err)
	}
	if _, _, err := validateOutsideCode(docs, nil); err != nil {
		return refuse(err)
	}
	if deps == nil {
		deps = &releaseRequirements{}
	}
	if len(deps.problems) > 0 {
		return refuse(errors.New(strings.Join(deps.problems, "; ")))
	}
	// A fresh repository answers to no name of this one's, so nothing in
	// the closure reads as `~` but `~` itself, and the install's refusal of
	// a closure from a name the repository answers to
	// (refuseInstallFromName) has no name to meet.
	fresh := t.ds.svc.base.Clone()
	fresh.SetNames(func() []string { return nil })
	published := func(string) string { return vocabulary.SourcePublished }
	// What an installer applies (catalog.AdmitSuggested): it installs the
	// requirements first, so each suggested mapping is read against the
	// kinds a fresh repository then holds, the seed's and the requirements'.
	reqs := make([]map[string]any, 0, len(deps.docs))
	for _, d := range deps.docs {
		reqs = append(reqs, documentEnvelope(d))
	}
	held := fresh
	if len(deps.packages) > 0 {
		none := func(string) (substrate.KindInfo, bool) { return substrate.KindInfo{}, false }
		bare, err := vocabulary.ParseDocuments(catalog.AdmitSuggested(reqs, none))
		if err != nil {
			return refuse(err)
		}
		if held, err = t.ds.compileCandidate(fresh, bare, deps.packages, published); err != nil {
			return refuse(err)
		}
	}
	kinds := func(ref string) (substrate.KindInfo, bool) {
		k, ok := held.ByIdentity(ref)
		if !ok {
			return substrate.KindInfo{}, false
		}
		return typeInfoIn(held, k), true
	}
	applied, err := vocabulary.ParseDocuments(catalog.AdmitSuggested(append(reqs, vocab...), kinds))
	if err != nil {
		return refuse(err)
	}
	touched := map[string]bool{pkg: true}
	for p := range deps.packages {
		touched[p] = true
	}
	candidate, err := t.ds.compileCandidate(fresh, applied, touched, published)
	if err != nil {
		return refuse(err)
	}
	graph, err := freshUnitGraph()
	if err != nil {
		return err
	}
	if g, ok := candidate.PackageByName(pkg); ok {
		var problems []string
		for _, name := range g.KindOrder {
			k := g.Kinds[name]
			for _, prop := range sortedKeys(k.Props) {
				if holdsQuantity(k.Props[prop]) {
					problems = append(problems, declaredUnitProblems(graph, k.Identity+" data.properties."+prop, k.Props[prop])...)
				}
			}
		}
		if len(problems) > 0 {
			return refuse(&substrate.ValidationError{Problems: problems})
		}
	}
	return nil
}

// releaseAdmission is every check the install path runs (what
// InstallBundleClosure calls, and what a transaction's end calls after its
// work, dataset.go inTxOn), each with what a release's admission does about
// it: run, or not, with the reason. TestEveryInstallCheckHasAReleaseAnswer
// reads the install path's source and fails on a call in neither, so the
// next rule the installer gains is a choice its author makes.
var releaseAdmission = map[string]struct {
	run bool
	why string
}{
	// InstallBundleClosure.
	"refuseInstallFromName":      {false, "it refuses a closure from a name the installer answers to: a fresh repository answers to none"},
	"closureAuthorities":         {false, "the input refuseInstallFromName reads"},
	"localizeDocuments":          {false, "it folds the installer's names into ~: a fresh repository has none, and the catalog's admission refuses a ~ in a published closure"},
	"ParseDocuments":             {true, "admitRelease parses the closure the catalog admits"},
	"refuseShippedUnderDocument": {false, "it refuses a package the binary ships under the project's name, which no publish uses (decision record 0292)"},
	"admitPublishedDocuments":    {true, "admitRelease runs it"},
	"relaxFloorsOnOwnPackages":   {false, "it relaxes floors on packages the installer owns: a fresh repository owns none"},
	"admitOutsideClosure":        {true, "admitRelease runs its rule on the closure, validateOutsideCode; whether an installer takes outside code at all is its operator's choice"},
	"applyVocabularyBatch":       {true, "admitRelease runs its compile, compileCandidate, against a fresh repository and what the release requires (resolveRequirements); the rest of the batch compares with the installer's own stored rows, which a fresh repository has none of"},
	"requireAddonConsent":        {false, "an app's consent to an add-on, not the closure's"},
	"SplitPackageRef":            {false, "not a check"},
	"localizeCopied":             {false, "a sample's copy of a record; a release carries declarations alone (releaseDocuments)"},
	"declarations":               {false, "not a check"},
	"validateTriggerRow":         {false, "a shipped trigger's check; a release carries declarations alone"},
	"refuseTakenTrigger":         {false, "a shipped trigger's check; a release carries declarations alone"},
	"keepTriggerSettings":        {false, "a shipped trigger's settings; a release carries declarations alone"},
	"anotherHandWrote":           {false, "a shipped record's check; a release carries declarations alone"},
	"put":                        {false, "a shipped record's write; a release carries declarations alone"},
	"docKey":                     {false, "not a check"},
	"name":                       {false, "not a check"},
	// A transaction's end (dataset.go inTxOn).
	"checkUnitGraph":     {false, "a release carries no unit or conversion record, so it leaves a fresh repository's graph as it found it"},
	"checkDeclaredUnits": {true, "admitRelease holds every quantity the release declares to the seed's unit graph, by the same rule (declaredUnitProblems)"},
	"checkMetricValues":  {false, "a release carries no record a metric rule reads"},
	"settleObserved":     {false, "settling the installer's own writes, which the admission does not make"},
	"settleFold":         {false, "settling the installer's own writes, which the admission does not make"},
	"settleChecksums":    {false, "settling the installer's own writes, which the admission does not make"},
	"unsettledErr":       {false, "the installer's readiness to commit (its dataset settled), not the closure's"},
	"commit":             {false, "the installer's commit"},
	"commitDoubted":      {false, "the installer's commit"},
	"settleActorCache":   {false, "the installer's cache, after its commit"},
	"check":              {false, "a test's end-of-transaction refusal (WithTestEndOfTransactionCheck)"},
	"beforeSignal":       {false, "a test's hook before the commit's signal"},
	"countChanges":       {false, "the installer's metrics, after its commit"},
	"fn":                 {false, "the after-commit work the installer's commit queued"},
}

// releaseRequirements are what an installing server receives for every
// package a release requires, transitively: their declarations, the
// packages, and a line for each requirement nothing satisfies.
type releaseRequirements struct {
	docs     []vocabulary.Document
	packages map[string]bool
	problems []string
}

// resolveRequirements reads every package the declarations docs require,
// transitively, as an installing server receives it, never from this
// repository's working declarations:
//   - a package the binary seeds is in every repository already;
//   - one this repository publishes comes from its latest release at or
//     above the floor, the bytes this server serves for it;
//   - any other from the door's resolver (the catalog's,
//     substrate.ReleaseResolverFrom); with none to ask, it is a problem,
//     because this repository's own copy may be older than what an
//     installing server receives.
//
// A requirement none of that satisfies is a problem, named, which stops the
// release.
func (ds *dataset) resolveRequirements(ctx context.Context, docs []vocabulary.Document) (*releaseRequirements, error) {
	out := &releaseRequirements{packages: map[string]bool{}}
	published, err := releasesIn(ctx, ds.db, ds.info.ID, "")
	if err != nil {
		return nil, err
	}
	resolve := substrate.ReleaseResolverFrom(ctx)
	queue := requirementFloors(docs)
	for len(queue) > 0 {
		next := map[string]int64{}
		for _, req := range sortedKeys(queue) {
			floor := queue[req]
			// A requirement met once is not resolved again at a higher floor
			// met later: the compile refuses a version below a floor
			// (requiresAtLeast), naming both.
			if _, seeded := ds.svc.base.PackageByName(req); seeded || out.packages[req] {
				continue
			}
			held, problem, err := ds.requiredDeclarations(ctx, req, floor, published, resolve)
			if err != nil {
				return nil, err
			}
			if problem != "" {
				out.problems = append(out.problems, problem)
				continue
			}
			out.packages[req] = true
			out.docs = append(out.docs, held...)
			for r, f := range requirementFloors(held) {
				next[r] = max(next[r], f)
			}
		}
		queue = next
	}
	return out, nil
}

// requiredDeclarations are the declarations an installing server receives
// for the required package req at or above floor, or the reason it
// receives none.
func (ds *dataset) requiredDeclarations(ctx context.Context, req string, floor int64, published []substrate.Release, resolve substrate.ReleaseResolver) ([]vocabulary.Document, string, error) {
	var latest *substrate.Release
	for i := range published {
		if published[i].Package == req && (latest == nil || published[i].Version > latest.Version) {
			latest = &published[i]
		}
	}
	if latest != nil {
		if latest.Version < floor {
			return nil, fmt.Sprintf("it requires %s at version %d or later, and the latest release of it here is %d: write its next release first", req, floor, latest.Version), nil
		}
		raw, err := releaseClosureIn(ctx, ds.db, ds.info.ID, req, latest.Version)
		if err != nil {
			return nil, "", err
		}
		vocab, _, err := catalog.AdmitClosure(raw, req, latest.Version)
		if err != nil {
			return nil, fmt.Sprintf("it requires %s, whose release %d does not install: %v", req, latest.Version, err), nil
		}
		docs, err := vocabulary.ParseDocuments(vocab)
		if err != nil {
			return nil, fmt.Sprintf("it requires %s, whose release %d does not parse: %v", req, latest.Version, err), nil
		}
		return docs, "", nil
	}
	if resolve != nil {
		vocab, err := resolve(ctx, req, floor)
		if err != nil {
			return nil, fmt.Sprintf("it requires %s, which an installing server cannot get: %v", req, err), nil
		}
		docs, err := vocabulary.ParseDocuments(vocab)
		if err != nil {
			return nil, fmt.Sprintf("it requires %s, whose published declarations do not parse: %v", req, err), nil
		}
		return docs, "", nil
	}
	if vocabulary.IsLocal(req) {
		return nil, fmt.Sprintf("it requires %s, which is not published, so an installing server cannot get it: publish it first, or stop requiring it", req), nil
	}
	return nil, fmt.Sprintf("it requires %s, and there is no catalog here to ask what an installing server receives of it: publish through the server's API, which asks its catalog", req), nil
}

// requirementFloors are the packages docs' bundles require, each with the
// least version it takes: `requires` at none, `requiresAtLeast` at its
// floor.
func requirementFloors(docs []vocabulary.Document) map[string]int64 {
	out := map[string]int64{}
	for _, d := range docs {
		if d.Kind != vocabulary.DocBundle {
			continue
		}
		if list, ok := d.Data["requires"].([]any); ok {
			for _, r := range list {
				if s, ok := r.(string); ok {
					out[s] = max(out[s], 0)
				}
			}
		}
		if floors, ok := d.Data["requiresAtLeast"].(map[string]any); ok {
			for r, f := range floors {
				v, _ := vocabulary.VersionValue(f)
				out[r] = max(out[r], v)
			}
		}
	}
	return out
}

// freshUnitGraph is the unit graph a fresh repository holds: the seed's
// units and conversions (unitseed.go), built once.
func freshUnitGraph() (*vocabulary.UnitGraph, error) {
	freshUnits.once.Do(func() {
		recs, err := shippedUnitRecords()
		if err != nil {
			freshUnits.err = err
			return
		}
		set := make(map[eref]map[string]any, len(recs))
		for _, r := range recs {
			set[r.ref()] = r.props
		}
		freshUnits.graph, _ = seedGraph(set)
	})
	return freshUnits.graph, freshUnits.err
}

var freshUnits struct {
	once  sync.Once
	graph *vocabulary.UnitGraph
	err   error
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
	return releasesIn(ctx, ds.db, ds.info.ID, authority)
}

// ReleaseClosure is the closure of one release, byte for byte.
func (ds *dataset) ReleaseClosure(ctx context.Context, pkg string, version int64) ([]byte, error) {
	return releaseClosureIn(ctx, ds.db, ds.info.ID, pkg, version)
}

// PublishedReleases is Releases of the repository whose id is repository,
// read on the server's own pool without opening it (decision record 0291),
// in one query that reads the repository's row beside its releases: an id
// no repository served here holds (none, one whose deletion began, one set
// aside) has nothing published. It is the maintenance pool, because no
// other role may read that row, and nothing keeps the answer: a door
// anybody may call costs one indexed read.
func (s *service) PublishedReleases(ctx context.Context, repository, authority string) ([]substrate.Release, error) {
	if !vocabulary.ValidRepositoryID(repository) {
		return nil, errNothingPublished
	}
	rows, err := s.maint.QueryContext(ctx, `
		SELECT p.authority, r.props->>'package', (r.props->>'version')::bigint, r.props->>'sha256',
		       COALESCE((r.props->>'dialect')::bigint, 0), r.created_at
		FROM repositories p
		LEFT JOIN records r ON r.repository = p.id AND r.kind = $2 AND r.deleted_at IS NULL
		  AND ($3 = '' OR starts_with(r.props->>'package', $3 || '/'))
		WHERE p.id = $1 AND p.deletion_began_at IS NULL
		ORDER BY r.props->>'package', (r.props->>'version')::bigint`, repository, kindRelease, authority)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.Release
	name, found := "", false
	for rows.Next() {
		var pkg, sha sql.NullString
		var version, dialect sql.NullInt64
		var at sql.NullTime
		if err := rows.Scan(&name, &pkg, &version, &sha, &dialect, &at); err != nil {
			return nil, err
		}
		found = true
		if pkg.Valid {
			out = append(out, substrate.Release{Package: pkg.String, Version: version.Int64, SHA256: sha.String, Dialect: dialect.Int64, ReleasedAt: at.Time.UTC()})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found || s.setAsideOf(repository, name) != nil {
		return nil, errNothingPublished
	}
	return out, nil
}

// PublishedClosure is ReleaseClosure of the repository whose id is
// repository, read as PublishedReleases reads.
func (s *service) PublishedClosure(ctx context.Context, repository, pkg string, version int64) ([]byte, error) {
	if !vocabulary.ValidRepositoryID(repository) {
		return nil, errNothingPublished
	}
	var name string
	var closure sql.NullString
	err := s.maint.QueryRowContext(ctx, `
		SELECT p.authority, r.props->>'closure' FROM repositories p
		LEFT JOIN records r ON r.repository = p.id AND r.kind = $2 AND r.id = $3 AND r.deleted_at IS NULL
		WHERE p.id = $1 AND p.deletion_began_at IS NULL`, repository, kindRelease, releaseID(pkg, version)).Scan(&name, &closure)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, errNothingPublished
	case err != nil:
		return nil, err
	case s.setAsideOf(repository, name) != nil:
		return nil, errNothingPublished
	case !closure.Valid:
		return nil, fmt.Errorf("%w: %s has no release %d here", substrate.ErrNotFound, pkg, version)
	}
	return []byte(closure.String), nil
}

// errNothingPublished answers a public read of an id no repository served
// here holds.
var errNothingPublished = fmt.Errorf("%w: nothing is published here", substrate.ErrNotFound)

// releasesIn lists repository's releases under authority, every one when
// it is empty, on q: the repository's own handle or the server's pool.
func releasesIn(ctx context.Context, q dbx, repository, authority string) ([]substrate.Release, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT props->>'package', (props->>'version')::bigint, props->>'sha256',
		       COALESCE((props->>'dialect')::bigint, 0), created_at
		FROM records WHERE repository = $1 AND kind = $2 AND deleted_at IS NULL
		  AND ($3 = '' OR starts_with(props->>'package', $3 || '/'))
		ORDER BY props->>'package', (props->>'version')::bigint`, repository, kindRelease, authority)
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
		out = append(out, r)
	}
	return out, rows.Err()
}

// releaseClosureIn is one release's closure of repository, on q.
func releaseClosureIn(ctx context.Context, q dbx, repository, pkg string, version int64) ([]byte, error) {
	var closure string
	err := q.QueryRowContext(ctx, `SELECT props->>'closure' FROM records WHERE repository = $1 AND kind = $2 AND id = $3 AND deleted_at IS NULL`,
		repository, kindRelease, releaseID(pkg, version)).Scan(&closure)
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
