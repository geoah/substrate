package engine

// Schema is records. The six schema kinds (and actor) are ordinary rows in
// the repository's records table, written through the one write surface with the
// loader as inline admission:
//
//   - a schema write builds a CANDIDATE registry — the current authorities with the
//     touched ones rebuilt from their record rows plus the incoming documents —
//     and compiles it (CEL guards, templates) BEFORE the transaction opens; a
//     failed closure fails the whole batch with the loader's full problem list;
//   - record rows and changelog rows commit together (the changelog row is the
//     established signal), then the registry pointer publishes under ds.mu —
//     commit IS activation, RCU-style: in-flight function deliveries finish on
//     the snapshot they started with;
//   - one per-repository schema-write mutex serializes schema writes against each
//     other; data writes never wait — they read whichever pointer is current;
//   - deleting a type with live instances is refused, counted inside the same
//     transaction; identities are never reused (history orphans by design);
//   - repository open rebuilds the registry FROM the schema record rows, which
//     is what retired the stored-manifest reload and its restart-to-activate
//     hazard.
//
// The shipped vocabulary is rows like everything else: the embedded tree is
// COPIED into a repository's changelog once, at creation, and afterwards only
// version-keyed upgrade entries touch it (seed.go). The tree never re-asserts
// at open, and who may write a declaration at all is decided in one place —
// authorizeDeclarationWrite.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// vocabularyRecordKinds maps the schema kinds' record type identities to their
// manifest short names — the types whose writes route through admission.
var vocabularyRecordKinds = map[string]string{
	kindPackage:       vocabulary.DocPackage,
	kindAuthority:     vocabulary.DocAuthority,
	kindKind:          vocabulary.DocKind,
	kindTrait:         vocabulary.DocTrait,
	kindPropertyType:  vocabulary.DocPropertyType,
	kindRecordMapping: vocabulary.DocRecordMapping,
	kindFunction:      vocabulary.DocFunction,
	kindAgent:         vocabulary.DocAgent,
	kindActor:         kindActorLocal,
	kindBundle:        vocabulary.DocBundle,
}

// kindActorLocal is actor's manifest short name (vocabulary.DocActor), spelled
// as a const so the map above stays greppable beside the identities.
const kindActorLocal = vocabulary.DocActor

// schemaKindRef resolves a manifest kind's SHORT name ("kind")
// back to its full type identity ("substrate.reamde.dev/core/kind").
func schemaKindRef(short string) (string, bool) {
	for ident, s := range vocabularyRecordKinds {
		if s == short {
			return ident, true
		}
	}
	return "", false
}

// vocabularyKindRefs lists the schema kinds' identities in one stable
// order, for SQL IN clauses.
var vocabularyKindRefs = func() []string {
	out := make([]string, 0, len(vocabularyRecordKinds))
	for ident := range vocabularyRecordKinds {
		out = append(out, ident)
	}
	sort.Strings(out)
	return out
}()

// vocabularyDocMeta carries the row-write extras a schema document travels with:
// labels/annotations and the CAS precondition.
type vocabularyDocMeta struct {
	labels      map[string]any
	annotations map[string]any
	ifVersion   *int64
}

// vocabularyBatch is one admission unit: documents to upsert, documents to
// remove, and authorities replaced wholesale (their absent declarations prune).
type vocabularyBatch struct {
	docs            []vocabulary.Document
	deletes         []vocabulary.Document
	replacePackages []string
	meta            map[string]vocabularyDocMeta // keyed by docKey
	// published marks a PROVIDER install (catalog Install, decision record
	// 0048): every package the batch touches lands `source: published`, which
	// promotes one a previous install left `installed`. Nothing else sets it,
	// so a hand `apply -f` of the same closure stays the repository's own.
	published bool
	// beforeGuards runs INSIDE the batch transaction, right after the
	// registry-dependency lock and BEFORE the refuse-breakage guards: a bundle
	// uninstall tears its delivery wiring (triggers referencing the owned
	// authority's callables) down here, so the dropped-callable guard sees them
	// already gone and the whole-package teardown never refuses on its own
	// triggers. A guard that fires afterwards rolls this back with the batch.
	beforeGuards func(t *txn) error
	// extra runs INSIDE the batch transaction, after the projection, against
	// the compiled candidate registry: connector registration writes its
	// default triggers and bookkeeping row here, so schema rows, triggers
	// and bookkeeping land — or fail — together, and the registry publishes
	// only after the whole installation committed.
	extra func(t *txn, candidate *vocabulary.Registry) error
}

func docKey(d vocabulary.Document) string { return d.Kind + "\x00" + d.ID }

// parseVocabularyDocs parses raw envelope maps into documents, every
// document's problems collected into one ValidationError.
func parseVocabularyDocs(raw []map[string]any) ([]vocabulary.Document, error) {
	var docs []vocabulary.Document
	var problems []string
	for _, r := range raw {
		d, err := vocabulary.DocumentFromMap(r)
		if err != nil {
			var ve *substrate.ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, ve.Problems...)
				continue
			}
			return nil, err
		}
		docs = append(docs, d)
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	return docs, nil
}

