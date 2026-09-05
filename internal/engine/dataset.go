package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// System types: state that belongs to the substrate's own machinery. The
// schema kinds among them are REAL, writable rows — but only through the
// loader-as-admission path (schemawrite.go); the txn-level guard still keeps
// the raw mutations out.
const (
	kindKind = "substrate.reamde.dev/core/kind"
	// The rest of the schema meta-model: the package's own header, the
	// authority that owns it, its traits, custom property types, mappings and
	// functions, so every declaration the console can render is addressable,
	// not just the kinds.
	kindPackage       = "substrate.reamde.dev/core/package"
	kindAuthority     = "substrate.reamde.dev/core/authority"
	kindTrait         = "substrate.reamde.dev/core/trait"
	kindPropertyType  = "substrate.reamde.dev/core/propertytype"
	kindRecordMapping = "substrate.reamde.dev/core/recordmapping"
	kindFunction      = "substrate.reamde.dev/core/function"
	kindAgent         = "substrate.reamde.dev/core/agent"
	kindBundle        = "substrate.reamde.dev/core/bundle"

	// kindRepository is the repository's own self-description: one record, id
	// `self`, saying which repository this is.
	kindRepository = "substrate.reamde.dev/core/repository"
	// kindToken and kindCredential (auth.go) are the two AUTH kinds: the
	// generic surface refuses writes to both (forbidSystemKind) and only the
	// auth paths write them. Revoking a token is the one exception — an
	// ordinary record delete, so every revoke path is the same write.
	kindToken       = "substrate.reamde.dev/core/token"
	kindActor       = "substrate.reamde.dev/core/actor"
	kindRecordMerge = "substrate.reamde.dev/core/recordmerge"
	kindRecordSplit = "substrate.reamde.dev/core/recordsplit"

	annApplyConflict = "substrate/conflict"

	// finalizerMerge holds a merged-away record against GC so split stays
	// possible.
	finalizerMerge = "substrate.merge"
)

var systemKinds = map[string]bool{
	kindKind: true, kindPackage: true, kindAuthority: true, kindTrait: true,
	kindPropertyType: true, kindRecordMapping: true, kindFunction: true,
	kindAgent: true, kindBundle: true,
	kindRepository: true, kindToken: true, kindCredential: true, kindRecoveryKey: true, kindActor: true,
	kindRecordMerge: true, kindRecordSplit: true,
	// The blob manifest is byte-store-managed: the generic record
	// API may not forge one, only the byte-store path writes it.
	kindBlob: true,
}

// The seams *dataset satisfies beyond substrate.Dataset. Each is an optional
// extension interface a consumer type-asserts (the HTTP layer, the catalog,
// the service loops), so without these assertions renaming or re-signaturing
// one method below compiles clean and turns a whole endpoint family into a
// 501 at runtime. Add a line here whenever a seam is added.
var (
	_ substrate.Dataset              = (*dataset)(nil)
	_ substrate.VocabularyApplier    = (*dataset)(nil)
	_ substrate.ChangeFeedOps        = (*dataset)(nil)
	_ substrate.AutomationOps        = (*dataset)(nil)
	_ substrate.TriggerDispatcher    = (*dataset)(nil)
	_ substrate.AgentOps             = (*dataset)(nil)
	_ substrate.ResolutionSweeper    = (*dataset)(nil)
	_ substrate.BundleOps            = (*dataset)(nil)
	_ substrate.BundleInstaller      = (*dataset)(nil)
	_ substrate.BundleUpgradePlanner = (*dataset)(nil)
	_ substrate.OAuthMaintainer      = (*dataset)(nil)
	_ substrate.BlobStore            = (*dataset)(nil)
)

