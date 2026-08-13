package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// System types: state that belongs to the substrate's own machinery. The
// schema kinds among them are REAL, writable rows — but only through the
// loader-as-admission path (schemawrite.go); the txn-level guard still keeps
// the raw mutations out.
const (
	kindKind = "core.substrate.reamde.dev/kind"
	// The rest of the schema meta-model: the authority's own header, its
	// traits, custom property types, mappings and functions, so every
	// declaration the console can render is addressable, not just the kinds.
	kindAuthority     = "core.substrate.reamde.dev/authority"
	kindTrait         = "core.substrate.reamde.dev/trait"
	kindPropertyType  = "core.substrate.reamde.dev/propertytype"
	kindRecordMapping = "core.substrate.reamde.dev/recordmapping"
	kindFunction      = "core.substrate.reamde.dev/function"
	kindAgent         = "core.substrate.reamde.dev/agent"
	kindBundle        = "core.substrate.reamde.dev/bundle"

	// kindRepository is the repository's own self-description: one record, id
	// `self`, saying which repository this is.
	kindRepository = "core.substrate.reamde.dev/repository"
	// kindToken and kindCredential (auth.go) are the two AUTH kinds: the
	// generic surface refuses writes to both (guardSystemKind) and only the
	// auth paths write them. Revoking a token is the one exception — an
	// ordinary record delete, so every revoke path is the same write.
	kindToken       = "core.substrate.reamde.dev/token"
	kindActor       = "core.substrate.reamde.dev/actor"
	kindRecordMerge = "core.substrate.reamde.dev/recordmerge"
	kindRecordSplit = "core.substrate.reamde.dev/recordsplit"

	annApplyConflict = "substrate/conflict"

	// finalizerMerge holds a merged-away record against GC so split stays
	// possible.
	finalizerMerge = "substrate.merge"
)

var systemKinds = map[string]bool{
	kindKind: true, kindAuthority: true, kindTrait: true,
	kindPropertyType: true, kindRecordMapping: true, kindFunction: true,
	kindAgent: true, kindBundle: true,
	kindRepository: true, kindToken: true, kindCredential: true, kindRecoveryKey: true, kindActor: true,
	kindRecordMerge: true, kindRecordSplit: true,
	// The blob manifest is byte-store-managed: the generic record
	// API may not forge one, only the byte-store path writes it.
	kindBlob: true,
}

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

	mu   sync.RWMutex
	reg  *vocabulary.Registry
	info substrate.RepositoryInfo

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

func (ds *dataset) KindByPlural(ctx context.Context, authority, plural string) (substrate.KindInfo, error) {
	t, ok := ds.registry().ByPlural(authority, plural)
	if !ok {
		return substrate.KindInfo{}, fmt.Errorf("%w: kind %s/%s", substrate.ErrNotFound, authority, plural)
	}
	return typeInfo(t), nil
}

func typeInfo(t *vocabulary.Kind) substrate.KindInfo {
	return substrate.KindInfo{
		Identity: t.Identity, Name: t.Name, Authority: t.Authority, Version: t.Version,
		Plural: t.Plural, Source: t.Source, Description: t.Description,
		Definition: t.Definition,
	}
}

func (ds *dataset) resolveType(name string) (*vocabulary.Kind, error) {
	t, err := ds.registry().Resolve(name)
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
	tier   substrate.Tier
	now    time.Time
	maxSeq int64
	// folded holds the fold effects this transaction has applied and not yet
	// written into an entry (fold.go). appendChange drains it, so every entry
	// carries the delta — with values — that a rebuild replays.
	folded []foldOp
	// seqLocked records that this transaction already holds the changelog's
	// ordering lock (taken once, on the first append).
	seqLocked bool
	// internal writes bypass the system-type guard.
	internal bool
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
	// this transaction applies — a function's `capabilities.emit`, or an
	// agent's declared emit intersected with its inherited ceiling (wave-3
	// #12). effEmitSet distinguishes an EMPTY set (the actor may emit nothing)
	// from an absent one (the generic write API, where no effect ceiling
	// applies). Carried so that ACCEPTING a change request authorizes the
	// TRANSITIVE write against the same ceiling the direct effect would face:
	// a function that can only emit the request type cannot become a confused
	// deputy for an arbitrary create/delete.
	effEmit    []string
	effEmitSet bool
}

func (ds *dataset) inTx(ctx context.Context, actor substrate.Actor, internal bool, fn func(*txn) error) error {
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	t := &txn{ctx: ctx, ds: ds, tx: tx, actor: actor, tier: ds.actorTier(actor), now: nowUTC(), internal: internal}
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := t.settleFold(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if t.maxSeq > 0 {
		ds.watch.signal(t.maxSeq)
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