// ApplyVocabularyDocuments is the batch apply verb: every document admitted or
// none, one transaction, activation on commit. Documents wear the same
// kind/metadata/data envelope the loader has always parsed.
func (ds *dataset) ApplyVocabularyDocuments(ctx context.Context, actor substrate.Actor, raw []map[string]any) ([]*substrate.Record, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	docs, err := parseVocabularyDocs(raw)
	if err != nil {
		return nil, err
	}
	written, err := ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{docs: docs})
	if err != nil {
		return nil, err
	}
	out := make([]*substrate.Record, 0, len(docs))
	for _, d := range docs {
		if e, ok := written[docKey(d)]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// InstallBundleClosure admits a bundle's schema closure AND its shipped data
// documents (the delivery wiring — triggers) as ONE repository transaction: the
// schema rows project, then every data document PUTs against the candidate
// registry, and the whole installation commits or rolls back together. A data
// document that fails to admit — a malformed trigger, an unresolvable callable,
// a failed write — rolls the schema apply back with it, so a failed install
// never leaves a live half-installed closure. The data documents
// are UPSERTS: a re-install refreshes the wiring in place. Their trigger
// callables resolve against the CANDIDATE registry, so a trigger may name a
// function the same closure installs — the same seam RegisterConnector's
// default triggers ride (dataset.go).
func (ds *dataset) InstallBundleClosure(ctx context.Context, actor substrate.Actor, vocabularyDocs []map[string]any, dataDocs []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	if len(vocabularyDocs) == 0 {
		return nil, fmt.Errorf("%w: no schema documents", substrate.ErrValidation)
	}
	docs, err := parseVocabularyDocs(vocabularyDocs)
	if err != nil {
		return nil, err
	}
	written, err := ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{
		docs:      docs,
		published: opts.Published,
		extra: func(t *txn, candidate *vocabulary.Registry) error {
			for _, in := range dataDocs {
				// A trigger's callable is validated against the CANDIDATE here:
				// the internal put below skips the callable check (the registry
				// pointer has not published yet), so an unresolvable trigger
				// would otherwise slip in — exactly as installDefaultTriggers
				// guards its writes.
				if in.Kind == typeTrigger {
					if err := t.ds.validateTriggerRow(candidate, in.ID, in.Properties, true); err != nil {
						return fmt.Errorf("delivery wiring %s: %w", in.ID, err)
					}
				}
				if _, err := t.put(in); err != nil {
					return fmt.Errorf("delivery wiring %s: %w", in.ID, err)
				}
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	out := make([]*substrate.Record, 0, len(docs))
	for _, d := range docs {
		if e, ok := written[docKey(d)]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// applyVocabularyBatch is the write path for schema: candidate before the
// transaction, rows + changelog inside it, pointer publish after commit —
// all under the per-repository schema-write mutex.
func (ds *dataset) applyVocabularyBatch(ctx context.Context, actor substrate.Actor, b vocabularyBatch) (map[string]*substrate.Record, error) {
	ds.vocabularyWriteMu.Lock()
	defer ds.vocabularyWriteMu.Unlock()

	current := ds.registry()
	st, err := ds.stageVocabularyBatch(ctx, current, &actor, b)
	if err != nil {
		return nil, err
	}
	candidate, touched := st.candidate, st.touched

	// Bodies prepare BEFORE the transaction — and therefore before
	// activation: every function the batch adds or changes must compile (Go)
	// or register (python) NOW, and the first failure fails the whole batch
	// as an admission error. Registration never accepts source that cannot
	// run; the lazy restart path stays what it is — recovery, not
	// validation.
	var prepare []*vocabulary.Function
	for _, aname := range sortedKeys(touched) {
		cand, _ := candidate.PackageByName(aname)
		if cand == nil {
			continue
		}
		cur, _ := current.PackageByName(aname)
		// A re-install after an uninstall is a FRESH install: uninstall tears
		// the owned package down whole (bundles.go), so `cur` is nil here and
		// every body prepares — the retired runner registrations warm again.
		for _, fname := range cand.FunctionOrder {
			f := cand.Functions[fname]
			if cur != nil {
				// "Already prepared" has to mean the same thing the RUNNER
				// means by it, which is Spec.Key(): repository, function and a
				// content hash over the body, its bundle's shared modules and
				// the capability envelope the sandbox applies at process
				// start. Comparing runtime and source alone was a narrower
				// identity than the one being warmed, so anything else that
				// re-keys an installation skipped preparation and reached
				// activation cold — a module-only bundle edit most obviously,
				// but a `permissions.network` withdrawal on unchanged source
				// too, which Spec.contentHash exists to catch.
				//
				// Each spec is built against ITS OWN registry: the stored
				// modules come from `current`, the shipped ones from
				// `candidate`, so a bundle that changed only its library is a
				// different key on the two sides and the body prepares.
				if prev := cur.Functions[fname]; prev != nil &&
					ds.runnerSpecIn(prev, current).Key() == ds.runnerSpecIn(f, candidate).Key() {
					continue // unchanged installation, already prepared
				}
			}
			prepare = append(prepare, f)
		}
	}
	if err := ds.prepareFunctions(ctx, candidate, prepare); err != nil {
		return nil, err
	}

	// The transaction: rows + changelog together, all or none.
	written := map[string]*substrate.Record{}
	err = ds.inTx(ctx, actor, true, func(t *txn) error {
		// The registry-dependency barrier's EXCLUSIVE side (wave-3 review
		// #11): trigger admission holds the shared side from callable
		// validation to commit, so the dropped-reference query below can
		// never race a trigger transaction that validated against the old
		// registry and is still uncommitted — that trigger commits first and
		// is seen, or it waits and revalidates against the committed rows.
		if err := t.lockKey(registryDepKey(ds)); err != nil {
			return err
		}
		// A whole-package teardown (bundle uninstall) removes the owned package's
		// delivery wiring first — under the same lock, before the guards below —
		// so dropping every callable never strands its own triggers. If a guard
		// then refuses (live data instances), this rolls back with the batch.
		if b.beforeGuards != nil {
			if err := b.beforeGuards(t); err != nil {
				return err
			}
		}
		// Refuse-breakage, all problems at once: a dropped type with live
		// rows, a narrowing definition diff stranding live rows, and (bundle
		// authorities) a dropped callable live triggers name.
		guards, err := droppedTypeGuards(t, st.droppedTypes)
		if err != nil {
			return err
		}
		narrowed, err := narrowingGuards(t, st.narrowings)
		if err != nil {
			return err
		}
		guards = append(guards, narrowed...)
		more, err := droppedCallableGuards(t, st.droppedCallables)
		if err != nil {
			return err
		}
		guards = append(guards, more...)
		if len(guards) > 0 {
			return fmt.Errorf("%w: %s", substrate.ErrGuard, strings.Join(guards, "; "))
		}
		if err := t.checkSchemaCAS(b.meta); err != nil {
			return err
		}
		got, err := t.projectPackages(candidate, touched, projectOpts{meta: b.meta, prune: true})
		if err != nil {
			return err
		}
		for k, e := range got {
			written[k] = e
		}
		// The refs index is the reverse projection of stored reference values
		// against the DECLARATION (refs.go), so a declaration that adds, drops
		// or re-points one changes what the same stored properties project to.
		// Re-derived here, in the apply's own transaction, against the candidate
		// closure: an index left alone would answer for a declaration that is
		// gone. The narrowing guards above have already refused every change
		// that would strand a LIVE value, so what this reaches is the additive
		// case and the tombstones the counts deliberately do not see.
		if err := t.reprojectRefs(candidate, st.reprojected); err != nil {
			return err
		}
		if b.extra != nil {
			if err := b.extra(t, candidate); err != nil {
				return err
			}
		}
		// THE DROPPED-KIND GUARD AGAIN, with every write this transaction makes
		// behind it. The count above is the transaction's opening reading, and
		// both writes below it CREATE rows: `extra` puts a bundle's data
		// documents against the still-live pre-commit registry (a widget row for
		// a kind the same upgrade removes), and the projection writes declaration
		// rows of the meta-kinds (a declaration of a category the same batch
		// stops declaring). Either way the publish would leave a
		// changelog-backed live row whose kind the registry it publishes cannot
		// resolve, so the count that decides is the one taken LAST. It also
		// covers whatever write is added to this transaction next.
		final, err := droppedTypeGuards(t, st.droppedTypes)
		if err != nil {
			return err
		}
		if len(final) > 0 {
			return fmt.Errorf("%w: this apply wrote rows of a kind it removes: %s",
				substrate.ErrGuard, strings.Join(final, "; "))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Publish: commit happened, the pointer swap is the activation. A crash
	// between the two is healed by the rebuild-from-records at repository open.
	ds.mu.Lock()
	ds.reg = candidate
	ds.mu.Unlock()

	if err := ds.ensureIndices(ctx); err != nil {
		return nil, err
	}
	// Bodies prepared synchronously above; what remains after publish is the
	// opposite motion — retiring runner state (python registrations, Go
	// processes) that no live installation references anymore.
	ds.reconcileRunner(ctx)
	return written, nil
}

// vocabularyStage is one batch's admission work, done before any transaction:
// the compiled candidate registry and the refuse-breakage classification of
// the diff against the currently stored one. The apply door and the read-only
// upgrade preview (PlanBundleUpgrade) share it, so what the preview reports
// and what the install refuses can never disagree.
type vocabularyStage struct {
	candidate        *vocabulary.Registry
	touched          map[string]bool
	droppedTypes     []string
	droppedCallables []droppedCallable
	// reprojected names the kinds whose REFERENCE declarations moved, so the
	// refs index is re-derived for their records in the apply's transaction
	// (refs.go). It is the kinds the change can be SEEN through, not every
	// touched kind: a declaration whose reference sites are identical projects
	// the same rows it already holds.
	reprojected []string
	narrowings  []narrowing
}

// stageVocabularyBatch builds the batch's candidate registry and classifies
// what admitting it would break. actor is the writing hand, checked at the
// ownership chokepoint; nil means the caller is only ASKING (the upgrade
// preview), and a read needs no license to write: nothing here writes.
func (ds *dataset) stageVocabularyBatch(ctx context.Context, current *vocabulary.Registry, actor *substrate.Actor, b vocabularyBatch) (*vocabularyStage, error) {
	// A batch carrying a bundle document is a whole-package apply: the closure
	// the document lists IS the authority, so install and upgrade both REPLACE it
	// — one atomic re-apply, absent declarations pruned, breakage refused
	// below. (This is the connector-registration transaction, generalized.)
	for _, d := range b.docs {
		if d.Kind == vocabulary.DocBundle {
			b.replacePackages = append(b.replacePackages, d.DeclaredPackage())
		}
	}

	// The touched authorities: every package a document declares into, plus the
	// wholesale replacements.
	touched := map[string]bool{}
	var problems []string
	for _, d := range append(append([]vocabulary.Document(nil), b.docs...), b.deletes...) {
		g := d.DeclaredPackage()
		if g == "" {
			problems = append(problems, fmt.Sprintf("%s %s: data.authority and data.package are required", d.Kind, d.ID))
			continue
		}
		touched[g] = true
	}
	for _, g := range b.replacePackages {
		touched[g] = true
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	if len(touched) == 0 {
		return nil, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	if actor != nil {
		// THE OWNERSHIP CHECK, at the one chokepoint (seed.go): who may write a
		// kind DECLARATION into each touched package.
		for _, g := range sortedKeys(touched) {
			if err := authorizeDeclarationWrite(*actor, current, g); err != nil {
				return nil, err
			}
			if err := authorizeNewPackage(*actor, current, g); err != nil {
				return nil, err
			}
		}
		// An actor's id does not embed its authority (alone among the kinds), so a
		// document can CLAIM any authority. Check by the actor declaration's CURRENT
		// authority too: a shipped actor redeclared into an installed package would
		// otherwise overwrite the shipped row from outside its package.
		for _, d := range append(append([]vocabulary.Document(nil), b.docs...), b.deletes...) {
			if d.Kind != kindActorLocal {
				continue
			}
			g, ok := current.ActorPackage(d.ID)
			if !ok {
				continue
			}
			if err := authorizeDeclarationWrite(*actor, current, g); err != nil {
				return nil, fmt.Errorf("actor %s belongs to %s: %w", d.ID, g, err)
			}
		}
	}

	replaced := map[string]bool{}
	for _, g := range b.replacePackages {
		replaced[g] = true
	}

	// Current documents of the touched packages, from their record rows.
	existing, err := ds.vocabularyDocumentRows(ctx, touched)
	if err != nil {
		return nil, err
	}
	resolveDeclarationVersions(&b, existing)
	merged := map[string]vocabulary.Document{}
	for k, d := range existing {
		if replaced[d.DeclaredPackage()] {
			continue // a replacement starts from the incoming set alone
		}
		merged[k] = d
	}
	for _, d := range b.docs {
		merged[docKey(d)] = d
	}
	for _, d := range b.deletes {
		delete(merged, docKey(d))
	}

	// The candidate: current registry minus the touched packages, plus their
	// rebuilt replacements — compiled whole (CEL guards, templates) before the
	// transaction opens. Every problem in every document reports at once.
	candidate := current.Clone()
	for g := range touched {
		candidate.Remove(g)
	}
	byPackage := map[string][]vocabulary.Document{}
	for _, d := range merged {
		g := d.DeclaredPackage()
		byPackage[g] = append(byPackage[g], d)
	}
	var rebuilt []*vocabulary.Package
	for _, aname := range sortedKeys(byPackage) {
		// An authority is rebuilt with the ORIGIN ITS STORED ROWS CLAIM, exactly as
		// storedPackages builds it at open. Two things read the origin — the row
		// the projection writes back, and the one loader rule keyed on it
		// (`runtime: host`, which only a shipped declaration may name) — so
		// rebuilding shipped vocabulary as `installed` would both re-stamp its
		// authority row and refuse core's own host functions the moment any batch
		// touched core. A package nobody has yet is the writer's, hence
		// installed.
		//
		// A PROVIDER install is the one thing that moves the origin: the batch
		// says `published` and every package it touches lands there, which is
		// how a repository holding the provider as `installed` is promoted by
		// its next install. `builtin` still wins, so a provider closure naming
		// core cannot demote the seed.
		source := vocabulary.SourceInstalled
		if b.published {
			source = vocabulary.SourcePublished
		}
		if cur, ok := current.PackageByName(aname); ok {
			switch cur.Source {
			case vocabulary.SourceBuiltin:
				source = vocabulary.SourceBuiltin
			case vocabulary.SourcePublished:
				source = vocabulary.SourcePublished
			}
		}
		gs, err := vocabulary.BuildPackages(byPackage[aname], source)
		if err != nil {
			var ve *substrate.ValidationError
			if errors.As(err, &ve) {
				problems = append(problems, ve.Problems...)
				continue
			}
			return nil, err
		}
		rebuilt = append(rebuilt, gs...)
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	if err := candidate.InstallAll(rebuilt); err != nil {
		return nil, err
	}
	// A declared default is stored by the write path, so it is refused here on
	// the terms the write path would refuse it on.
	if defaults := checkDeclaredDefaults(candidate, touched); len(defaults) > 0 {
		return nil, &substrate.ValidationError{Problems: defaults}
	}

	// Types the candidate no longer declares: refuse while instances exist,
	// counted transactionally below. Functions carry no delivery state of
	// their own any more — cursors belong to TRIGGER records, which outlive
	// a function's uninstall (the dispatcher skips a trigger whose callable
	// no longer resolves, loudly, without moving its cursor).
	var droppedTypes []string
	for aname := range touched {
		cur, _ := current.PackageByName(aname)
		cand, _ := candidate.PackageByName(aname)
		if cur != nil {
			for _, tn := range cur.KindOrder {
				if cand == nil || cand.Kinds[tn] == nil {
					droppedTypes = append(droppedTypes, cur.Kinds[tn].Identity)
				}
			}
		}
	}
	sort.Strings(droppedTypes)

	return &vocabularyStage{
		candidate: candidate,
		touched:   touched,
		// Refuse-with-instances, plus:
		// Bundle upgrades additionally refuse dropping a callable — function OR
		// agent — that live triggers still reference (bundles.go): the closure
		// re-apply is atomic and must not strand delivery. Outside bundle authorities
		// the looser contract stands — the dispatcher skips an unresolvable
		// callable loudly.
		droppedTypes:     droppedTypes,
		droppedCallables: droppedBundleCallables(current, candidate, touched),
		reprojected:      reprojectedKinds(current, candidate, touched),
		// Evolution-with-data: a NARROWING definition
		// diff — property dropped/renamed/kind-changed, enum value or state
		// removed, required added — is classified here against the currently
		// stored definitions and refused while live rows would be stranded,
		// with the count. Additive changes pass through untouched (schemadiff.go).
		narrowings: classifyNarrowings(current, candidate, touched),
	}, nil
}

// resolveDeclarationVersions is where THE API MAINTAINS THE VERSION: nobody
// bumps by hand (issue #48). An incoming declaration's version is honored
// when it moves PAST the stored one — the shipped tree and bundle closures
// pin versions, and those upgrades ride through untouched. Everything else
// (absent, an echo of the stored value, or lower) resolves here: a changed
// definition lands at stored+1, an unchanged one keeps the stored version,
// and a new declaration rides its authority's (the loader's cascade).
//
// A declaration that cannot carry a version of its own — a trait, a
// function, everything but a kind — moves its AUTHORITY forward instead when
// it changes, since a package's version is a statement about the closure
// it ships; a delete moves it too, so the prune reads as an upgrade.
//
// Documents are stamped COPY-ON-WRITE: a bundle's closure documents are the
// catalog's cached maps, shared across repositories, and the resolution must
// not write into them.
func resolveDeclarationVersions(b *vocabularyBatch, existing map[string]vocabulary.Document) {
	storedVersionOf := func(kind, id string) int64 {
		v, _ := vocabulary.VersionValue(existing[kind+"\x00"+id].Data["version"])
		return v
	}

	// Which authorities must move: a changed or deleted declaration with no
	// version of its own to move. (A NEW declaration needs none — it lands
	// whatever its version says.)
	needsBump := map[string]bool{}
	for _, d := range b.docs {
		if d.Kind == vocabulary.DocKind || d.Kind == vocabulary.DocPackage || d.Kind == vocabulary.DocAuthority {
			continue // each carries a version of its own to move
		}
		if stored, has := existing[docKey(d)]; has && !declarationDataEqual(d.Data, stored.Data) {
			needsBump[d.DeclaredPackage()] = true
		}
	}
	for _, d := range b.deletes {
		needsBump[d.DeclaredPackage()] = true
	}

	// The authorities first: they are the kinds' cascade fallback below.
	authorityVersion := map[string]int64{}
	seenAuthority := map[string]bool{}
	for i, d := range b.docs {
		if d.Kind != vocabulary.DocPackage {
			continue
		}
		seenAuthority[d.ID] = true
		stored := storedVersionOf(vocabulary.DocPackage, d.ID)
		explicit, _ := vocabulary.VersionValue(d.Data["version"])
		v := explicit
		switch {
		case explicit > stored:
			// An explicit move forward is honored as written.
		case needsBump[d.ID]:
			v = stored + 1
		default:
			v = stored
		}
		if v == 0 {
			// A brand-new package declaring nothing: the loader defaults it.
			authorityVersion[d.ID] = vocabulary.DefaultVersion
			continue
		}
		if v != explicit {
			b.docs[i] = docWithVersion(d, v)
		}
		authorityVersion[d.ID] = v
	}
	// A package that must move but is not in the batch: bring it in, at
	// stored+1, so the bump lands with the change that demands it. (A fresh
	// authority has nothing stored to move past; its own document is
	// mandatory in that batch and was handled above.)
	for _, g := range sortedKeys(needsBump) {
		if seenAuthority[g] {
			continue
		}
		stored := storedVersionOf(vocabulary.DocPackage, g)
		if stored == 0 {
			continue
		}
		authority, name := vocabulary.SplitPackageRef(g)
		b.docs = append(b.docs, vocabulary.Document{
			Kind: vocabulary.DocPackage, ID: g,
			Data: map[string]any{"authority": authority, "package": name, "version": stored + 1},
		})
		authorityVersion[g] = stored + 1
	}

	// The kinds: each against its own stored row, with the package's
	// version as the effective fallback exactly as the loader cascades it.
	cascade := func(g string) int64 {
		if v, ok := authorityVersion[g]; ok {
			return v
		}
		if v := storedVersionOf(vocabulary.DocPackage, g); v != 0 {
			return v
		}
		return vocabulary.DefaultVersion
	}
	for i, d := range b.docs {
		if d.Kind != vocabulary.DocKind && d.Kind != vocabulary.DocAuthority {
			continue
		}
		stored, has := existing[docKey(d)]
		if !has {
			continue // a new kind rides the cascade
		}
		storedV, _ := vocabulary.VersionValue(stored.Data["version"])
		explicit, _ := vocabulary.VersionValue(d.Data["version"])
		if explicit > storedV {
			continue // an explicit move forward is honored as written
		}
		effective := explicit
		if effective == 0 {
			effective = cascade(d.DeclaredPackage())
		}
		if effective > storedV {
			continue // the package's own move carries it (the boot/bundle ride)
		}
		v := storedV
		if !declarationDataEqual(d.Data, stored.Data) {
			v = storedV + 1
		}
		if v != explicit && v > 0 {
			b.docs[i] = docWithVersion(d, v)
		}
	}
}

// declarationDataEqual compares two declaration data maps minus the version
// key — the version is the statement ABOUT the change, not part of it.
// json.Marshal sorts map keys and renders an integral float and an int the
// same, so a YAML-decoded document and a jsonb read-back compare honestly.
// Anything unmarshalable counts as changed; the loader will say why.
func declarationDataEqual(a, b map[string]any) bool {
	ja, errA := json.Marshal(minusVersionKey(a))
	jb, errB := json.Marshal(minusVersionKey(b))
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ja, jb)
}

func minusVersionKey(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		if k == propDeclarationVersion {
			continue
		}
		out[k] = v
	}
	return out
}

// docWithVersion is the document with its data.version set, on a COPY of the
// data map — the original may be a catalog-cached closure document. The hint
// is the map's half alone: length arithmetic in an allocation size is what
// CodeQL flags as overflowable, and the one extra key grows it organically
// (same ruling as declarationReplacement).
func docWithVersion(d vocabulary.Document, v int64) vocabulary.Document {
	data := make(map[string]any, len(d.Data))
	for k, val := range d.Data {
		data[k] = val
	}
	data[propDeclarationVersion] = v
	d.Data = data
	return d
}

// checkSchemaCAS verifies each document's ifVersion against the stored row,
// inside the batch transaction (exact under the schema-write mutex: nothing
// else writes these rows).
func (t *txn) checkSchemaCAS(meta map[string]vocabularyDocMeta) error {
	for key, m := range meta {
		if m.ifVersion == nil {
			continue
		}
		typ, id, _ := strings.Cut(key, "\x00")
		ident, ok := schemaKindRef(typ)
		if !ok {
			ident = typ
		}
		row, err := t.loadRow(eref{Kind: ident, ID: id}, true)
		if err != nil {
			return err
		}
		if err := checkCAS(row, m.ifVersion); err != nil {
			return err
		}
	}
	return nil
}

// --- projection: registry facts as record rows -------------------------------

// projectOpts tunes one projection pass.
type projectOpts struct {
	// meta carries the labels/annotations a batch's documents travel with.
	meta map[string]vocabularyDocMeta
	// skip passes a declaration by, keyed as typeIdent+"\x00"+id. The
	// boot-time upgrade uses it for the never-downgrade rule (seed.go); nil
	// projects everything.
	skip func(key string) bool
	// prune tombstones the touched packages' declarations the registry no
	// longer declares — what a whole-package re-apply (install, upgrade,
	// uninstall) needs. THE SEED AND THE SHIPPED-VOCABULARY UPGRADE NEVER
	// PRUNE: the embedded tree is a source, the repository's own
	// changelog is the truth, and re-assert-and-prune is dead.
	prune bool
}

// projectPackages writes the touched packages' declarations as record rows
// from the candidate registry — no-op suppressed, so unchanged declarations
// stay changelog-silent — and, when asked, tombstones rows of those authorities the
// candidate stopped declaring. It returns every written (or unchanged) record
// keyed by kind+id.
func (t *txn) projectPackages(reg *vocabulary.Registry, authorities map[string]bool, opts projectOpts) (map[string]*substrate.Record, error) {
	out := map[string]*substrate.Record{}
	live := map[string]bool{}

	var names []string
	for g := range authorities {
		names = append(names, g)
	}
	sort.Strings(names)
	// Enumerate before writing: which kinds' OWN declarations this pass writes
	// decides what every row of the pass is validated against, so the set has to
	// be complete before the first row lands (projectionKind).
	var passes [][]declaration
	projecting := map[string]bool{}
	for _, aname := range names {
		g, ok := reg.PackageByName(aname)
		if !ok {
			continue // the authority is being removed whole; prune takes its rows
		}
		decls, err := packageDeclarations(g)
		if err != nil {
			return nil, err
		}
		passes = append(passes, decls)
		for _, d := range decls {
			if d.typ != kindKind || (opts.skip != nil && opts.skip(d.key())) {
				continue
			}
			projecting[d.id] = true
		}
	}
	// The projection's rows are validated against `reg` (projectionKind), so the
	// fold's search bands come from it too: a row indexed under a declaration
	// other than the one it was admitted against would be re-indexed differently
	// by its own replay. Restored before the batch's `extra` hook, whose data
	// documents are ordinary writes against the live registry.
	prevReg := t.writeReg
	t.writeReg = reg
	defer func() { t.writeReg = prevReg }()
	for _, decls := range passes {
		if err := t.projectPackage(reg, projecting, decls, live, opts, out); err != nil {
			return nil, err
		}
	}
	if !opts.prune {
		return out, nil
	}
	if err := t.pruneSchemaRows(authorities, live); err != nil {
		return nil, err
	}
	return out, nil
}

// declaration is one projected schema row: the manifest's short kind name,
// the record type it stores as, its id, and the properties the row carries —
// `version` among them, on every one of them.
type declaration struct {
	short string
	typ   string
	id    string
	props map[string]any
}

// key is the declaration's identity as the projection and the version diff
// both key it: the record type plus the id.
func (d declaration) key() string { return d.typ + "\x00" + d.id }

// version reads the declaration's own version off its projected properties.
// Zero is the absent version, ordering below everything.
func (d declaration) version() int64 {
	v, _ := vocabulary.VersionValue(d.props["version"])
	return v
}

// projectPackage writes one authority's declarations: the header, its actors, and
// every property type, trait, record type, mapping, function, agent and
// bundle. `projecting` is the whole pass's set of kinds whose own declaration
// this projection writes, which is what projectionKind reads.
func (t *txn) projectPackage(reg *vocabulary.Registry, projecting map[string]bool, decls []declaration, live map[string]bool, opts projectOpts, out map[string]*substrate.Record) error {
	for _, d := range decls {
		if opts.skip != nil && opts.skip(d.key()) {
			continue
		}
		props, err := t.declarationReplacement(d)
		if err != nil {
			return err
		}
		in := substrate.PutInput{Kind: d.typ, ID: d.id, Properties: props}
		if m, ok := opts.meta[d.short+"\x00"+d.id]; ok {
			in.Labels = m.labels
			in.Annotations = m.annotations
		}
		ty, err := t.projectionKind(reg, projecting, d.typ)
		if err != nil {
			return fmt.Errorf("substrate/engine: project %s %s: %w", d.short, d.id, err)
		}
		e, err := t.putKind(ty, in)
		if err != nil {
			return fmt.Errorf("substrate/engine: project %s %s: %w", d.short, d.id, err)
		}
		live[d.key()] = true
		out[d.short+"\x00"+d.id] = e
	}
	return nil
}

// declarationReplacement is the declaration's properties plus an explicit null
// for every AUTHORED property the stored row still holds and the declaration no
// longer declares.
//
// A put MERGES, and a declaration is not a merge: the row's properties ARE the
// declaration, so a key an author deleted has to leave the row or the rebuild
// would keep handing it back and the change would never land. (While the content
// rode in one `definition` blob, replacing the blob did this for free.) What is
// deliberately NOT nulled is the engine's own — a disabled bundle stays
// disabled, a quarantine mark survives a re-projection — since those are the
// server's properties and not the declaration's.
func (t *txn) declarationReplacement(d declaration) (map[string]any, error) {
	row, err := t.loadRow(eref{Kind: d.typ, ID: d.id}, false)
	if err != nil || row == nil {
		return d.props, err
	}
	// Sized to the declaration's half alone: the sum of two lengths is what
	// CodeQL flags as an overflowable allocation size, and a map hint is not
	// worth the finding — the row's keys grow the map organically below.
	props := make(map[string]any, len(d.props))
	for k, v := range d.props {
		props[k] = v
	}
	for k := range row.Props {
		if _, written := props[k]; written || engineOwned(d.short, k) {
			continue
		}
		props[k] = nil
	}
	return props, nil
}

// projectionKind resolves the kind a projected declaration row stores as,
// against the declaration that will be LIVE once this projection commits.
//
// Which of a declaration row's properties are DECLARED is decided by the
// meta-kind's own declaration: core's `kind` for a kind row, `trait` for a trait
// row, and so on down the list. Two cases, and they need opposite registries.
//
//   - This pass writes that meta-kind's declaration too. Then the candidate's
//     declaration is the one the repository ends up holding, and the row has to
//     be held to it: a property added to core's `kind` rides on every row the
//     same pass projects, and validating those rows against the STORED (older)
//     declaration refuses them, taking the boot upgrade, and with it the
//     repository's open, down. The candidate does not publish until commit, so
//     writing the new declaration row earlier in the same transaction cannot
//     help. This is what carries the typed flip itself into a repository whose
//     stored meta-kinds still declare a `definition` blob.
//   - This pass leaves that declaration alone: the boot upgrade's
//     never-downgrade skip (a stored declaration at or ahead of the binary's),
//     or an authority the batch does not touch. Then the STORED declaration
//     survives the commit and the live registry is what decides, so a row the
//     candidate would admit and the surviving declaration rejects must not
//     land.
func (t *txn) projectionKind(reg *vocabulary.Registry, projecting map[string]bool, typeIdent string) (*vocabulary.Kind, error) {
	if projecting[typeIdent] {
		return resolveKindIn(reg, typeIdent)
	}
	return t.ds.resolveType(typeIdent)
}

// packageDeclarations renders every declaration an authority stores as rows, in one
// stable order. It is the ONE enumeration of an authority's contents: the
// projection writes it, and the boot-time upgrade diffs versions across it
// (seed.go), so the writer and the differ can never disagree about what a
// authority holds.
//
// A ROW IS THE DECLARATION'S OWN DATA MAP plus what the engine stamps. There is
// no `definition` blob and no projected mirror: the loader admits ONE spelling
// per key and refuses every retired one by name (vocabulary/load.go's
// tombstones), so the properties a row carries are the keys the author wrote, and
// the read-back (rowDocument) is that same map with the stamped keys dropped.
// Reading what an older binary stored is not this projection's job: a row in
// dialect 1's shape is refused at the gate (dialect.go), so the loader is handed
// the admitted spelling like anybody else. What the engine stamps is declared `managed: true`
// on the core kinds, which is the same list spelled where a client can read it.
//
// EVERY DECLARATION CARRIES A VERSION. A record type's own
// `version:` wins where it declares one; every other kind takes the declaring
// authority's, which is the cheapest consistent rule — an authority's version is a
// statement about the declarations it ships.
func packageDeclarations(g *vocabulary.Package) ([]declaration, error) {
	var decls []declaration
	var missing []string
	// add takes the declaration's own data map and the properties the engine
	// stamps over it. The retired keys of the kind — the `definition` blob an
	// older binary stored, the id-derived `name`, the agent mirrors, a
	// never-stored `sourceYAML` — travel as explicit nulls: a projection's put
	// MERGES, so without them a migrated row would keep the blob it was
	// translated out of. A null against an absent property is a no-op, so a
	// repository that never held them stays changelog-silent.
	add := func(short, typeIdent, id string, data, stamped map[string]any) error {
		props, err := jsonSafe(data)
		if err != nil {
			return err
		}
		for k, v := range stamped {
			props[k] = v
		}
		for k := range retiredDeclarationProps(short) {
			if _, live := props[k]; !live {
				props[k] = nil
			}
		}
		if v, _ := vocabulary.VersionValue(props[propDeclarationVersion]); v < 1 {
			props[propDeclarationVersion] = g.Version
		}
		if v, _ := vocabulary.VersionValue(props[propDeclarationVersion]); v < 1 {
			// The version is MANDATORY: it is what a boot-time upgrade diffs
			// against, so a declaration without one could never converge.
			missing = append(missing, short+" "+id)
		}
		decls = append(decls, declaration{short: short, typ: typeIdent, id: id, props: props})
		return nil
	}

	actors := make([]any, 0, len(g.Actors))
	for _, a := range g.Actors {
		actors = append(actors, a)
	}
	// An AUTHORITY row owns packages and declares nothing else, so its
	// projection is the row and it is done.
	if g.IsAuthority() {
		data := map[string]any{"version": g.Version}
		if g.Description != "" {
			data["description"] = g.Description
		}
		if err := add(vocabulary.DocAuthority, kindAuthority, g.Identity, data,
			map[string]any{"source": g.Source}); err != nil {
			return nil, err
		}
		return decls, nil
	}
	// The explicit quarantined/quarantineReason nulls CLEAR any issue-010
	// marker: a re-projection of the package (a catalog re-install producing a
	// valid closure) is what lifts the quarantine. Null against an absent
	// property is a no-op, so a healthy package's re-projection writes nothing.
	header := map[string]any{
		"authority": g.Authority, "package": g.Name, "version": g.Version,
	}
	if g.Description != "" {
		header["description"] = g.Description
	}
	if err := add(vocabulary.DocPackage, kindPackage, g.Identity, header,
		map[string]any{
			"actors": actors, "source": g.Source,
			propPackageQuarantined: nil, propPackageQuarantineReason: nil,
		}); err != nil {
		return nil, err
	}
	for _, a := range g.Actors {
		// An actor named for its package has no row of its own: the package
		// row above already holds that id and lists the actor.
		if a == g.Identity {
			continue
		}
		// The tier is the actor's explicit attribute: projected
		// onto the row so the rebuild — and every reader — gets data, never
		// an inference from the actor's spelling.
		tier := string(substrate.TierMachine)
		if declared, ok := g.ActorTiers[a]; ok {
			tier = string(declared)
		}
		if err := add(kindActorLocal, kindActor, a,
			map[string]any{"authority": g.Authority, "package": g.Name, "tier": tier},
			map[string]any{"source": g.Source}); err != nil {
			return nil, err
		}
	}
	for _, n := range g.DatatypeOrder {
		d := g.PropertyTypes[n]
		if err := add(vocabulary.DocPropertyType, kindPropertyType, d.Identity(),
			d.Definition, map[string]any{"source": g.Source}); err != nil {
			return nil, err
		}
	}
	for _, n := range g.TraitOrder {
		c := g.Traits[n]
		if err := add(vocabulary.DocTrait, kindTrait, c.Identity(),
			c.Definition, map[string]any{"source": g.Source}); err != nil {
			return nil, err
		}
	}
	for _, n := range g.KindOrder {
		ty := g.Kinds[n]
		// The type's OWN version (the loader defaults it to the authority's and a
		// `version:` in the declaration overrides it) — the one kind whose data
		// may carry a per-declaration version.
		if err := add(vocabulary.DocKind, kindKind, ty.Identity, ty.Definition,
			map[string]any{"version": ty.Version, "source": ty.Source}); err != nil {
			return nil, err
		}
	}
	for _, n := range g.MappingOrder {
		m := g.Mappings[n]
		if err := add(vocabulary.DocRecordMapping, kindRecordMapping, m.Identity(),
			m.Definition, map[string]any{"source": g.Source}); err != nil {
			return nil, err
		}
	}
	for _, n := range g.FunctionOrder {
		fn := g.Functions[n]
		// NO `source` stamp: on a function that word is the BODY, an authored
		// property, and the declaration's origin is not projected here at all.
		if err := add(vocabulary.DocFunction, kindFunction, fn.Identity(),
			fn.Definition, nil); err != nil {
			return nil, err
		}
	}
	for _, n := range g.AgentOrder {
		a := g.Agents[n]
		if err := add(vocabulary.DocAgent, kindAgent, a.Identity(), a.Definition, nil); err != nil {
			return nil, err
		}
	}
	if b := g.Bundle; b != nil {
		// `disabled` and `purging` are deliberately untouched: an upgrade of a
		// disabled bundle stays disabled. The explicit `uninstalled` null clears
		// the retired reversible-uninstall marker off any legacy row a
		// pre-teardown binary wrote — uninstall is a whole-package teardown now
		// (bundles.go), so no live bundle row ever carries it, and the null is a
		// no-op otherwise.
		if err := add(vocabulary.DocBundle, kindBundle, b.Identity(),
			b.Definition, map[string]any{"uninstalled": nil}); err != nil {
			return nil, err
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: every declaration needs a version: %s",
			substrate.ErrValidation, strings.Join(missing, ", "))
	}
	return decls, nil
}

// engineDeclarationProps are the properties a declaration ROW carries that its
// DOCUMENT does not, split by who put them there.
//
// The ENGINE-STAMPED half is not listed anywhere in Go: a row reads back as a
// document by WHITELIST — the keys the loader admits for that kind
// (vocabulary.DeclarationDataKeys) and nothing else — so the stamped set needs
// no second spelling here, and a property some FUTURE binary stamps cannot reach
// this loader as an unknown key. Which properties the engine owns is declared
// where a client can read it (`managed: true`), and TestManagedPropertiesAreNot
// DocumentKeys holds the two statements together.
//
// The RETIRED half does need a list, because it is a list of DEAD spellings and
// nothing derives it: retiredDeclarationProps, below.

// propDeclarationBlob is dialect 1's one authored property: the whole
// declaration, as json. Spelled once so the refusals that name it are greppable.
const propDeclarationBlob = "definition"

// retiredDeclarationProps are dialect 1's own row properties: the `definition`
// blob and the mirror columns that pre-typed projection wrote beside it — the
// id-derived `name` and `plural`, an agent's function/sub-agent mirrors, a
// never-stored `sourceYAML`.
//
// It outlived the rung that read it (#217) because two LIVE paths still do:
// `engineOwned` excludes them from the properties a projection preserves, and
// the projection writes each as an explicit null so a merge-only put clears one
// off a row that still carries it. The list cannot grow: a spelling is retired
// once, and dialect 1 is closed.
func retiredDeclarationProps(short string) map[string]bool {
	out := map[string]bool{propDeclarationBlob: true, "sourceYAML": true, "name": true}
	switch short {
	case vocabulary.DocKind:
		out["plural"] = true
	case vocabulary.DocAgent:
		out["functions"], out["subagents"] = true, true
	}
	return out
}

// propDeclarationVersion is the version EVERY declaration row carries: the
// property a boot-time upgrade diffs on, stamped by the projection when the
// declaration pinned none (packageDeclarations) and therefore not part of the
// authored declaration a read surface renders (dataset.go authoredKindData).
const propDeclarationVersion = "version"

// engineOwned reports whether a stored property is the ENGINE's rather than the
// declaration's: not a document key, and not one of the retired spellings. It is
// what a projection leaves alone — a version ahead of its authority's, a
// quarantine mark, a disabled bundle.
func engineOwned(short, prop string) bool {
	return !vocabulary.DeclarationDataKeys(short)[prop] && !retiredDeclarationProps(short)[prop] &&
		!columnBackedProp[prop]
}

// pruneSchemaRows tombstones schema record rows of the touched packages the
// candidate registry stopped declaring. Without it a removed declaration
// would be listed forever, since projection only ever writes rows.
//
// The scope is the TOUCHED authorities and nothing else. The v0 boot widened it to
// every row claiming `source: builtin` — the re-assert-and-prune the seed rule
// killed: a declaration the shipped tree stopped declaring now
// stays in the repositories that already hold it, because their changelog is the
// truth and nothing rewrites history behind them.
func (t *txn) pruneSchemaRows(authorities, live map[string]bool) error {
	args := make([]any, 0, len(vocabularyKindRefs))
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	rows, err := t.query(`
		SELECT id, kind, props->>'authority', props->>'package' FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL`, args...)
	if err != nil {
		return err
	}
	type srow struct{ id, typ, pkg string }
	var all []srow
	for rows.Next() {
		var id, typ string
		var authority, pkg *string
		if err := rows.Scan(&id, &typ, &authority, &pkg); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, srow{id, typ, rowPackage(typ, id, authority, pkg)})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	type stale struct{ id, typ string }
	var gone []stale
	for _, s := range all {
		if !authorities[s.pkg] || live[s.typ+"\x00"+s.id] {
			continue
		}
		gone = append(gone, stale{s.id, s.typ})
	}
	for _, s := range gone {
		ref := eref{Kind: s.typ, ID: s.id}
		if s.typ == kindBundle {
			// The bundle row's `bindings` are its input choices (inputs.go). A
			// tombstone leaves the property standing, so without this a later
			// re-install of the same bundle would resurrect a binding the
			// uninstall was supposed to clear — fresh install, stale explicit
			// choice. It is cleared by WRITING the row, in this transaction, so
			// the delta rides the changelog and a rebuild replays it.
			row, err := t.loadRow(ref, true)
			if err != nil {
				return err
			}
			if len(bindingsOf(row)) > 0 {
				if err := t.writeBindings(ref, map[string]any{}); err != nil {
					return fmt.Errorf("substrate/engine: clear bundle bindings: %w", err)
				}
			}
		}
		if _, err := t.softDelete(ref); err != nil {
			return fmt.Errorf("substrate/engine: prune schema row %s: %w", s.id, err)
		}
	}
	return nil
}

// rowPackage is the GROUP a stored declaration row belongs to, and the one
// spelling of that rule on the SQL side (vocabulary.Document.DeclaredPackage is
// the same rule on the document side): a package row and an authority row are
// their own group, everything else names its authority and its package.
func rowPackage(typeIdent, id string, authority, pkg *string) string {
	if typeIdent == kindPackage || typeIdent == kindAuthority {
		return id
	}
	if authority == nil || pkg == nil || *authority == "" || *pkg == "" {
		return ""
	}
	return vocabulary.PackageRef(*authority, *pkg)
}

// --- rows back into documents -------------------------------------------------

// vocabularyDocumentRows reads the touched packages' schema record rows back as
// loader documents — the store is the source the candidate rebuilds from.
func (ds *dataset) vocabularyDocumentRows(ctx context.Context, authorities map[string]bool) (map[string]vocabulary.Document, error) {
	args := make([]any, 0, len(vocabularyKindRefs))
	ph := make([]string, 0, len(vocabularyKindRefs))
	for i, ident := range vocabularyKindRefs {
		args = append(args, ident)
		ph = append(ph, "$"+strconv.Itoa(i+1))
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, kind, props FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL
		ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]vocabulary.Document{}
	authorityActors := map[string][]string{} // authority id → declared actors
	for rows.Next() {
		var id, typ string
		var raw []byte
		if err := rows.Scan(&id, &typ, &raw); err != nil {
			return nil, err
		}
		var props map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &props); err != nil {
				return nil, fmt.Errorf("substrate/engine: decode schema row %s: %w", id, err)
			}
		}
		d, ok, derr := rowDocument(id, typ, props)
		if derr != nil {
			return nil, derr
		}
		if !ok || !authorities[d.DeclaredPackage()] {
			continue
		}
		if typ == kindPackage {
			authorityActors[id] = append(authorityActors[id], anyStrings(props["actors"])...)
		}
		out[docKey(d)] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Record 60: a package-named actor has no row of its own — the package row
	// is it. Synthesize its document so the rebuilt package keeps the actor.
	for aname, actors := range authorityActors {
		for _, a := range actors {
			if a != vocabulary.PackageActor(aname) {
				continue
			}
			d, err := vocabulary.DocumentFromMap(vocabulary.ActorManifest(aname, a))
			if err != nil {
				return nil, err
			}
			if _, exists := out[docKey(d)]; !exists {
				out[docKey(d)] = d
			}
		}
	}
	return out, nil
}

// rowDocument rebuilds the loader document a schema record row stores. An
// unknown kind is an ERROR, never a silent skip: a
// schema row this binary cannot rebuild means the store speaks a newer
// dialect, and dropping it would rebuild authorities missing declarations — the
// dialect gate at repository open is the front door, this is the bolt on the back.
// The boolean skip survives only for rows that are legitimately not documents
// (a pre-promotion actor mirror row the seed rewrites, a row too old to
// rebuild that deleteVocabularyRecord addresses by name alone).
func rowDocument(id, typeIdent string, props map[string]any) (vocabulary.Document, bool, error) {
	short, ok := vocabularyRecordKinds[typeIdent]
	if !ok {
		return vocabulary.Document{}, false, fmt.Errorf("%w: schema row %s %s has a kind this binary does not know",
			ErrVocabularyDialectNewer, typeIdent, id)
	}
	d := vocabulary.Document{Kind: short, ID: id}
	// THE BLOB IS DECIDED FIRST, BY PRESENCE, for every schema kind and whatever
	// the value is. A null, a string or a list under that key is not a declaration
	// this binary can read either, and reading the row's typed properties around it
	// would rebuild an authority from half a declaration — so presence alone is the
	// question. Nothing translates it: the gate at open refuses a store carrying
	// one (dialect.go) and this is the same refusal at the row.
	if _, held := props[propDeclarationBlob]; held {
		return vocabulary.Document{}, false, fmt.Errorf(
			"%w: schema row %s %s carries a `%s` property — dialect 2 stores a declaration's own properties, so this row is dialect 1 or corruption, and no rung translates either",
			ErrDeclarationUntranslated, typeIdent, id, propDeclarationBlob)
	}
	switch short {
	case kindActorLocal:
		authority, _ := props["authority"].(string)
		pkg, _ := props["package"].(string)
		if authority == "" || pkg == "" {
			return vocabulary.Document{}, false, nil // a pre-promotion mirror row; the seed rewrites it
		}
		d.Data = map[string]any{"authority": authority, "package": pkg}
		// machine is the parse default, so the rebuilt document omits it —
		// the boot no-op comparison stays parsed projection against parsed
		// projection.
		if tier, _ := props["tier"].(string); tier != "" && tier != string(substrate.TierMachine) {
			d.Data["tier"] = tier
		}
	default:
		// A DELETED KEY IS NAMED, NEVER DROPPED. The read below is a whitelist, so
		// a row carrying a spelling the loader retired would otherwise lose it in
		// silence — a function whose `emit` vanished keeps running and writes
		// nothing, which is the worst answer available. Only an UNRELEASED binary
		// wrote such a row, and nothing migrates one, so the refusal is the whole
		// handling: it names the replacement, and the store it comes from is a
		// development one to wipe.
		for _, name := range sortedKeys(props) {
			replacement, gone := vocabulary.DeletedDeclarationKeys(short)[name]
			if !gone {
				continue
			}
			return vocabulary.Document{}, false, fmt.Errorf(
				"%w: schema row %s %s carries the deleted `%s` property, replaced by %s",
				ErrDeclarationUntranslated, typeIdent, id, name, replacement)
		}
		// THE TYPED ROW: its properties ARE the declaration, so the document's
		// data is the property map with the engine's own keys dropped
		// (serverDeclarationProps). Nothing is derived and nothing is renamed —
		// what the author wrote is what comes back.
		d.Data = declarationData(short, props)
	}
	// No sourceYAML read-back: rows store the parsed declaration only (record
	// 61), so the rebuilt document's source is agreed absence — the boot no-op
	// comparison is parsed projection against parsed projection.
	return d, true, nil
}

// declarationData is one declaration row's DOCUMENT data: the properties the
// loader admits for that kind, and nothing else. It is the read half of the
// projection's write (packageDeclarations), and it is a WHITELIST rather than
// a list of exclusions on purpose — see engineDeclarationProps.
func declarationData(short string, props map[string]any) map[string]any {
	keys := vocabulary.DeclarationDataKeys(short)
	data := make(map[string]any, len(props))
	for k, v := range props {
		if !keys[k] || v == nil {
			continue
		}
		data[k] = v
	}
	return data
}

// columnBackedProp are the properties every record carries in a column of its
// own and no declaration ever declares. They read back in `properties` like any
// other (the wire shows them there), so a client echoing a record it just read
// would otherwise send `title` into a declaration's data and be told the loader
// does not know the key.
var columnBackedProp = map[string]bool{
	substrate.PropTitle: true, substrate.PropBody: true,
	substrate.PropAt: true, substrate.PropEndsAt: true, substrate.PropDueAt: true,
}

// anyStrings reads a jsonb string list property.
func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprint(it))
	}
	return out
}

// --- boot: rebuild and migrate -------------------------------------------------
//
// The seed is NOT here and does not run at open: the embedded tree is written
// into a repository's changelog ONCE, at creation, and afterwards only as explicit
// upgrade entries (seed.go). What open does is read the repository's own rows
// back — they are the whole of its vocabulary.

// The package-level quarantine markers, on the package row. A stored closure
// that no longer admits under the current binary is marked here and left OUT of
// the live registry, so its one bundle is disabled instead of the whole
// repository bricking; a re-install (which projects a valid closure, clearing
// the marker) is how it clears.
const (
	propPackageQuarantined      = "quarantined"
	propPackageQuarantineReason = "quarantineReason"
	// quarantineReasonMax bounds the stored admission-error text so a huge
	// problem list can never bloat the package row.
	quarantineReasonMax = 2000
)

// quarantinedPackage pairs a stored authority that could not join the live
// registry with the reason. It carries the NAME, not a built authority: a
// closure that fails to PARSE never produces one, and the name is all the
// marker needs.
type quarantinedPackage struct {
	name   string
	reason string
}

// cappedQuarantineReason holds the stored reason to quarantineReasonMax.
func cappedQuarantineReason(reason string) string {
	if len(reason) > quarantineReasonMax {
		return reason[:quarantineReasonMax]
	}
	return reason
}

// loadStoredVocabulary rebuilds THE WHOLE VOCABULARY from the repository's own
// schema record rows and installs it into this dataset's registry. It is the
// only way a live registry is ever built: the
// embedded tree seeded the rows at creation and upgrades append to them, so
// after that the repository's own changelog — folded into these rows — is the ONLY
// source of its kinds. Shipped authorities load exactly like installed ones,
// each carrying the `source` its rows record.
//
// Issue 010: a stored closure that no longer admits under this binary (a
// tightened trait/schema contract on an already-installed bundle) must not
// brick the whole repository. When the whole set fails to admit together, the
// admissible subset opens and every closure that still fails is QUARANTINED —
// logged, marked on its authority row (a state the console/status shows),
// and left out of the live registry — so the repository opens with the rest. A
// re-install of the offending bundle clears the mark. The same holds one step
// earlier, for a closure that no longer PARSES (storedPackages): both
// failures arrive here as quarantine candidates and are marked identically.
func (ds *dataset) loadStoredVocabulary(ctx context.Context) error {
	built, unparsed, err := ds.storedPackages(ctx, nil)
	if err != nil {
		return err
	}
	if len(built) == 0 {
		// A repository is SEEDED at creation, so no vocabulary at all means
		// the store was made some other way — say so loudly rather than serve
		// a repository in which nothing resolves. Core's own parse failure is
		// a hard error upstream, so this is never a quarantine cascade.
		return fmt.Errorf("substrate/engine: repository %s holds no vocabulary — it was never seeded", ds.info.Name)
	}
	// Fast path: the whole stored set admits together. A binary that RELAXED
	// a contract also clears any stale quarantine markers here.
	good, quarantined := built, unparsed
	if err := ds.reg.InstallAll(built); err != nil {
		// Slow path: install the admissible subset and quarantine the rest. The
		// failed InstallAll removed everything it added, so ds.reg is clean
		// again.
		var inadmissible []quarantinedPackage
		good, inadmissible = ds.admissibleSubset(built)
		quarantined = append(quarantined, inadmissible...)
	}
	if err := ds.clearGroupQuarantine(ctx, good); err != nil {
		return err
	}
	for _, q := range quarantined {
		// Every value here came off a stored row, so each one passes the log
		// filters first: the two ids by the id grammar, the reason as repaired
		// prose (triggers.go). A crafted declaration cannot forge a log line.
		ds.svc.log.Error("substrate: quarantining a stored closure that no longer loads under this binary — the repository opens WITHOUT it; re-install the bundle to clear",
			"repository", logSafeID(ds.info.Name), "package", logSafeID(q.name), "reason", logSafeText(q.reason))
		if err := ds.markGroupQuarantined(ctx, q.name, q.reason); err != nil {
			return err
		}
	}
	return nil
}

// admissibleSubset installs the maximal subset of built packages that admits
// into ds.reg and returns the rest as quarantined, with each one's admission
// error. It is a fixpoint over single-package Install: a package that depends
// on a sibling admits once that sibling is in, and Install self-removes a
// package that fails, so ds.reg is left holding exactly the admissible subset.
// n (installed packages) is small, so the O(n^2) worst case is fine.
func (ds *dataset) admissibleSubset(built []*vocabulary.Package) (good []*vocabulary.Package, quarantined []quarantinedPackage) {
	remaining := append([]*vocabulary.Package(nil), built...)
	for {
		progressed := false
		var next []*vocabulary.Package
		for _, g := range remaining {
			if err := ds.reg.Install(g); err == nil {
				good = append(good, g)
				progressed = true
			} else {
				next = append(next, g)
			}
		}
		remaining = next
		if !progressed || len(remaining) == 0 {
			break
		}
	}
	// Whatever remains cannot admit even with every good authority present. Re-run
	// Install once per remaining authority to capture its reason — it fails and
	// self-removes, so ds.reg is untouched.
	for _, g := range remaining {
		reason := "stored closure failed admission under the current binary"
		if err := ds.reg.Install(g); err != nil {
			reason = err.Error()
		}
		quarantined = append(quarantined, quarantinedPackage{name: g.Identity, reason: cappedQuarantineReason(reason)})
	}
	return good, quarantined
}

// markGroupQuarantined records the quarantine state on the package's own row,
// so BundleStatuses can surface it and a later re-install (which re-projects
// the row, clearing these props) lifts it.
func (ds *dataset) markGroupQuarantined(ctx context.Context, group, reason string) error {
	_, err := ds.patchInternal(ctx, substrate.ActorSystem, groupKind(group), group, substrate.PatchInput{
		Properties: map[string]any{
			propPackageQuarantined:      true,
			propPackageQuarantineReason: reason,
		},
	})
	return err
}

// groupKind is the meta-kind a declaration GROUP stores as: a package row for a
// package, the authority row for an authority. The mark has to land on the row
// that exists, or an authority document this binary cannot parse could never be
// quarantined and the repository would refuse to open instead.
func groupKind(group string) string {
	if authority, _ := vocabulary.SplitPackageRef(group); authority == "" {
		return kindAuthority
	}
	return kindPackage
}

// clearGroupQuarantine lifts the quarantine marker off any of the given
// packages whose package row still carries it — the open-time twin of the
// projection clear, for a binary that relaxed a contract so the closure admits
// again without a re-install.
func (ds *dataset) clearGroupQuarantine(ctx context.Context, authorities []*vocabulary.Package) error {
	for _, g := range authorities {
		var flagged bool
		err := ds.db.QueryRowContext(ctx,
			`SELECT props ? $2 FROM records WHERE id = $1 AND kind = $3 AND deleted_at IS NULL`,
			g.Identity, propPackageQuarantined, groupKind(g.Identity)).Scan(&flagged)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !flagged {
			continue
		}
		if _, err := ds.patchInternal(ctx, substrate.ActorSystem, groupKind(g.Identity), g.Identity, substrate.PatchInput{
			Properties: map[string]any{propPackageQuarantined: nil, propPackageQuarantineReason: nil},
		}); err != nil {
			return err
		}
	}
	return nil
}

// storedPackages rebuilds every package the repository stores FROM its schema
// record rows, skipping the identities the caller already accounts for. Each
// package keeps the `source` its own row records: `builtin` for what the
// creation seed (or a shipped upgrade) wrote, `published` for a provider the
// catalog installed, `installed` for everything else a user or a bundle
// declared, so ownership reads the same answer at open as it did at the write. It parses and returns the packages without installing them
// anywhere, alongside the ones that no longer parse under this binary, which
// are the quarantine candidates for the caller to mark.
func (ds *dataset) storedPackages(ctx context.Context, skip func(string) bool) ([]*vocabulary.Package, []quarantinedPackage, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, COALESCE(props->>'source', $3) FROM records
		WHERE kind IN ($1, $2) AND deleted_at IS NULL
		ORDER BY created_at, id`, kindPackage, kindAuthority, vocabulary.SourceInstalled)
	if err != nil {
		return nil, nil, err
	}
	bySource := map[string]map[string]bool{}
	authorities := map[string]bool{}
	for rows.Next() {
		var name, source string
		if err := rows.Scan(&name, &source); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		if skip != nil && skip(name) {
			continue
		}
		// A row spelling an origin this binary does not know reads as the
		// weakest one: an unknown word must never be taken for ownership the
		// repository's own token cannot write past.
		if source != vocabulary.SourceBuiltin && source != vocabulary.SourcePublished {
			source = vocabulary.SourceInstalled
		}
		if bySource[source] == nil {
			bySource[source] = map[string]bool{}
		}
		bySource[source][name] = true
		authorities[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	_ = rows.Close()
	if len(authorities) == 0 {
		return nil, nil, nil
	}
	docs, err := ds.vocabularyDocumentRows(ctx, authorities)
	if err != nil {
		return nil, nil, err
	}
	// One BuildAuthorities pass per source: an authority is built with the origin its
	// own row claims, and the rebuilt types carry it (`Type.Source`).
	var built []*vocabulary.Package
	var unparsed []quarantinedPackage
	for _, source := range []string{vocabulary.SourceBuiltin, vocabulary.SourcePublished, vocabulary.SourceInstalled} {
		names := bySource[source]
		if len(names) == 0 {
			continue
		}
		all := make([]vocabulary.Document, 0, len(docs))
		for _, key := range sortedKeys(docs) {
			if d := docs[key]; names[d.DeclaredPackage()] {
				all = append(all, d)
			}
		}
		gs, err := vocabulary.BuildPackages(all, source)
		if err == nil {
			built = append(built, gs...)
			continue
		}
		// BuildAuthorities reports every problem in the whole stream at once,
		// so one authority whose manifests no longer parse under this binary
		// (an agent still naming the deleted `llm` key, say) would take down
		// every authority that shares its source — and with it the repository's
		// open. Issue 010's quarantine, one step earlier: rebuild per
		// authority, keep the ones that still parse.
		good, bad, cerr := buildPackagesSeparately(all, source)
		if cerr != nil {
			return nil, nil, fmt.Errorf("substrate/engine: rebuild stored schema: %w", cerr)
		}
		built = append(built, good...)
		unparsed = append(unparsed, bad...)
	}
	return built, unparsed, nil
}

// buildPackagesSeparately rebuilds a source's documents ONE AUTHORITY AT A
// TIME — every authority is bucketed and built independently anyway, so this
// changes nothing but the blast radius of a failure. Core is the exception it
// returns an error for: a repository whose own meta-kinds do not parse
// resolves nothing, so quarantining core would be a lie dressed as a recovery.
func buildPackagesSeparately(docs []vocabulary.Document, source string) ([]*vocabulary.Package, []quarantinedPackage, error) {
	byPackage := map[string][]vocabulary.Document{}
	var order []string
	for _, d := range docs {
		name := d.DeclaredPackage()
		if _, seen := byPackage[name]; !seen {
			order = append(order, name)
		}
		byPackage[name] = append(byPackage[name], d)
	}
	var built []*vocabulary.Package
	var unparsed []quarantinedPackage
	for _, name := range order {
		gs, err := vocabulary.BuildPackages(byPackage[name], source)
		if err == nil {
			built = append(built, gs...)
			continue
		}
		if name == vocabulary.PackageCore {
			return nil, nil, err
		}
		unparsed = append(unparsed, quarantinedPackage{name: name, reason: cappedQuarantineReason(err.Error())})
	}
	return built, unparsed, nil
}

// --- the generic verbs, routed through admission --------------------------------

// putSchemaRecord is a generic PUT of one schema record: a batch of one. The
// row's authored content IS its properties, and what the engine owns is refused
// rather than obeyed or dropped (checkDeclarationWrite).
func (ds *dataset) putSchemaRecord(ctx context.Context, actor substrate.Actor, ty *vocabulary.Kind, in substrate.PutInput) (*substrate.Record, error) {
	short := vocabularyRecordKinds[ty.Identity]
	if in.ID == "" {
		return nil, fmt.Errorf("%w: %s records are addressed by their declared identity — put carries metadata.id",
			substrate.ErrValidation, short)
	}
	// The stored row is what an echoed managed property is compared against; a
	// missing one is a create, where there is nothing to echo.
	existing, err := ds.Get(ctx, ty.Identity, in.ID)
	if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		return nil, err
	}
	if err := checkDeclarationWrite(ty, short, existing, in.Properties); err != nil {
		return nil, err
	}
	doc, err := documentFromProps(short, in.ID, in.Properties)
	if err != nil {
		return nil, err
	}
	b := vocabularyBatch{
		docs: []vocabulary.Document{doc},
		meta: map[string]vocabularyDocMeta{docKey(doc): {
			labels: in.Labels, annotations: in.Annotations, ifVersion: in.IfVersion,
		}},
	}
	written, err := ds.applyVocabularyBatch(ctx, actor, b)
	if err != nil {
		return nil, err
	}
	if e, ok := written[docKey(doc)]; ok {
		return e, nil
	}
	return ds.Get(ctx, ty.Identity, in.ID)
}

// patchSchemaRecord merges a patch's properties over the stored row and runs
// the result through admission, exactly like a put.
func (ds *dataset) patchSchemaRecord(ctx context.Context, actor substrate.Actor, existing *substrate.Record, in substrate.PatchInput) (*substrate.Record, error) {
	short := vocabularyRecordKinds[existing.Kind]
	ty, err := ds.resolveType(existing.Kind)
	if err != nil {
		return nil, err
	}
	// The PATCH's own properties are what the writer supplied; the merge below
	// carries the stored ones, which are the engine's own answer by definition.
	if err := checkDeclarationWrite(ty, short, existing, in.Properties); err != nil {
		return nil, err
	}
	props := map[string]any{}
	for k, v := range existing.Properties {
		props[k] = v
	}
	for k, v := range in.Properties {
		if v == nil {
			delete(props, k)
			continue
		}
		props[k] = v
	}
	doc, err := documentFromProps(short, existing.ID, props)
	if err != nil {
		return nil, err
	}
	b := vocabularyBatch{
		docs: []vocabulary.Document{doc},
		meta: map[string]vocabularyDocMeta{docKey(doc): {
			labels: in.Labels, annotations: in.Annotations, ifVersion: in.IfVersion,
		}},
	}
	written, err := ds.applyVocabularyBatch(ctx, actor, b)
	if err != nil {
		return nil, err
	}
	if e, ok := written[docKey(doc)]; ok {
		return e, nil
	}
	return ds.Get(ctx, existing.Kind, existing.ID)
}

// deleteVocabularyRecord removes one declaration through admission: the closure
// must still hold without it (an authority header outlives its members; a type
// with live instances refuses).
func (ds *dataset) deleteVocabularyRecord(ctx context.Context, actor substrate.Actor, existing *substrate.Record) (*substrate.Record, error) {
	doc, ok, err := rowDocument(existing.ID, existing.Kind, existing.Properties)
	if err != nil {
		return nil, err
	}
	if !ok {
		// A row too old to rebuild still names its package and kind.
		doc = vocabulary.Document{
			Kind: vocabularyRecordKinds[existing.Kind], ID: existing.ID,
			Data: map[string]any{
				"authority": fmt.Sprint(existing.Properties["authority"]),
				"package":   fmt.Sprint(existing.Properties["package"]),
			},
		}
	}
	if _, err := ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{deletes: []vocabulary.Document{doc}}); err != nil {
		return nil, err
	}
	return ds.Get(ctx, existing.Kind, existing.ID)
}

// documentFromProps rebuilds the loader document a generic write carries: the
// declaration IS the properties, so the document's data is the supplied
// properties the loader admits for that kind. What the ENGINE owns is refused
// rather than dropped (checkDeclarationWrite, the caller's first step): a
// silently replaced value would tell a client its edit landed.
//
// There is no `definition` arm: the blob has no legal spelling any more, on the
// wire or in a row. A write carrying one is refused by name upstream, and a
// merge over a row that still holds one carries no declaration at all — which is
// what the empty-data refusal below says.
func documentFromProps(short, id string, props map[string]any) (vocabulary.Document, error) {
	d := vocabulary.Document{Kind: short, ID: id}
	switch short {
	case vocabulary.DocAuthority:
		// An authority row owns the packages published under it and says what
		// it is; a closure's ownership and quarantine sit on the PACKAGE
		// (record 0047), so the row's own version and description are the whole
		// of what a write may carry.
		data := map[string]any{}
		if v, ok := vocabulary.VersionValue(props["version"]); ok && v > 0 {
			data["version"] = v
		}
		if desc, _ := props["description"].(string); desc != "" {
			data["description"] = desc
		}
		d.Data = data
	case kindActorLocal:
		authority, _ := props["authority"].(string)
		pkg, _ := props["package"].(string)
		if authority == "" || pkg == "" {
			return d, fmt.Errorf("%w: an actor record carries `authority` and `package` — the package that declares it", substrate.ErrValidation)
		}
		d.Data = map[string]any{"authority": authority, "package": pkg}
		if tier, _ := props["tier"].(string); tier != "" && tier != string(substrate.TierMachine) {
			d.Data["tier"] = tier
		}
	default:
		d.Data = declarationData(short, props)
		if len(d.Data) == 0 {
			return d, fmt.Errorf("%w: a %s record carries its declaration in its properties — this write carries none",
				substrate.ErrValidation, short)
		}
	}
	return d, nil
}

// checkDeclarationWrite holds a generic declaration write to what the writer
// owns. Three answers, and they are the settled rule:
//
//   - a property the loader admits is the DECLARATION's, and it lands;
//   - a MANAGED property (the version, the origin, the quarantine marks, the
//     bundle lifecycle bools — each declared `managed: true`) is the engine's:
//     absent is fine, since the engine stamps it, and an echoed value EQUAL to
//     the one the row already holds is fine, since `get -o yaml | apply -f` must
//     round-trip. A DIFFERENT value is refused, naming the property: silently
//     replacing it would tell the client its edit landed;
//   - anything else is refused as undeclared, rather than dropped, so a typo
//     is not a change that vanishes. A RETIRED spelling is refused by that name —
//     the `definition` blob naming the properties that carry the declaration
//     instead, the dead mirrors naming the rule — since a writer still sending
//     one is working from a document this substrate stopped storing.
//
// A DELETED declaration key is the fourth answer, and it is the loader's own
// (vocabulary.DeletedDeclarationKeys): a declaration arrives here as properties
// and at the YAML door as a document, and the two doors say the same sentence,
// so `emit` names `permissions.writes` whichever one the writer knocked at.
// Like the blob, it is decided by PRESENCE: no row carries one of these keys, so
// there is nothing a null could be clearing.
//
// `title` and `body` are the exception that proves the rule: every record
// carries them in a column, a read hands them back in `properties`, and a
// declaration's title is derived from its template — so an echoed one is
// ignored, not refused.
func checkDeclarationWrite(ty *vocabulary.Kind, short string, existing *substrate.Record, props map[string]any) error {
	dataKeys := vocabulary.DeclarationDataKeys(short)
	deleted := vocabulary.DeletedDeclarationKeys(short)
	var problems []string
	for _, name := range sortedKeys(props) {
		switch {
		// THE BLOB FIRST, and by PRESENCE: a null is this dialect's delete marker
		// everywhere else, but there is nothing left to delete under this key — no
		// row carries one — so a write naming it is a client working from a
		// document this substrate stopped storing, whatever value it sends.
		case name == propDeclarationBlob:
			problems = append(problems, fmt.Sprintf(
				"props.%s: the retired blob — a %s carries its declaration in its own properties: %s",
				name, short, strings.Join(sortedKeys(dataKeys), ", ")))
			continue
		case deleted[name] != "":
			problems = append(problems, fmt.Sprintf(
				"props.%s: the deleted key, replaced by %s", name, deleted[name]))
			continue
		case dataKeys[name], columnBackedProp[name], props[name] == nil:
			continue
		case retiredDeclarationProps(short)[name]:
			problems = append(problems, fmt.Sprintf("props.%s: the retired spelling — the declaration is the properties now", name))
			continue
		}
		p, declared := ty.Prop(name)
		if !declared {
			problems = append(problems, fmt.Sprintf("props.%s: not declared on %s", name, ty.Identity))
			continue
		}
		if !p.Managed {
			continue // an ordinary property of the kind the loader does not read
		}
		var held any
		if existing != nil {
			held = existing.Properties[name]
		}
		if jsonEqual(held, props[name]) {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"props.%s: the engine stamps it (%v) — drop it or send the stored value", name, held))
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
}

// reprojectedKinds lists the touched packages' kinds whose reference
// declarations differ between the stored closure and the candidate — added,
// dropped, re-shaped or given link data. A kind the candidate drops entirely is
// in the list too: its records' rows must go with the declaration that
// described them.
func reprojectedKinds(current, candidate *vocabulary.Registry, touched map[string]bool) []string {
	sites := func(reg *vocabulary.Registry, ident string) string {
		ty, ok := reg.ByIdentity(ident)
		if !ok {
			return ""
		}
		var b strings.Builder
		for _, name := range ty.PropOrder {
			appendReferenceShape(&b, name, ty.Props[name])
		}
		return b.String()
	}
	seen := map[string]bool{}
	var out []string
	for aname := range touched {
		for _, reg := range []*vocabulary.Registry{current, candidate} {
			g, ok := reg.PackageByName(aname)
			if !ok || g == nil {
				continue
			}
			for _, tn := range g.KindOrder {
				ident := g.Kinds[tn].Identity
				if seen[ident] {
					continue
				}
				seen[ident] = true
				if sites(current, ident) != sites(candidate, ident) {
					out = append(out, ident)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// appendReferenceShape writes the part of one declaration that the refs
// derivation reads: where a reference sits, in which container, and which link
// properties travel with it. Two declarations with the same string project the
// same rows from the same stored values.
//
// EVERY NODE ON A REFERENCE-BEARING PATH, with its datatype and its container
// flags, not the reference node alone. deriveRefs addresses a nested pointer
// through its ANCESTORS: an object property that gains `repeated: true` moves
// the pointer inside it from `callable` to `0.callable`, and an ancestor that
// changes datatype stops being walked at all. A fingerprint that read only the
// leaf compared equal across those, so no kind was re-projected and tombstoned
// records kept rows at addresses the declaration no longer describes.
//
// A node holding no reference anywhere below it is skipped whole, because
// deriveRefs does not walk it either (holdsReference).
func appendReferenceShape(b *strings.Builder, path string, p *vocabulary.Property) {
	if !holdsReference(p) {
		return
	}
	fmt.Fprintf(b, "%s|%s|%v|%v|%s;", path, p.Datatype, p.Repeated, p.Keyed,
		strings.Join(p.PropertyOrder, ","))
	for _, fn := range p.FieldOrder {
		appendReferenceShape(b, path+"."+fn, p.Fields[fn])
	}
}

// reprojectRefs re-derives the refs index for the kinds whose reference
// declarations moved, against the CANDIDATE closure: the candidate is what the
// committed rows must project against, and the live registry does not hold it
// until the publish.
func (t *txn) reprojectRefs(candidate *vocabulary.Registry, kinds []string) error {
	prev := t.writeReg
	t.writeReg = candidate
	defer func() { t.writeReg = prev }()
	for _, ident := range kinds {
		if err := t.syncRefsOfKind(ident); err != nil {
			return err
		}
	}
	return nil
}