type dataset struct {
	svc *service
	// db is the repository-scoped pool: every connection in it carries this
	// dataset's `substrate.repository` setting and runs as substrate_app, so
	// the row level security policies bind every statement it ever issues.
	db *sql.DB
	// scope is the repository the pool above is pinned to. It also keys the
	// per-repository advisory locks (identity.go lockKey).
	scope Scope
	// dek is the repository's data-encryption key, unwrapped at open: every
	// sealed-store payload seals under it. The control plane holds it
	// wrapped under the host credential key; the repository's recoverykey
	// record holds it wrapped to the user's age recipient.
	dek   []byte
	watch *broadcaster

	// changelogStamped remembers that this repository's changelog dialect is
	// COMMITTED at this binary's maximum, so the stamp rides the first
	// appending transaction and no later one (changelogdialect.go). It is set
	// after that transaction commits, never before: a rolled-back stamp that
	// left this true would let a later append land with nothing claiming it.
	changelogStamped atomic.Bool

	mu   sync.RWMutex
	reg  *vocabulary.Registry
	info substrate.RepositoryInfo
	// blobSweepAfter is the blob orphan sweep's cursor: the last digest the
	// previous pass looked at, so a store with more objects than one batch is
	// walked whole instead of the sweep restarting at the front every time.
	// Empty starts over, which is also where a restart begins.
	blobSweepAfter string

	// vocabularyWriteMu serializes SCHEMA writes (the ACCESS EXCLUSIVE analog,
	// scoped to schema): two concurrent schema writes must not both validate
	// against the same base and both commit. Data writes never take it.
	vocabularyWriteMu sync.Mutex

	// lifecycleFence is the ONE dataset-wide lifecycle fence (bundles.go,
	// review #2). An invocation TREE — a trigger delivery, a direct
	// call/chat/fire, and every nested host Call, function tool and sub-agent
	// under it — takes the SHARED side ONCE at its root and holds it through
	// its last effect, message, thread-settlement and cursor/fire-state write;
	// disable/uninstall/purge take the EXCLUSIVE side, so a lifecycle verb
	// drains every admitted invocation before it returns and nothing admits
	// while it runs. One fence, not per-bundle: a nested cross-bundle call
	// would otherwise re-acquire a SECOND reader, and a reader queued behind a
	// pending writer deadlocks. The root holds the single fence; nested calls
	// inherit the lease through the context (lifecycleLeaseKey) and only
	// RE-CHECK each callee's lifecycle under the held fence — never lock again.
	lifecycleFence sync.RWMutex

	// whens caches compiled trigger guards by source text (triggers.go).
	whens whenCache

	// warnedPolicies remembers which actionless policy rows have already been
	// warned about (policy.go). loadPolicies runs per write evaluation, and a
	// row that can no longer be written would otherwise warn on every agent
	// write for the life of the process.
	warnedPolicies sync.Map
}

func (ds *dataset) close() {
	ds.watch.close()
	_ = ds.db.Close()
}

func (ds *dataset) Repository() substrate.RepositoryInfo {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.info
}

func (ds *dataset) registry() *vocabulary.Registry {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.reg
}

// --- type registry ---

func (ds *dataset) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	types := ds.registry().Kinds()
	out := make([]substrate.KindInfo, 0, len(types))
	for _, t := range types {
		out = append(out, typeInfo(t))
	}
	return out, nil
}

func (ds *dataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	t, ok := ds.registry().ByIdentity(ref)
	if !ok {
		return substrate.KindInfo{}, fmt.Errorf("%w: kind %q", substrate.ErrNotFound, ref)
	}
	return typeInfo(t), nil
}

