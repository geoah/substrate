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
// back to its full type identity ("core.substrate.reamde.dev/kind").
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
	docs               []vocabulary.Document
	deletes            []vocabulary.Document
	replaceAuthorities []string
	meta               map[string]vocabularyDocMeta // keyed by docKey
	// beforeGuards runs INSIDE the batch transaction, right after the
	// registry-dependency lock and BEFORE the refuse-breakage guards: a bundle
	// uninstall tears its delivery wiring (triggers referencing the owned
	// authority's callables) down here, so the dropped-callable guard sees them
	// already gone and the whole-authority teardown never refuses on its own
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
// authority/type/metadata/data envelope the loader has always parsed.
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
func (ds *dataset) InstallBundleClosure(ctx context.Context, actor substrate.Actor, vocabularyDocs []map[string]any, dataDocs []substrate.PutInput) ([]*substrate.Record, error) {
	if len(vocabularyDocs) == 0 {
		return nil, fmt.Errorf("%w: no schema documents", substrate.ErrValidation)
	}
	docs, err := parseVocabularyDocs(vocabularyDocs)
	if err != nil {
		return nil, err
	}
	written, err := ds.applyVocabularyBatch(ctx, actor, vocabularyBatch{
		docs: docs,
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
		cand, _ := candidate.AuthorityByName(aname)
		if cand == nil {
			continue
		}
		cur, _ := current.AuthorityByName(aname)
		// A re-install after an uninstall is a FRESH install: uninstall tears
		// the owned authority down whole (bundles.go), so `cur` is nil here and
		// every body prepares — the retired runner registrations warm again.
		for _, fname := range cand.FunctionOrder {
			f := cand.Functions[fname]
			if cur != nil {
				if prev := cur.Functions[fname]; prev != nil &&
					prev.Runtime == f.Runtime && prev.Source == f.Source {
					continue // unchanged body, already prepared
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
		// A whole-authority teardown (bundle uninstall) removes the owned authority's
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
		got, err := t.projectAuthorities(candidate, touched, projectOpts{meta: b.meta, prune: true})
		if err != nil {
			return err
		}
		for k, e := range got {
			written[k] = e
		}
		if b.extra != nil {
			if err := b.extra(t, candidate); err != nil {
				return err
			}
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
	narrowings       []narrowing
}

// stageVocabularyBatch builds the batch's candidate registry and classifies
// what admitting it would break. actor is the writing hand, checked at the
// authority chokepoint; nil means the caller is only ASKING (the upgrade
// preview), and a read needs no license to write: nothing here writes.
func (ds *dataset) stageVocabularyBatch(ctx context.Context, current *vocabulary.Registry, actor *substrate.Actor, b vocabularyBatch) (*vocabularyStage, error) {
	// A batch carrying a bundle document is a whole-authority apply: the closure
	// the document lists IS the authority, so install and upgrade both REPLACE it
	// — one atomic re-apply, absent declarations pruned, breakage refused
	// below. (This is the connector-registration transaction, generalized.)
	for _, d := range b.docs {
		if d.Kind == vocabulary.DocBundle {
			b.replaceAuthorities = append(b.replaceAuthorities, d.DeclaredAuthority())
		}
	}

	// The touched authorities: every authority a document declares into, plus the
	// wholesale replacements.
	touched := map[string]bool{}
	var problems []string
	for _, d := range append(append([]vocabulary.Document(nil), b.docs...), b.deletes...) {
		g := d.DeclaredAuthority()
		if g == "" {
			problems = append(problems, fmt.Sprintf("%s %s: data.authority is required", d.Kind, d.ID))
			continue
		}
		touched[g] = true
	}
	for _, g := range b.replaceAuthorities {
		touched[g] = true
	}
	if len(problems) > 0 {
		return nil, &substrate.ValidationError{Problems: problems}
	}
	if len(touched) == 0 {
		return nil, fmt.Errorf("%w: no documents", substrate.ErrValidation)
	}
	if actor != nil {
		// THE AUTHORITY CHECK, at the one chokepoint (seed.go): who may write a
		// kind DECLARATION into each touched authority.
		for _, g := range sortedKeys(touched) {
			if err := authorizeDeclarationWrite(*actor, current, g); err != nil {
				return nil, err
			}
		}
		// An actor's id does not embed its authority (alone among the kinds), so a
		// document can CLAIM any authority. Check by the actor declaration's CURRENT
		// authority too: a shipped actor redeclared into an installed authority would
		// otherwise overwrite the shipped row from outside its authority.
		for _, d := range append(append([]vocabulary.Document(nil), b.docs...), b.deletes...) {
			if d.Kind != kindActorLocal {
				continue
			}
			g, ok := current.ActorAuthority(d.ID)
			if !ok {
				continue
			}
			if err := authorizeDeclarationWrite(*actor, current, g); err != nil {
				return nil, fmt.Errorf("actor %s belongs to %s: %w", d.ID, g, err)
			}
		}
	}

	replaced := map[string]bool{}
	for _, g := range b.replaceAuthorities {
		replaced[g] = true
	}

	// Current documents of the touched authorities, from their record rows.
	existing, err := ds.vocabularyDocumentRows(ctx, touched)
	if err != nil {
		return nil, err
	}
	merged := map[string]vocabulary.Document{}
	for k, d := range existing {
		if replaced[d.DeclaredAuthority()] {
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

	// The candidate: current registry minus the touched authorities, plus their
	// rebuilt replacements — compiled whole (CEL guards, templates) before the
	// transaction opens. Every problem in every document reports at once.
	candidate := current.Clone()
	for g := range touched {
		candidate.Remove(g)
	}
	byAuthority := map[string][]vocabulary.Document{}
	for _, d := range merged {
		g := d.DeclaredAuthority()
		byAuthority[g] = append(byAuthority[g], d)
	}
	var rebuilt []*vocabulary.Authority
	for _, aname := range sortedKeys(byAuthority) {
		gs, err := vocabulary.BuildAuthorities(byAuthority[aname], vocabulary.SourceInstalled)
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

	// Types the candidate no longer declares: refuse while instances exist,
	// counted transactionally below. Functions carry no delivery state of
	// their own any more — cursors belong to TRIGGER records, which outlive
	// a function's uninstall (the dispatcher skips a trigger whose callable
	// no longer resolves, loudly, without moving its cursor).
	var droppedTypes []string
	for aname := range touched {
		cur, _ := current.AuthorityByName(aname)
		cand, _ := candidate.AuthorityByName(aname)
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
		// Evolution-with-data: a NARROWING definition
		// diff — property dropped/renamed/kind-changed, enum value or state
		// removed, required added — is classified here against the currently
		// stored definitions and refused while live rows would be stranded,
		// with the count. Additive changes pass through untouched (schemadiff.go).
		narrowings: classifyNarrowings(current, candidate, touched),
	}, nil
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
	// prune tombstones the touched authorities' declarations the registry no
	// longer declares — what a whole-authority re-apply (install, upgrade,
	// uninstall) needs. THE SEED AND THE SHIPPED-VOCABULARY UPGRADE NEVER
	// PRUNE: the embedded tree is a source, the repository's own
	// changelog is the truth, and re-assert-and-prune is dead.
	prune bool
}

// projectAuthorities writes the touched authorities' declarations as record rows
// from the candidate registry — no-op suppressed, so unchanged declarations
// stay changelog-silent — and, when asked, tombstones rows of those authorities the
// candidate stopped declaring. It returns every written (or unchanged) record
// keyed by kind+id.
func (t *txn) projectAuthorities(reg *vocabulary.Registry, authorities map[string]bool, opts projectOpts) (map[string]*substrate.Record, error) {
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
		g, ok := reg.AuthorityByName(aname)
		if !ok {
			continue // the authority is being removed whole; prune takes its rows
		}
		decls, err := authorityDeclarations(g)
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
		if err := t.projectAuthority(reg, projecting, decls, live, opts, out); err != nil {
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
func (d declaration) version() string {
	v, _ := d.props["version"].(string)
	return v
}

// projectAuthority writes one authority's declarations: the header, its actors, and
// every property type, trait, record type, mapping, function, agent and
// bundle. `projecting` is the whole pass's set of kinds whose own declaration
// this projection writes, which is what projectionKind reads.
func (t *txn) projectAuthority(reg *vocabulary.Registry, projecting map[string]bool, decls []declaration, live map[string]bool, opts projectOpts, out map[string]*substrate.Record) error {
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
	props := make(map[string]any, len(d.props)+len(row.Props))
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

// authorityDeclarations renders every declaration an authority stores as rows, in one
// stable order. It is the ONE enumeration of an authority's contents: the
// projection writes it, and the boot-time upgrade diffs versions across it
// (seed.go), so the writer and the differ can never disagree about what a
// authority holds.
//
// A ROW IS THE DECLARATION'S OWN DATA MAP plus what the engine stamps. There is
// no `definition` blob and no projected mirror: the loader left the parsed data
// in the one canonical form (vocabulary/canonical.go), so the properties a row
// carries are the keys the author wrote, and the read-back
// (rowDocument) is that same map with the stamped keys dropped. What the engine
// stamps is declared `managed: true` on the core kinds, which is the same list
// spelled where a client can read it.
//
// EVERY DECLARATION CARRIES A VERSION. A record type's own
// `version:` wins where it declares one; every other kind takes the declaring
// authority's, which is the cheapest consistent rule — an authority's version is a
// statement about the declarations it ships.
func authorityDeclarations(g *vocabulary.Authority) ([]declaration, error) {
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
		if v, ok := props["version"].(string); !ok || v == "" {
			props["version"] = g.Version
		}
		if v, _ := props["version"].(string); v == "" {
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
	// The explicit quarantined/quarantineReason nulls CLEAR any issue-010
	// marker: a re-projection of the authority (a catalog re-install producing a
	// valid closure) is what lifts the quarantine. Null against an absent
	// property is a no-op, so a healthy authority's re-projection writes nothing.
	if err := add(vocabulary.DocAuthority, kindAuthority, g.Name,
		map[string]any{"version": g.Version},
		map[string]any{
			"actors": actors, "source": g.Source,
			propAuthorityQuarantined: nil, propAuthorityQuarantineReason: nil,
		}); err != nil {
		return nil, err
	}
	for _, a := range g.Actors {
		// A single-writer connector's actor IS its authority: the
		// authority row above already holds that id and lists the actor.
		if a == g.Name {
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
			map[string]any{"authority": g.Name, "tier": tier},
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
		// pre-teardown binary wrote — uninstall is a whole-authority teardown now
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
// nothing derives it: the `definition` blob, the id-derived `name` and `plural`,
// an agent's function/sub-agent mirrors, a never-stored `sourceYAML`. The
// projection writes each as an explicit null so a merge-only put clears them.
// The list cannot grow — a spelling is retired once — and stage C deletes it
// with the arms that read it.
func retiredDeclarationProps(short string) map[string]bool {
	out := map[string]bool{"definition": true, "sourceYAML": true, "name": true}
	switch short {
	case vocabulary.DocKind:
		out["plural"] = true
	case vocabulary.DocAgent:
		out["functions"], out["subagents"] = true, true
	}
	return out
}

// engineOwned reports whether a stored property is the ENGINE's rather than the
// declaration's: not a document key, and not one of the retired spellings. It is
// what the rung preserves across a translation (a version ahead of its
// authority's, a quarantine mark, a disabled bundle) and what a projection
// leaves alone.
func engineOwned(short, prop string) bool {
	return !vocabulary.DeclarationDataKeys(short)[prop] && !retiredDeclarationProps(short)[prop] &&
		!columnBackedProp[prop]
}

// pruneSchemaRows tombstones schema record rows of the touched authorities the
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
		SELECT id, kind, props->>'authority' FROM records
		WHERE kind IN (`+strings.Join(ph, ", ")+`) AND deleted_at IS NULL`, args...)
	if err != nil {
		return err
	}
	type srow struct{ id, typ, authority string }
	var all []srow
	for rows.Next() {
		var id, typ string
		var authority *string
		if err := rows.Scan(&id, &typ, &authority); err != nil {
			_ = rows.Close()
			return err
		}
		g := ""
		if typ == kindAuthority {
			g = id
		} else if authority != nil {
			g = *authority
		}
		all = append(all, srow{id, typ, g})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	type stale struct{ id, typ string }
	var gone []stale
	for _, s := range all {
		if !authorities[s.authority] || live[s.typ+"\x00"+s.id] {
			continue
		}
		gone = append(gone, stale{s.id, s.typ})
	}
	for _, s := range gone {
		ref := eref{Kind: s.typ, ID: s.id}
		if s.typ == kindBundle {
			// The bundle row's outgoing edges are its input BINDINGS
			// (inputs.go). A tombstone leaves edges standing, so without
			// this a later re-install of the same bundle would resurrect a
			// binding the uninstall was supposed to clear — fresh install,
			// stale explicit choice. The deletes ride the tombstone's own
			// changelog entry, so a rebuild replays them.
			edges, err := t.edgesOf(ref)
			if err != nil {
				return err
			}
			rels := mapKeysOf(edges)
			sort.Strings(rels)
			for _, rel := range rels {
				for _, dst := range edges[rel] {
					if _, err := t.deleteEdge(rel, ref, dst); err != nil {
						return fmt.Errorf("substrate/engine: prune bundle binding %s: %w", rel, err)
					}
				}
			}
		}
		if _, err := t.softDelete(ref); err != nil {
			return fmt.Errorf("substrate/engine: prune schema row %s: %w", s.id, err)
		}
	}
	return nil
}

// mapKeysOf lists a map's keys, for deterministic iteration through
// sortedStrings.
func mapKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- rows back into documents -------------------------------------------------

// vocabularyDocumentRows reads the touched authorities' schema record rows back as
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
		if !ok || !authorities[d.DeclaredAuthority()] {
			continue
		}
		if typ == kindAuthority {
			authorityActors[id] = append(authorityActors[id], anyStrings(props["actors"])...)
		}
		out[docKey(d)] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Record 60: an authority-named actor has no row of its own — the authority
	// row is it. Synthesize its document so the rebuilt authority keeps the actor.
	for aname, actors := range authorityActors {
		for _, a := range actors {
			if a != vocabulary.AuthorityActor(aname) {
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
	switch short {
	case vocabulary.DocAuthority:
		data := map[string]any{}
		if v, _ := props["version"].(string); v != "" {
			data["version"] = v
		}
		d.Data = data
	case kindActorLocal:
		g, _ := props["authority"].(string)
		if g == "" {
			return vocabulary.Document{}, false, nil // a pre-promotion mirror row; the seed rewrites it
		}
		d.Data = map[string]any{"authority": g}
		// machine is the parse default, so the rebuilt document omits it —
		// the boot no-op comparison stays parsed projection against parsed
		// projection.
		if tier, _ := props["tier"].(string); tier != "" && tier != string(substrate.TierMachine) {
			d.Data["tier"] = tier
		}
	default:
		// THE TYPED ROW: its properties ARE the declaration, so the document's
		// data is the property map with the engine's own keys dropped
		// (serverDeclarationProps). Nothing is derived and nothing is renamed —
		// what the author wrote is what comes back.
		if def, legacy := props["definition"].(map[string]any); legacy {
			// A row an older binary wrote, before the typed flip: its authored
			// content is the blob alone. Reachable only before the dialect rung has
			// run over this repository (dialect.go).
			d.Data = def
			break
		}
		d.Data = declarationData(short, props)
	}
	// No sourceYAML read-back: rows store the parsed declaration only (record
	// 61), so the rebuilt document's source is agreed absence — the boot no-op
	// comparison is parsed projection against parsed projection.
	return d, true, nil
}

// declarationData is one declaration row's DOCUMENT data: the properties the
// loader admits for that kind, and nothing else. It is the read half of the
// projection's write (authorityDeclarations), and it is a WHITELIST rather than
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

// The authority-level quarantine markers, on the authority row. A
// stored closure that no longer admits under the current binary is marked
// here and left OUT of the live registry, so its one bundle is disabled
// instead of the whole repository bricking; a re-install (which projects a valid
// closure, clearing the marker) is how it clears.
const (
	propAuthorityQuarantined      = "quarantined"
	propAuthorityQuarantineReason = "quarantineReason"
	// quarantineReasonMax bounds the stored admission-error text so a huge
	// problem list can never bloat the authority row.
	quarantineReasonMax = 2000
)

// quarantinedAuthority pairs a stored authority that could not join the live
// registry with the reason. It carries the NAME, not a built authority: a
// closure that fails to PARSE never produces one, and the name is all the
// marker needs.
type quarantinedAuthority struct {
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
// earlier, for a closure that no longer PARSES (storedAuthorities): both
// failures arrive here as quarantine candidates and are marked identically.
func (ds *dataset) loadStoredVocabulary(ctx context.Context) error {
	built, unparsed, err := ds.storedAuthorities(ctx, nil)
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
		var inadmissible []quarantinedAuthority
		good, inadmissible = ds.admissibleSubset(built)
		quarantined = append(quarantined, inadmissible...)
	}
	if err := ds.clearGroupQuarantine(ctx, good); err != nil {
		return err
	}
	for _, q := range quarantined {
		ds.svc.log.Error("substrate: quarantining a stored closure that no longer loads under this binary — the repository opens WITHOUT it; re-install the bundle to clear",
			"repository", ds.info.Name, "authority", q.name, "reason", q.reason)
		if err := ds.markGroupQuarantined(ctx, q.name, q.reason); err != nil {
			return err
		}
	}
	return nil
}

// admissibleSubset installs the maximal subset of built authorities that admits
// into ds.reg and returns the rest as quarantined, with each one's admission
// error. It is a fixpoint over single-authority Install: an authority that depends on a
// sibling admits once that sibling is in, and Install self-removes an authority
// that fails — so ds.reg is left holding exactly the admissible subset. n
// (installed bundle authorities) is small, so the O(n^2) worst case is fine.
func (ds *dataset) admissibleSubset(built []*vocabulary.Authority) (good []*vocabulary.Authority, quarantined []quarantinedAuthority) {
	remaining := append([]*vocabulary.Authority(nil), built...)
	for {
		progressed := false
		var next []*vocabulary.Authority
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
		quarantined = append(quarantined, quarantinedAuthority{name: g.Name, reason: cappedQuarantineReason(reason)})
	}
	return good, quarantined
}

// markGroupQuarantined records the quarantine state on the authority's authority
// row, so BundleStatuses can surface it and a later re-install (which
// re-projects the row, clearing these props) lifts it.
func (ds *dataset) markGroupQuarantined(ctx context.Context, authority, reason string) error {
	_, err := ds.patchInternal(ctx, substrate.ActorSystem, kindAuthority, authority, substrate.PatchInput{
		Properties: map[string]any{
			propAuthorityQuarantined:      true,
			propAuthorityQuarantineReason: reason,
		},
	})
	return err
}

// clearGroupQuarantine lifts the quarantine marker off any of the given authorities
// whose authority row still carries it — the open-time twin of the
// projection clear, for a binary that relaxed a contract so the closure admits
// again without a re-install.
func (ds *dataset) clearGroupQuarantine(ctx context.Context, authorities []*vocabulary.Authority) error {
	for _, g := range authorities {
		var flagged bool
		err := ds.db.QueryRowContext(ctx,
			`SELECT props ? $2 FROM records WHERE id = $1 AND kind = $3 AND deleted_at IS NULL`,
			g.Name, propAuthorityQuarantined, kindAuthority).Scan(&flagged)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !flagged {
			continue
		}
		if _, err := ds.patchInternal(ctx, substrate.ActorSystem, kindAuthority, g.Name, substrate.PatchInput{
			Properties: map[string]any{propAuthorityQuarantined: nil, propAuthorityQuarantineReason: nil},
		}); err != nil {
			return err
		}
	}
	return nil
}

// storedAuthorities rebuilds every authority the repository stores FROM its schema
// record rows, skipping the names the caller already accounts for. Each authority
// keeps the `source` its authority row records — `builtin` for what the
// creation seed (or a shipped upgrade) wrote, `installed` for everything a
// user or a bundle declared — so authority reads the same answer at open
// as it did at the write. It parses and returns the authorities without installing
// them anywhere, alongside the ones that no longer parse under this binary —
// quarantine candidates for the caller to mark.
func (ds *dataset) storedAuthorities(ctx context.Context, skip func(string) bool) ([]*vocabulary.Authority, []quarantinedAuthority, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, COALESCE(props->>'source', $2) FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		ORDER BY created_at, id`, kindAuthority, vocabulary.SourceInstalled)
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
		if source != vocabulary.SourceBuiltin {
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
	var built []*vocabulary.Authority
	var unparsed []quarantinedAuthority
	for _, source := range []string{vocabulary.SourceBuiltin, vocabulary.SourceInstalled} {
		names := bySource[source]
		if len(names) == 0 {
			continue
		}
		all := make([]vocabulary.Document, 0, len(docs))
		for _, key := range sortedKeys(docs) {
			if d := docs[key]; names[d.DeclaredAuthority()] {
				all = append(all, d)
			}
		}
		gs, err := vocabulary.BuildAuthorities(all, source)
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
		good, bad, cerr := buildAuthoritiesSeparately(all, source)
		if cerr != nil {
			return nil, nil, fmt.Errorf("substrate/engine: rebuild stored schema: %w", cerr)
		}
		built = append(built, good...)
		unparsed = append(unparsed, bad...)
	}
	return built, unparsed, nil
}

// buildAuthoritiesSeparately rebuilds a source's documents ONE AUTHORITY AT A
// TIME — every authority is bucketed and built independently anyway, so this
// changes nothing but the blast radius of a failure. Core is the exception it
// returns an error for: a repository whose own meta-kinds do not parse
// resolves nothing, so quarantining core would be a lie dressed as a recovery.
func buildAuthoritiesSeparately(docs []vocabulary.Document, source string) ([]*vocabulary.Authority, []quarantinedAuthority, error) {
	byAuthority := map[string][]vocabulary.Document{}
	var order []string
	for _, d := range docs {
		name := d.DeclaredAuthority()
		if _, seen := byAuthority[name]; !seen {
			order = append(order, name)
		}
		byAuthority[name] = append(byAuthority[name], d)
	}
	var built []*vocabulary.Authority
	var unparsed []quarantinedAuthority
	for _, name := range order {
		gs, err := vocabulary.BuildAuthorities(byAuthority[name], source)
		if err == nil {
			built = append(built, gs...)
			continue
		}
		if name == vocabulary.AuthorityCore {
			return nil, nil, err
		}
		unparsed = append(unparsed, quarantinedAuthority{name: name, reason: cappedQuarantineReason(err.Error())})
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
		// A row too old to rebuild still names its authority and kind.
		doc = vocabulary.Document{
			Kind: vocabularyRecordKinds[existing.Kind], ID: existing.ID,
			Data: map[string]any{"authority": fmt.Sprint(existing.Properties["authority"])},
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
// The `definition` arm survives for one caller shape: a client (or a stored row
// read before the rung) still speaking the blob. It parses to the same document,
// so nothing downstream can tell which spelling arrived — but a write carrying
// BOTH spellings is refused, since obeying one of them means discarding the
// other's edits.
func documentFromProps(short, id string, props map[string]any) (vocabulary.Document, error) {
	d := vocabulary.Document{Kind: short, ID: id}
	switch short {
	case vocabulary.DocAuthority:
		data := map[string]any{}
		if v, ok := props["version"]; ok && v != nil {
			data["version"] = fmt.Sprint(v)
		}
		d.Data = data
	case kindActorLocal:
		g, _ := props["authority"].(string)
		if g == "" {
			return d, fmt.Errorf("%w: an actor record carries `authority` — the authority that declares it", substrate.ErrValidation)
		}
		d.Data = map[string]any{"authority": g}
		if tier, _ := props["tier"].(string); tier != "" && tier != string(substrate.TierMachine) {
			d.Data["tier"] = tier
		}
	default:
		data := declarationData(short, props)
		if def, blob := props["definition"].(map[string]any); blob {
			if len(data) > 0 {
				return d, fmt.Errorf("%w: a %s write carries its declaration TWICE — in `definition` and in %s; send one",
					substrate.ErrValidation, short, strings.Join(sortedKeys(data), ", "))
			}
			d.Data = def
			break
		}
		d.Data = data
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
//     is not a change that vanishes.
//
// `title` and `body` are the exception that proves the rule: every record
// carries them in a column, a read hands them back in `properties`, and a
// declaration's title is derived from its template — so an echoed one is
// ignored, not refused.
func checkDeclarationWrite(ty *vocabulary.Kind, short string, existing *substrate.Record, props map[string]any) error {
	if _, blob := props["definition"]; blob {
		return nil // the retired spelling carries the whole document; nothing to split
	}
	dataKeys := vocabulary.DeclarationDataKeys(short)
	var problems []string
	for _, name := range sortedKeys(props) {
		switch {
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