// typeInfo is one declared kind as the read surfaces see it.
//
// `Definition` is RENDERED FROM THE PARSED DECLARATION — the data map the loader
// left behind, which is the same map the projection writes as the row's
// properties (vocabularywrite.go packageDeclarations) — and no longer read out
// of a `definition` property, because no row carries one.
//
// It is the AUTHORED half of that map: the engine's own `version` is dropped,
// because a declaration that pinned none has one stamped onto its row, and after
// a reload the map would otherwise hand back a value nobody wrote — the same
// declaration reading differently before and after a restart, which is exactly
// what a client diffing it (or fingerprinting it, gql.RegistryKey) would trip
// over. The stamped value is `Version` beside it, which is where a reader that
// wants it already looks. Nothing else on a kind is engine-owned: `source` is
// not a document key at all.
func typeInfo(t *vocabulary.Kind) substrate.KindInfo {
	authority, pkg := vocabulary.SplitPackageRef(t.Package)
	return substrate.KindInfo{
		Identity: t.Identity, Name: t.Name,
		Authority: authority, Package: pkg, Version: t.Version,
		Source: t.Source, Description: t.Description,
		Definition: authoredKindData(t.Definition),
	}
}

// authoredKindData is a kind's declaration without the engine-stamped `version`.
// It copies rather than deletes: the map it is given is the REGISTRY's, and a
// registry-resident declaration is a read-only view (M5) — a delete here would
// reach the row the projection writes from it.
func authoredKindData(data map[string]any) map[string]any {
	if _, stamped := data[propDeclarationVersion]; !stamped {
		return data
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		if k == propDeclarationVersion {
			continue
		}
		out[k] = v
	}
	return out
}

func (ds *dataset) resolveType(name string) (*vocabulary.Kind, error) {
	return resolveKindIn(ds.registry(), name)
}

// resolveKindIn resolves a kind reference against a GIVEN registry. Every
// ordinary write resolves against the LIVE one (resolveType above); the
// vocabulary projection resolves a declaration row's kind against the candidate
// when this projection is what decides that kind's stored declaration
// (vocabularywrite.go projectionKind).
func resolveKindIn(reg *vocabulary.Registry, name string) (*vocabulary.Kind, error) {
	t, err := reg.Resolve(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	return t, nil
}

// Tokens and the credential are RECORDS, and everything about them —
// minting, the hash lookup, the sealed material behind the credential — lives
// in auth.go. There is no scope list and no actor set on a token any more: a
// token has full access to its repository, so there is nothing
// left here to configure.

// jsonSafe round-trips a YAML-decoded definition through JSON so it stores
// as jsonb and compares byte-identically on re-reconcile.
func jsonSafe(v map[string]any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// --- transactions ---

type txn struct {
	ctx   context.Context
	ds    *dataset
	tx    *sql.Tx
	actor substrate.Actor
	// tier is the transaction's manager tier — the WRITE CONTEXT's standing
	// against mapping recompute. Resolved once at inTx from the
	// actor's DATA (actorTier), and set to bundle EXPLICITLY by
	// function/agent dispatch (setEffectEmit) — never inferred from the
	// actor's spelling.
	tier substrate.Tier
	// principal is the token id the door verified for the request this
	// transaction serves (substrate.PrincipalFrom), stamped on every
	// changelog entry it appends and every manager row it lands. Empty when
	// no token stands behind the write — the seed, the boot upgrade, a
	// background worker, registration and login — and never taken from the
	// caller: the actor is asserted, the principal is resolved. It follows
	// the CONTEXT and nothing else, so a dispatch that runs on the request's
	// own context (an agent chat, a synchronous call) carries the token that
	// caused it, while one the trigger pass fires later runs on the server's
	// ticker context (ProcessTriggers) and carries none. The changelog's
	// caused_by is what links that entry back to the write behind it.
	principal string
	now       time.Time
	maxSeq    int64
	// entries records every changelog row this transaction appended, in
	// order: the (seq, op, kind, id) address, never the payload. Slices of it
	// stamp llmmessage rows with what a dispatch wrote (`changes`), so a
	// thread's reader resolves the delta from the changelog instead of
	// parsing tool payloads.
	entries []changeEntry
	// pending is the same appends as the CHECKSUM sees them: every column
	// the line carries, with the payload text AS POSTGRES STORED IT
	// (appendChange's RETURNING). settleChecksums stamps each one at commit,
	// after settleFold has made the last payload final, and leaves the
	// encoded lines here for the segment writer that runs after commit.
	pending []pendingEntry
	// changeSink, when set, receives this transaction's entries AFTER COMMIT
	// (inTx) — the agent loop's per-dispatch collector. After commit, so a
	// rolled-back write stamps nothing.
	changeSink *[]changeEntry
	// afterCommit runs once the transaction has committed, in order — the
	// decision's thread resume, which must never run inside the transaction
	// that recorded it.
	afterCommit []func()
	// interactionThread marks the agent loop's own ask dispatch: the ONE
	// writer allowed to stamp an interaction's thread reference
	// (interactions.go admitInteraction).
	interactionThread bool
	// policyDecision marks the ENGINE's own judge-driven decision on a
	// policy-gated request (phase 4): the one bundle-tier hand the gated
	// guard admits.
	policyDecision bool
	// folded holds the fold effects this transaction has applied and not yet
	// written into an entry (fold.go). appendChange drains it, so every entry
	// carries the delta — with values — that a rebuild replays.
	folded []foldOp
	// seqLocked records that this transaction already holds the changelog's
	// ordering lock (taken once, on the first append).
	seqLocked bool
	// internal writes bypass the system-type guard.
	internal bool
	// writeReg is the registry this transaction's DECLARATIONS come from, when it
	// is not the live one: the vocabulary projection resolves a declaration row's
	// kind against the candidate it is installing (vocabularywrite.go
	// projectionKind). The fold consults the registry for exactly one thing —
	// the weighted search bands — and computing those from a declaration OTHER
	// than the one the row was validated against is what made a live row and its
	// own replay disagree: a replay reads the registry the rebuild holds, which is
	// the declaration the row ended up under.
	writeReg *vocabulary.Registry
	// refMissing collects the `mustExist:` misses of the reference pass, so the
	// refusal leaves as the not-found it is rather than as a shape problem
	// (references.go validateReferences).
	refMissing []error
	// recomputing marks a mapping recompute's own write (§7.1): recompute
	// never triggers recompute, and the manager ledger records the winning
	// contributor's actor — recomputeManagers, per accepted property —
	// instead of the transaction's, always at the machine tier (attribution
	// never pins).
	recomputing       bool
	recomputeManagers map[string]substrate.Actor
	// causedBy is the changelog seq that caused this transaction's writes —
	// set only when a function's effects apply, stamped onto every changelog
	// row so the causal-depth walk can follow the chain.
	causedBy int64
	// effEmit is the EFFECTIVE emit set of the bundle actor whose effects
	// this transaction applies: a function's `permissions.writes`, or an
	// agent's declared emit intersected with its inherited ceiling (wave-3
	// #12). effEmitSet distinguishes an EMPTY set (the actor may emit nothing)
	// from an absent one (the generic write API, where no effect ceiling
	// applies). Carried so that ACCEPTING a change request authorizes the
	// TRANSITIVE write against the same ceiling the direct effect would face:
	// a function that can only emit the request type cannot become a confused
	// deputy for an arbitrary create/delete.
	effEmit    []string
	effEmitSet bool
	// heldRegistryDep records that this transaction already took the SHARED
	// registry-dependency lock, so the several doors that need it (the write's
	// entry, preRecordLocks, trigger and policy admission) can each ask without
	// a round trip per ask. An advisory lock is transaction-scoped and released
	// at commit, so "taken once" is "held to the end".
	heldRegistryDep bool
}

func (ds *dataset) inTx(ctx context.Context, actor substrate.Actor, internal bool, fn func(*txn) error) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// ONE rollback covers every exit, the PANIC included: a panic that unwound
	// past here would leave the transaction open and its connection held out of
	// an 8-connection pool until the context died, and a detached task's context
	// outlives the request that scheduled it. Rollback after Commit reports
	// sql.ErrTxDone and changes nothing.
	defer func() { _ = tx.Rollback() }()
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, actor: actor, tier: ds.actorTier(actor),
		principal: substrate.PrincipalFrom(ctx), now: nowUTC(), internal: internal,
	}
	if err := fn(t); err != nil {
		return err
	}
	if err := t.settleFold(); err != nil {
		return err
	}
	// After settleFold: the last entry's payload is final only once the
	// transaction's late effects have merged into it, and the checksum covers
	// the payload as stored.
	if err := t.settleChecksums(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if t.maxSeq > 0 {
		ds.watch.signal(t.maxSeq)
	}
	if t.changeSink != nil && len(t.entries) > 0 {
		*t.changeSink = append(*t.changeSink, t.entries...)
	}
	for _, fn := range t.afterCommit {
		fn()
	}
	return nil
}

// asActor runs fn with the transaction attributed to another actor, then puts
// the previous one back. One transaction can therefore hold entries from two
// hands where the act genuinely has two — creation is the case that needs it:
// the seed entries are the shipped tree's (`bundle:core`) and the auth
// material in the same commit is the substrate's. The tier
// follows the actor, since it is resolved from the actor's data.
func (t *txn) asActor(actor substrate.Actor, fn func() error) error {
	prevActor, prevTier := t.actor, t.tier
	t.actor, t.tier = actor, t.ds.actorTier(actor)
	defer func() { t.actor, t.tier = prevActor, prevTier }()
	return fn()
}

func (t *txn) query(sqlText string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(t.ctx, sqlText, args...)
}

func (t *txn) row(sqlText string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(t.ctx, sqlText, args...)
}

func (t *txn) exec(sqlText string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, sqlText, args...)
}

// --- watch ---

type broadcaster struct {
	mu     sync.Mutex
	subs   map[chan int64]struct{}
	closed bool
	last   int64
	timer  *time.Timer
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: map[chan int64]struct{}{}}
}

// signal coalesces bursts: the highest committed seq is delivered once per
// ~300ms window.
func (b *broadcaster) signal(seq int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if seq > b.last {
		b.last = seq
	}
	if b.timer != nil {
		return
	}
	b.timer = time.AfterFunc(300*time.Millisecond, b.flush)
}

func (b *broadcaster) flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timer = nil
	if b.closed {
		return
	}
	for ch := range b.subs {
		select {
		case ch <- b.last:
		default:
		}
	}
}

func (b *broadcaster) subscribe(ctx context.Context) <-chan int64 {
	ch := make(chan int64, 1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return ch
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}()
	return ch
}

func (b *broadcaster) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

func (ds *dataset) WatchSignal(ctx context.Context) <-chan int64 {
	return ds.watch.subscribe(ctx)
}

// --- small helpers ---

func jsonStrings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var vals []any
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func actorNamespace(a substrate.Actor) string { return string(a) }

// metaKeyAllowed enforces the label/annotation namespace rule: the owner may
// touch any key, every other actor only its own namespace.
func metaKeyAllowed(actor substrate.Actor, key string) error {
	if !vocabulary.ValidMetaKey(key) {
		return fmt.Errorf("%w: %q must be a namespaced key (\"<actor>/<name>\")", substrate.ErrValidation, key)
	}
	if substrate.HumanActors[actor] || actor == substrate.ActorSystem {
		return nil
	}
	if ns := vocabulary.MetaKeyNamespace(key); ns != actorNamespace(actor) {
		return fmt.Errorf("%w: actor %q may not write key %q", substrate.ErrForbidden, actor, key)
	}
	return nil
}

// inRawTx runs fn in a plain scoped transaction: no actor, no fold, no
// changelog entry, the shape a re-key or a maintenance read needs. It
// settles nothing on purpose: fn must not fold.
func (ds *dataset) inRawTx(ctx context.Context, fn func(*txn) error) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	t := &txn{ctx: ctx, ds: ds, tx: tx, now: nowUTC(), internal: true}
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	if len(t.folded) > 0 || len(t.pending) > 0 {
		_ = tx.Rollback()
		return errors.New("substrate/engine: a raw transaction folded or appended; it may not")
	}
	return tx.Commit()
}
