package engine

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/scope"
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
	// kindToken (auth.go) is the AUTH kind, as kindCredential
	// (identityboot.go) was until decision 0274: the generic surface refuses
	// writes to both (forbidSystemKind) and only the auth paths write a
	// token. Revoking a token is the one exception — an
	// ordinary record delete, so every revoke path is the same write. It is
	// the owner's alone: softDeleteIf refuses it below the owner tier.
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
	kindRepository: true, kindToken: true, kindCredential: true, kindActor: true, kindRole: true,
	// Declared, empty and written by nothing in a repository from before
	// decision 0264 (repomigration_0008_purge_the_recovery_key.go).
	kindRecoveryKey: true,
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
	// people are the identities that may open the repository, read off the
	// control plane at open (identities.go) and derived again from the
	// person actors (people.go settlePeople). Open-time only.
	people []string
	// beforeIdentities is whether the repository was from before identities
	// when this open began (people.go fromBeforeIdentities). Open-time only.
	beforeIdentities bool
	// firstPerson is the identity the first person's actor names, a string,
	// "" while it names none: what a person's write is told apart by
	// (personActorFor).
	firstPerson atomic.Value
	// dek is the repository's data-encryption key, unwrapped at open: every
	// sealed-store payload seals under it. The control plane holds it
	// wrapped under the host credential key, and nothing else holds it.
	dek   []byte
	watch *broadcaster
	// tail is the one reader of the changelog's tail the repository's
	// streams share (changetail.go).
	tail changeTail
	// actors is the actor records this process has read standing in the
	// repository (actors.go actorCache).
	actors actorCache
	// observedKinds is the kinds holding an observed segment (measured.go),
	// so a record of any other kind leaves the fold without a probe.
	observedKinds observedKinds
	// generation is the repository's history generation (repositories.go),
	// the marker every change cursor this dataset hands out is bound to. It
	// is read once at open: the row changes it only when a boot imports the
	// repository, and no dataset is open then.
	generation string

	// dir is the repository's directory under the data root, which holds
	// what Postgres does not: the blob bytes and the webhook spool
	// (repodir.go).
	dir string
	// gate orders the writers that go through inTx into the changelog lock,
	// an owner's write ahead of waiting background writers (writegate.go).
	// It is given back once the changelog lock is held, so it comes before
	// that lock in the lock order and is never held with it.
	gate writeGate
	// unsettled is the latched refusal after a commit that would have
	// published a registry answered failure: it may have committed, and this
	// process serves the declarations it replaced, so writes and the passes
	// that derive under the registry are refused until a restart loads what
	// Postgres holds (ErrRegistryUnsettled). It is set under mu, which that
	// commit holds from before its COMMIT, so a reader under mu sees the
	// registry published or the latch set, never between the two.
	unsettled atomic.Pointer[error]
	// testSealedFault, when set, refuses commitSealed's writes before they
	// commit: a test's credential store that cannot be written. Tests only.
	testSealedFault atomic.Pointer[func() error]

	// changelogStamped remembers that this repository's changelog dialect is
	// COMMITTED at this binary's maximum, so the stamp rides the first
	// appending transaction and no later one (changelogdialect.go). It is set
	// after that transaction commits, never before: a rolled-back stamp that
	// left this true would let a later append land with nothing claiming it.
	changelogStamped atomic.Bool
	// runningClaims holds the failure ids this process is running a delivery
	// under: a dispatch's claim from inside the transaction that writes it
	// until its completion or park ends, a retry's failure from before
	// anything runs until its retirement or re-park ends, and a webhook
	// fire's pending entry from before anything runs until its retirement or
	// park ends (functions.go acquireClaim, deliverFire). The insert is
	// compare-and-swap (sync.Map.LoadOrStore), so two hands on one failure
	// cannot both start a loop, and a resume racing the door's own spawn
	// cannot both start a fire: the second answers ErrConflict. A crash
	// empties it, and the claim it leaves behind is retryable.
	runningClaims sync.Map
	// runningThreads holds the llm/thread ids an agent loop of this process
	// runs now, each mapped to its loop, from the thread's opening until the
	// loop returns (agentloop.go runAgent). A `running` thread outside it
	// whose lease expired has no loop left to settle it (settleLostThreads).
	runningThreads sync.Map
	// lostRuns, under lostMu, maps a thread id to the reason its loop failed
	// with when even the settle after that failure failed, for
	// settleLostThreads (agents.go noteLostRun).
	lostMu   sync.Mutex
	lostRuns map[string]string
	// jobs is the background calls agent loops of this process started, by
	// thread (agentjobs.go).
	jobs agentJobs
	// chatTurns counts the detached chat turns this process runs for each
	// person of the repository, from admission to settle (agents.go
	// ChatAgent), against the service's chatTurnsPerPerson.
	chatTurns personSlots
	// firing counts the schedule occurrences this process is delivering now,
	// keyed by fireKey (functions.go startFire). A function fire writes no
	// row until it settles or parks, so without it a status read shows an
	// occurrence the pass is running as neither pending nor in flight.
	firingMu sync.Mutex
	firing   map[string]int
	// activity, under activityMu, is what this process saw of each
	// trigger's dispatch, by trigger id (functions.go triggerActivity):
	// when a pass last reached it, when a delivery of it last settled, and
	// the record deliveries of it running now. Memory only, so a restart
	// clears it and another process's passes never show in it.
	activityMu sync.Mutex
	activity   map[string]*triggerActivity
	// scheduleCursors, under scheduleCursorsMu, is each schedule trigger's
	// position in its RRULE, by trigger id (schedulecursor.go). Memory
	// only: the first walk after an open starts at the rule's anchor.
	scheduleCursorsMu sync.Mutex
	scheduleCursors   map[string]*scheduleCursor
	// resumingWebhooks is set while a dispatcher pass's resume of pending
	// webhook requests runs (webhooks.go resumeWebhooks), so passes that
	// come faster than a fire settles do not start a second walk.
	resumingWebhooks atomic.Bool
	// retryingTemporary is set while a dispatcher pass's walk of the
	// temporary failures that are due runs (temporaryretry.go).
	retryingTemporary atomic.Bool
	// webhookReplays is the door's memory of the signatures it admitted
	// (webhooksignature.go): memory only, so a restart clears it.
	webhookReplays webhookReplays
	// webhookSecretWarnings paces the door's warning for a signing secret
	// that does not resolve (webhooksignature.go).
	webhookSecretWarnings webhookSecretWarnings
	// webhookKeys holds each trigger's resolved signing key for a few
	// seconds (webhooksignature.go): memory only.
	webhookKeys webhookKeyCache
	// spool is the webhook requests the door answered and the drain has not
	// recorded yet (webhookspool.go).
	spool webhookSpool
	// interruptedRerun is where this open's one walk over the agent
	// deliveries the last stop interrupted stands (agents.go
	// rerunInterruptedAgentRuns): rerunNotStarted until a dispatcher pass
	// starts it, rerunWalking while it runs, rerunWalked once it returned.
	// Once per open, because only the open-time sweep parks a delivery as
	// interrupted at one attempt.
	interruptedRerun atomic.Int32
	// healthReadAt is when a pass last read the triggers' health, in Unix
	// nanoseconds on the health clock (health.go healthDue).
	healthReadAt atomic.Int64
	// statsCache holds the collection statistics the lexical arm's BM25F
	// reads (bm25.go), refreshed after statsTTL.
	statsCache searchStatsCache
	// spend is the spend cap's bookkeeping (spend.go): the cached read of
	// the window's settled spend and the repository cap, what running agent
	// chains have charged, and which triggers are held.
	spend spendLedger
	// unseeded, under unseededMu, is what the last unit seed skipped or
	// failed to write (unitseed.go), which GET /api/v1/units lists.
	unseededMu sync.Mutex
	unseeded   []unseededUnit

	mu  sync.RWMutex
	reg *vocabulary.Registry
	// parked, under mu, is what the loader left out of reg (fold.go
	// parkedSet): read by the fold's index derivations alone, and swapped
	// with reg wherever reg is.
	parked *parkedSet
	// info is the repository's id and state. It holds no name: the names
	// are the names cache's, asked by id when needed (namescache.go).
	info substrate.RepositoryInfo
	// creating is the name a repository being created is registered under,
	// set on the creation's dataset alone (createSeededRepository): no row
	// holds it yet, so the cache cannot answer it, and nothing can rename it.
	creating string
	// beforePublish and beforeSignal, under mu, are set only by tests.
	// beforePublish runs inside commitAndPublish, after the commit and before
	// the swap, with mu held exclusively; beforeSignal runs after the publish
	// and before the head signal. The two orders they pin ("committed, then
	// published, then signaled") are observable nowhere else without a race.
	beforePublish func(t *txn)
	beforeSignal  func(t *txn)
	// deliveryFault, under mu, is set only by tests: it runs at the start of
	// a dispatched delivery's settlement (functions.go dispatchSettlement),
	// after the effects applied and before the cursor moves, so a test can
	// fail the write there and show that neither committed.
	deliveryFault func(t *txn) error
	// wrapLLMClient, under mu, is set only by tests: every agent loop's
	// client passes through it before its first completion (agentloop.go
	// runAgent). The engine builds its clients from provider rows, so this is
	// the one place the live chain can meter a completion before it is sent.
	wrapLLMClient func(wire llm.Wire, c llm.Client) llm.Client
	// beforeProviderRead, under mu, is set only by tests: it runs in
	// resolveProvider before the llm/provider row is read, so a test can
	// commit a new key between a call's start and the read that pays for it.
	beforeProviderRead func(id string)
	// afterPinnedRead is set only by tests, before any loop runs: a hook a
	// new snapshot passes between its pinned reads and its capture, so a
	// test can change a role there.
	afterPinnedRead func()
	// beforeJob is set only by tests, before any loop runs: a hook a
	// background job passes before it reads whose rules hold it, so a test
	// can hold it while another turn claims its thread.
	beforeJob func()
	// beforeTitle is set only by tests, before any loop runs: a hook the
	// title's background task passes before it does anything, so a test can
	// hold it while another turn claims the thread.
	beforeTitle func()
	// skipThreadTitles, under mu, is set only by tests: it keeps a chat's
	// first turn from spending a scripted completion on its title
	// (agenttitle.go), which suites that script a model's queue turn by turn
	// would otherwise lose to a background call.
	skipThreadTitles bool
	// agentWriteTimeout and agentRowWrite are set only by tests, before any
	// loop runs: the budget of a detached chat turn's own writes (zero is
	// agentSettleTimeout), and a hook every agent message row passes
	// through before it is written, so a test can hold one.
	agentWriteTimeout time.Duration
	agentRowWrite     func(ctx context.Context, props map[string]any) error
	// failedSettleTimeout is set only by tests: the budget of the settle
	// after a failed write (zero is agentFailedSettleTimeout).
	failedSettleTimeout time.Duration
	// exporting is the repository's one export slot (export.go): set by
	// Export when it pins a point, cleared when WriteTo returns, so a second
	// export is refused while one streams.
	exporting atomic.Bool
	// exportHeld, under exportHeldMu, is the digests the streaming export
	// listed, set by Export with the slot and cleared when WriteTo returns.
	// deleteOrphanBytes leaves a held digest's bytes in the store: the
	// archive's changelog ends before the collection that tombstoned the
	// manifest, so the restore needs the bytes the stream has not reached
	// yet (#1022). The orphan sweep deletes them once the hold is gone.
	exportHeldMu  sync.Mutex
	exportHeld    map[string]bool
	exportHoldAll bool
	// archiveHeld and archiveHoldAll are the archive pass's hold, the same
	// rule under the same mutex (archive.go).
	archiveHeld    map[string]bool
	archiveHoldAll bool

	// blobSweepAfter is the blob orphan sweep's cursor: the last digest the
	// previous pass looked at, so a store with more objects than one batch is
	// walked whole instead of the sweep restarting at the front every time.
	// Empty starts over, which is also where a restart begins.
	blobSweepAfter string

	// vocabularyWriteMu serializes SCHEMA writes (the ACCESS EXCLUSIVE analog,
	// scoped to schema): two concurrent schema writes must not both validate
	// against the same base and both commit. Data writes never take it.
	vocabularyWriteMu sync.Mutex

	// lifecycleFence is the ONE dataset-wide lifecycle fence (bundles.go).
	// An invocation TREE — a trigger delivery, a direct
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
	// unregisterMetrics drops this pool's sql.DBStats collector, the
	// webhook spool's gauges and the usage inventory on close; nil on a
	// dataset that never registered them (creation's seed dataset).
	unregisterMetrics func()
	// usage is the repository's last inventory reading for the usage gauges
	// (usagemetrics.go).
	usage usageInventory

	// reindexFrom is the search index version the open found below this
	// binary's (searchindex.go checkSearchIndex); zero when the index is
	// current or the process is read-only. reindex is the pass that brings
	// it up to date behind the open.
	reindexFrom int
	reindex     backgroundPass

	// reprojectPending is whether index re-derivations were owed at the open
	// (reprojection.go checkIndexReprojections) or requested since by a
	// vocabulary apply (kickIndexReprojection): the kinds a declaration
	// change reshaped and left for the pass behind the open or the commit,
	// which is reproject. An apply sets it while a rebuild may read it.
	reprojectPending atomic.Bool
	reproject        backgroundPass
	// archive is the repository's archive as this process writes it, nil in
	// a process that writes none (archive.go); archiveStop ends its passes.
	archive     *archiveState
	archiveStop func()
}

func (ds *dataset) close() {
	// Before the pool closes: the reindex and the reprojection write
	// through it.
	ds.stopSearchReindex(true)
	ds.stopIndexReprojection(true)
	ds.stopArchiver()
	ds.watch.close()
	if ds.unregisterMetrics != nil {
		ds.unregisterMetrics()
	}
	_ = ds.db.Close()
}

func (ds *dataset) Repository() substrate.RepositoryInfo {
	ds.mu.RLock()
	info := ds.info
	ds.mu.RUnlock()
	if names := ds.answersTo(); len(names) > 0 {
		info.Authority, info.FormerNames = names[0], names[1:]
	}
	info.Spellings = ds.names()
	return info
}

// Head is the changelog's highest committed seq and the history generation it
// belongs to: the pair a change cursor is held to before it resumes.
func (ds *dataset) Head(ctx context.Context) (substrate.ChangelogHead, error) {
	seq, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return substrate.ChangelogHead{}, err
	}
	return substrate.ChangelogHead{Seq: seq, Generation: ds.historyGeneration()}, nil
}

func (ds *dataset) registry() *vocabulary.Registry {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.reg
}

// --- type registry ---

func (ds *dataset) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	reg := ds.registry()
	types := reg.Kinds()
	out := make([]substrate.KindInfo, 0, len(types))
	for _, t := range types {
		out = append(out, typeInfoIn(reg, t))
	}
	return out, nil
}

func (ds *dataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	reg := ds.registry()
	if t, ok := reg.ByIdentity(ref); ok {
		return typeInfoIn(reg, t), nil
	}
	if !vocabulary.Qualified(ref) {
		// A bare name is a malformed reference, not a missing kind: the
		// refusal is the registry's, naming every identity carrying the word,
		// so the caller learns the spelling instead of a 404 (record 0101).
		_, err := reg.Resolve(ref)
		return substrate.KindInfo{}, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	return substrate.KindInfo{}, fmt.Errorf("%w: kind %q", substrate.ErrNotFound, ref)
}

// typeInfo is one declared kind as the read surfaces see it.
//
// `Definition` is RENDERED FROM THE PARSED DECLARATION — the data map the loader
// left behind, which is the same map the projection writes as the row's
// properties (vocabularywrite.go packageDeclarations). No row carries a
// `definition` property to read it out of.
//
// It is the AUTHORED half of that map: the engine's own `version` is dropped,
// because a declaration that pinned none has one stamped onto its row, and after
// a reload the map would otherwise hand back a value nobody wrote — the same
// declaration reading differently before and after a restart, which is exactly
// what a client diffing it (or fingerprinting it) would trip
// over. The stamped value is `Version` beside it, which is where a reader that
// wants it already looks. Nothing else on a kind is engine-owned: `source` is
// not a document key at all.
func typeInfo(t *vocabulary.Kind) substrate.KindInfo {
	authority, pkg := vocabulary.SplitPackageRef(t.Package)
	info := substrate.KindInfo{
		Identity: t.Identity, Name: t.Name,
		Authority: authority, Package: pkg, Version: t.Version,
		Source: t.Source, Description: t.Description,
		Definition: withMappedProperties(t, authoredKindData(t.Definition)),
	}
	if !t.Label.Empty() {
		info.Label = &substrate.KindLabel{Singular: t.Label.Singular, Plural: t.Label.Plural}
	}
	return info
}

// typeInfoIn is typeInfo with the kind's control class under reg's bundles.
func typeInfoIn(reg *vocabulary.Registry, t *vocabulary.Kind) substrate.KindInfo {
	info := typeInfo(t)
	switch {
	case vocabulary.SignInMaterial(t.Identity):
		info.Control = vocabulary.ControlSignIn
	default:
		info.Control = string(controlClassIn(reg, t))
	}
	return info
}

// withMappedProperties merges a kind's MAPPING-OWNED properties into the
// declaration a read surface is handed (record 96). The document declares no
// slot — the mapping synthesizes it — so a client reading a kind's properties
// would otherwise be told a mirror has no link at all, and could neither
// render the value it serves nor say who manages it.
//
// READ-SIDE ONLY, and a copy: the map is the REGISTRY's, the stored row is
// written from the same map, and a declaration a document never carried must
// not be written back into one. Each entry carries `managed: true` and
// `mappedBy`, so a client renders it read-only and can name the mapping.
func withMappedProperties(t *vocabulary.Kind, data map[string]any) map[string]any {
	mapped := t.MappedProperties()
	if len(mapped) == 0 {
		return data
	}
	out := make(map[string]any, len(data)+1)
	for k, v := range data {
		out[k] = v
	}
	props := map[string]any{}
	if declared, ok := data["properties"].(map[string]any); ok {
		for k, v := range declared {
			props[k] = v
		}
	}
	for name, p := range mapped {
		props[name] = p.SynthesizedDeclaration()
	}
	out["properties"] = props
	return out
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

// resolveKindIn resolves a kind reference against a GIVEN registry: the
// dataset's live one outside a transaction (resolveType above), the
// transaction's declarations inside one (txn.resolveType), and the candidate
// for a declaration row whose stored declaration the projection is deciding
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
// in auth.go. There is no actor set on a token: a token without scopes has
// full access to its repository, and an app's token is held to its scopes at
// the API, with the gates an app's token alone meets here reading
// txn.scoped.

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
	ctx context.Context
	ds  *dataset
	tx  *sql.Tx
	// idAdmitted is set while an accepted create request mints its target:
	// the target id was judged against the proposer when the request was
	// written (admitRequestDiff), so checkCreateID does not judge it again
	// against the accepting owner.
	idAdmitted bool
	// effectBy is the function whose effect the transaction is applying
	// (applyEffects), so a refusal can name the body that has to change
	// rather than the actor it writes as.
	effectBy *vocabulary.Function
	// wc is the context tx was begun under (beginWrite): the request's until
	// commit starts, the commit budget's from then on. Nil on a
	// transaction that commits some other way.
	wc *writeCtx
	// tokensMoved is set when the transaction folded a token record, so the
	// token cache fences its repository across the COMMIT (tokencache.go).
	tokensMoved bool
	// commitSent is set once the COMMIT is asked of Postgres, and committed
	// once Postgres answered it: sent and not committed is a commit in doubt.
	commitSent, committed bool
	actor                 substrate.Actor
	// ensuringActor is set while ensureLinks writes an actor record, so the
	// record's own entry does not ask for it again.
	ensuringActor bool
	// actorsStanding is the id of every actor record this transaction read
	// standing or wrote; they reach the dataset's cache only once it commits
	// (actors.go actorLive), so a rollback leaves no record cached that the
	// store does not hold. actorEpoch is the cache's invalidation count when
	// the transaction began, before it read any actor record, and
	// actorsDisabled the records it disabled, which it invalidates again once
	// its commit is visible.
	actorsStanding map[string]bool
	actorEpoch     uint64
	actorsDisabled []string
	// onBehalfOf is the actor the request this transaction serves acts for
	// (substrate.OnBehalfOfFrom): the person, behind installed code a
	// person's request started (linksOf). Empty on the trigger pass's own
	// context.
	onBehalfOf substrate.Actor
	// principal is the token id the door verified for the request this
	// transaction serves (substrate.PrincipalFrom), stamped on every
	// changelog entry it appends as its token. Empty when
	// no token stands behind the write — the seed, the boot upgrade, a
	// background worker, registration and login — and never taken from the
	// caller: the actor is asserted, the principal is resolved. It follows
	// the CONTEXT and nothing else, so a dispatch that runs on the request's
	// own context (an agent chat, a synchronous call) carries the token that
	// caused it, while one the trigger pass fires later runs on the server's
	// ticker context (ProcessTriggers) and carries none. The changelog's
	// caused_by is what links that entry back to the write behind it.
	principal string
	// scoped is true when an app's token stands behind the request
	// (substrate.ScopesFrom): the gates an app's token alone meets read it.
	scoped bool
	// decides is the change request an app's token may decide under a
	// `decide:` scope (substrate.DecisionFrom), "" otherwise; deciding is
	// set once the decided transition is admitted, for the accept's own
	// materialization (decision 0195).
	decides  string
	deciding bool
	now      time.Time
	maxSeq   int64
	// bulk queues the effects and entries of a bulk run of creating puts
	// (bulkwrite.go), nil outside one.
	bulk *bulkWrite
	// entries records every changelog row this transaction appended, in
	// order: the (seq, op, kind, id) address, never the payload. Slices of it
	// stamp llm/message rows with what a dispatch wrote (`changes`), so a
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
	// publishReg is the registry this transaction's commit activates: the
	// vocabulary apply's candidate (vocabularywrite.go). commitAndPublish
	// swaps it in under ds.mu held from before the commit, so a writer the
	// commit wakes and a watcher the head signal wakes both read it.
	publishReg *vocabulary.Registry
	// writeParked is the parked set this transaction's fold derives under
	// when it is not the dataset's (fold.go parkedSet): read beside the
	// candidate a vocabulary apply publishes, less every package the apply
	// touches, which the commit activates with publishReg, or beside the
	// candidate the boot upgrade reloads. Nil otherwise.
	writeParked *parkedSet
	// interactionThread marks the agent loop's own ask dispatch: the ONE
	// writer allowed to stamp an interaction's thread reference
	// (interactions.go admitInteraction).
	interactionThread bool
	// approvalLoop marks the agent loop's own landing of an approval, the one
	// writer that creates one; approvalRun marks the engine's run of an
	// approved call, the one writer of its run properties and of the
	// running, done and failed states (approvals.go).
	approvalLoop bool
	approvalRun  bool
	// migrating marks a repository migration's transaction (repomigrate.go):
	// it folds before the stored vocabulary loads, so a row whose kind the
	// registry does not yet hold keeps its derived indexes as they stand,
	// and unprojected names its kind for the pass behind the open.
	migrating   bool
	unprojected map[string]bool
	// movingRecords marks a kind move carrying a record from one kind to
	// another (move.go, record 0078). The row was admitted once already, under
	// the old kind, so the CREATION-only contracts (an interaction's batch, a
	// change request's reviewed envelope) must not judge it a second time: it
	// is the same record arriving, not a new one being authored.
	movingRecords bool
	// policyDecision marks the ENGINE's own judge-driven decision on a
	// policy-gated request: the one bundle-tier hand the gated
	// guard admits.
	policyDecision bool
	// folded holds the fold effects this transaction has applied and not yet
	// written into an entry (fold.go). appendChange drains it, so every entry
	// carries the delta — with values — that a rebuild replays.
	folded []foldOp
	// takenOut holds, for the line being folded, each property a record
	// effect took out of a row that held it, and whether the row holds it
	// again (fold.go settleTakenOut).
	takenOut map[eref]map[string]bool
	// seqLocked records that this transaction already holds the changelog's
	// ordering lock (taken once, on the first append).
	seqLocked bool
	// keptCursors marks a replay that copied the resume cursors paged_cursors
	// held into pg_temp.substrate_kept_cursors before it cleared the table
	// (rebuild.go keepPagedCursors): a page entry that names its cursor by
	// hash is folded back with the kept cursor whose digest it names.
	keptCursors bool
	// internal writes bypass the system-type guard.
	internal bool
	// writeReg is the registry this transaction's writes are held to when it
	// is not the live one: a vocabulary apply sets its candidate for the whole
	// transaction (vocabularywrite.go), and the boot upgrade sets it for its
	// projection. Read through declarations(), never directly. The fold
	// consults the registry for exactly one thing, the weighted search bands,
	// and computing those from a declaration OTHER than the one the row was
	// validated against is what made a live row and its own replay disagree:
	// a replay reads the registry the rebuild holds, which is the declaration
	// the row ended up under.
	writeReg *vocabulary.Registry
	// recomputing marks a mapping recompute's own write: recompute never
	// triggers recompute, and each property it writes is marked with the
	// source records behind the value — recomputeSources, per property —
	// and pinned by nothing (decision 0284).
	recomputeInitiator substrate.Actor
	recomputing        bool
	recomputeSources   map[string][]string
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
	// roles are the roles each person this transaction's check read holds
	// (roles.go), read once per transaction.
	roles map[substrate.Actor]scope.Roles
	// exempt is the exemption this transaction writes under (run.go), ""
	// for a write held as any is.
	exempt exemptOp
	// claiming is set by a turn's claim of its thread (agentloop.go
	// claimThread): the one write that makes the thread's `heldTo` the
	// turn's (heldto.go stampHeldTo). A hand's write of the lease does not.
	claiming bool
	// report is the holders of the item a report row reports, set while
	// that row is written (agentdecision.go putThreadSystemRowID): the row
	// carries them and never the writer's (heldto.go stampHeldTo).
	report *reportHolders
	// observed is what this transaction's observed point writes touched,
	// recomputed once at its end (observedsettle.go settleObserved).
	observed *observedTouch
	// unitTouched marks a transaction that folded a unit or conversion row
	// (unitgraph.go): unitBefore is the graph it found, taken at the first
	// such fold, and unitLocal the graph built from its own writes, nil
	// until its next read after each fold.
	unitTouched bool
	unitBefore  *unitGraphAt
	unitLocal   *unitGraphAt
	// metricValues and metricRecords are the observed values and the metrics
	// the transaction folded, which its end holds
	// to the metric rules (metricrules.go checkMetricValues).
	metricValues  map[eref]metricValueNote
	metricRecords map[eref]bool
	// replaying marks a rebuild's or an import's replay of the changelog
	// (rebuild.go), which re-derives the fold and judges nothing.
	replaying bool
	// heldRegistryDep records that this transaction already took the SHARED
	// registry-dependency lock, so the several doors that need it (the write's
	// entry, preRecordLocks, trigger and policy admission) can each ask without
	// a round trip per ask. An advisory lock is transaction-scoped and released
	// at commit, so "taken once" is "held to the end".
	heldRegistryDep bool
}

func (ds *dataset) inTx(ctx context.Context, actor substrate.Actor, internal bool, fn func(*txn) error) error {
	return ds.inTxOn(ctx, ds.db, actor, internal, fn)
}

// inTxOn is inTx with its transaction begun on db: the dataset's handle on
// the shared pool, or a connection dialed outside it
// (runRepositoryMigrations).
func (ds *dataset) inTxOn(ctx context.Context, db *sql.DB, actor substrate.Actor, internal bool, fn func(*txn) error) error {
	// A read-only process is not the directory's writer, so it appends
	// nothing to the tables either: a row it committed would be one the file
	// never receives until the server's next boot (repodir.go).
	if ds.svc.readOnly {
		return ErrDirectoryReadOnly
	}
	// A process whose writer lease dropped may no longer be this
	// repository's writer, so it writes nothing until the lease is retaken
	// (writerlease.go). One atomic read, no round trip.
	if err := ds.svc.lease.err(); err != nil {
		return err
	}
	// A vocabulary commit whose answer was lost refuses every write until a
	// restart loads the declarations Postgres holds (ErrRegistryUnsettled).
	if err := ds.unsettledErr(); err != nil {
		return err
	}
	// A person's write links their own actor (people.go).
	actor = ds.personActorFor(ctx, actor)
	// The write gate before the pool connection (writegate.go): a background
	// writer waits there holding nothing while a person's write is on its way
	// to the changelog lock, and one background writer at a time goes on to
	// it. The pass is given back once the lock is held, and on every exit
	// before that.
	pass, err := ds.gate.enter(ctx, ds.personFirst(ctx, actor, internal))
	if err != nil {
		return err
	}
	defer pass()
	tx, wc, err := beginWrite(ctx, db)
	if err != nil {
		return err
	}
	defer wc.release()
	// ONE rollback covers every exit, the PANIC included: a panic that unwound
	// past here would leave the transaction open and its connection held out of
	// an 8-connection pool until the context died, and a detached task's context
	// outlives the request that scheduled it. Rollback after Commit reports
	// sql.ErrTxDone and changes nothing. It runs before wc.release, so the
	// transaction ends here and not in database/sql's answer to the cancel.
	defer func() { _ = tx.Rollback() }()
	_, scoped := substrate.ScopesFrom(ctx)
	t := &txn{
		ctx: ctx, ds: ds, tx: tx, wc: wc, actor: actor,
		principal: substrate.PrincipalFrom(ctx), onBehalfOf: substrate.OnBehalfOfFrom(ctx),
		scoped: scoped, now: nowUTC(), internal: internal,
		decides: substrate.DecisionFrom(ctx), actorEpoch: ds.actors.now(),
	}
	// The changelog lock before anything else the transaction locks: the
	// first key of the global order (rows.go changelogLockKey). The cost is
	// plain: every write of a repository now serializes from its start, so
	// its whole pre-append phase (loads, validation, a recompute) runs one
	// writer at a time, where before only the stretch from the first append to
	// the commit did. What it buys is that no writer ever holds a row, or a
	// record lock, while waiting for the changelog that another writer holds
	// while waiting for that row. A transaction that appends nothing and must
	// not wait here uses inRawTx.
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	pass()
	t.seqLocked = true
	if err := fn(t); err != nil {
		return err
	}
	// The unit graph the transaction leaves, against the one it found
	// (unitgraph.go), before anything reads the graph to settle.
	if err := t.checkUnitGraph(); err != nil {
		return err
	}
	// The quantity declarations and the metric rules over the transaction's
	// final state (quantitywrite.go, metricrules.go).
	if err := t.checkDeclaredUnits(); err != nil {
		return err
	}
	if err := t.checkMetricValues(); err != nil {
		return err
	}
	// The observed index's rollups and latest rows, from the points the
	// transaction's folds left (observedindex.go).
	if err := t.settleObserved(); err != nil {
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
	// A transaction that moved a token fences its repository's tokens in the
	// cache before its COMMIT, and the fence stands until the outcome is
	// proved (tokencache.go): a lookup in between reads the table.
	repository := ds.scope.Repository
	if t.tokensMoved {
		ds.svc.tokens.fence(repository)
	}
	err = ds.commit(tx, t)
	if errors.Is(err, substrate.ErrWriteInDoubt) {
		ds.commitDoubted()
	}
	if err == nil {
		ds.settleActorCache(t)
	}
	if t.tokensMoved {
		switch {
		case t.committed:
			ds.svc.tokens.unfence(repository)
		case !t.commitSent:
			// No COMMIT was sent: the rollback is the outcome, and it runs
			// before the fence lifts.
			_ = tx.Rollback()
			ds.svc.tokens.unfence(repository)
		default:
			ds.svc.settleTokens(repository)
		}
	}
	if err != nil {
		return err
	}
	ds.mu.RLock()
	beforeSignal := ds.beforeSignal
	ds.mu.RUnlock()
	if beforeSignal != nil {
		beforeSignal(t)
	}
	if t.maxSeq > 0 {
		ds.watch.signal(t.maxSeq)
	}
	if t.changeSink != nil && len(t.entries) > 0 {
		*t.changeSink = append(*t.changeSink, t.entries...)
	}
	// A replay re-derives history that was counted when it was written.
	if !t.replaying {
		ds.countChanges(t.pending)
	}
	for _, fn := range t.afterCommit {
		fn()
	}
	return nil
}

// commit commits the transaction: Postgres alone confirms a write (decision
// 0262), and the archive copies it after the fact (archive.go).
//
// The request's context cancels nothing from the top of this function on
// (beginWrite): a client that disconnects while Postgres commits would
// otherwise have the driver abandon a COMMIT the server may already have
// applied. A commit that reports failure may have committed all the same (a
// connection lost at the answer, the commit budget spent); the next read
// finds out which. One that would have published a registry latches the
// dataset (ErrRegistryUnsettled), because the registry this process serves
// may be behind the declarations the table holds, and only the open loads
// them again.
func (ds *dataset) commit(tx *sql.Tx, t *txn) error {
	if err := t.wc.detach(); err != nil {
		return err
	}
	// The seam is where a test's process dies before the commit: the
	// deferred rollback in inTx is what a crash does to the transaction.
	if err := ds.svc.commitFault(commitBeforeCommit); err != nil {
		return err
	}
	// THE LEASE AGAIN, and the latch. The checks at the write's door (inTx,
	// inRawTx) are as old as the write: this transaction may have waited for
	// a pool connection and run a body of any length, and the heartbeat may
	// have latched the refusal at any point in that. Atomic loads,
	// immediately before the commit.
	if err := ds.svc.lease.err(); err != nil {
		return err
	}
	if err := ds.unsettledErr(); err != nil {
		return err
	}
	// Postgres alone confirms the write, so no work follows the COMMIT for
	// the token cache's fence to cover (tokencache.go): the write is in doubt
	// exactly when its COMMIT was sent and no answer came (the connection
	// lost at the answer, the commit budget spent, or the seam that stands
	// for either), and only the database proves which way it went
	// (settleTokens).
	t.commitSent = true
	err := ds.commitAndPublish(tx, t)
	if err == nil {
		// The seam for a commit whose answer was lost after Postgres
		// committed: the error path below runs as it would for one that
		// rolled back.
		err = ds.svc.commitFault(commitInDoubt)
	}
	t.committed = err == nil
	if err != nil {
		return inDoubt(err)
	}
	return inDoubt(ds.svc.commitFault(commitAfterCommit))
}

// inDoubt marks an error a write answers once its COMMIT was sent, in the
// commit phase or in a read after it (getAfterCommit): the write may have
// landed, so its client is told it cannot conclude the write failed
// (substrate.ErrWriteInDoubt, decision 0271). Every error before the COMMIT
// rolled the write back and is not marked.
func inDoubt(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", substrate.ErrWriteInDoubt, err)
}

// ErrRegistryUnsettled is the refusal every write meets after a commit that
// would have published a registry answered failure: it may have committed,
// and the registry this process serves may be behind the declarations the
// table holds. It is an ErrRestartRequired, so the wire answers 503 and
// names the restart.
var ErrRegistryUnsettled = fmt.Errorf("%w: a vocabulary change's commit answered failure and may have committed; restart the server so the repository serves the declarations Postgres holds", substrate.ErrRestartRequired)

// unsettledErr is the latched refusal (dataset.unsettled), nil when none.
func (ds *dataset) unsettledErr() error {
	if err := ds.unsettled.Load(); err != nil {
		return *err
	}
	return nil
}

// commitAndPublish commits the transaction and, when it carries a registry to
// publish, swaps the pointer under ds.mu held from BEFORE the commit. The
// registry-dependency advisory lock a parked data write waits on releases at
// the commit, and that write's next act is ds.registry(), which takes ds.mu
// shared: with the mutex held across the commit and the swap, the write reads
// the published declaration and not the one the commit retired. A commit that
// fails publishes nothing and latches the dataset (ErrRegistryUnsettled).
func (ds *dataset) commitAndPublish(tx *sql.Tx, t *txn) error {
	if t.publishReg == nil {
		return ds.svc.commitTx(tx)
	}
	ds.mu.Lock()
	defer ds.mu.Unlock()
	err := ds.svc.commitTx(tx)
	if err == nil {
		// The seam for a commit whose answer was lost after Postgres
		// committed, with the registry not yet published.
		err = ds.svc.commitFault(commitUnpublished)
	}
	if err != nil {
		latched := fmt.Errorf("%w: %w", ErrRegistryUnsettled, err)
		ds.unsettled.Store(&latched)
		return latched
	}
	if ds.beforePublish != nil {
		ds.beforePublish(t)
	}
	ds.reg = t.publishReg
	if t.writeParked != nil {
		ds.parked = t.writeParked
	}
	return nil
}

// commitBudget bounds what a write's commit phase asks of Postgres, from the
// top of commit on, where the request's context no longer reaches
// (beginWrite). When it runs out the transaction's own context is canceled:
// a commit not yet sent rolls back, and one in flight is in doubt. A commit
// phase takes milliseconds; the budget is for a database that stopped
// answering.
const commitBudget = 30 * time.Second

// writeCtx is the context a write's transaction is begun under: canceled
// with the request's until the transaction is ready to commit, and from then
// on by the commit budget alone. database/sql rolls a transaction back when
// the context it was begun under ends, and pgx runs the COMMIT under that
// same context, so a request that ended mid-commit had the driver abandon a
// COMMIT Postgres may already have applied. The directory then cut the
// transaction's lines as if it rolled back (#516).
type writeCtx struct {
	req    context.Context
	ctx    context.Context
	cancel context.CancelFunc
	// stop disarms the forwarding of the request's end to ctx; it reports
	// false once that forwarding has run.
	stop  func() bool
	timer *time.Timer
}

// beginWrite begins a transaction on db for a write that commits through
// commit. The caller defers release, AFTER its deferred rollback
// is registered so the rollback runs first. Statements take the request's
// own context as before, so a request that ends during the body still stops
// it.
func beginWrite(ctx context.Context, db *sql.DB) (*sql.Tx, *writeCtx, error) {
	wc := newWriteCtx(ctx)
	tx, err := db.BeginTx(wc.ctx, nil)
	if err != nil {
		wc.release()
		return nil, nil, err
	}
	return tx, wc, nil
}

// newWriteCtx is beginWrite's context, before any transaction is begun under
// it.
func newWriteCtx(ctx context.Context) *writeCtx {
	txCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	wc := &writeCtx{req: ctx, ctx: txCtx, cancel: cancel}
	wc.stop = context.AfterFunc(ctx, cancel)
	return wc
}

// detach starts the commit phase: the request's end no longer reaches the
// transaction, and the commit budget bounds what is left. A request that
// ended before this point gets its error back with nothing prepared and
// nothing in doubt, and the rollback ends the transaction. The request's
// error is read before the forwarding is disarmed, because a cancel sets it
// before the forwarding runs and stop would otherwise win that race.
func (wc *writeCtx) detach() error {
	if wc == nil {
		return nil
	}
	if err := wc.req.Err(); err != nil {
		return err
	}
	if !wc.stop() {
		return wc.req.Err()
	}
	wc.timer = time.AfterFunc(commitBudget, wc.cancel)
	return nil
}

// release ends the transaction's context once the write is over.
func (wc *writeCtx) release() {
	wc.stop()
	if wc.timer != nil {
		wc.timer.Stop()
	}
	wc.cancel()
}

// asActor runs fn with the transaction attributed to another actor, then puts
// the previous one back. One transaction can therefore hold entries from two
// hands where the act genuinely has two — creation is the case that needs it:
// the seed entries are the shipped tree's (`bundle:core`) and the auth
// material in the same commit is the substrate's. The tier
// follows the actor, since it is resolved from the actor's data.
func (t *txn) asActor(actor substrate.Actor, fn func() error) error {
	prevActor := t.actor
	t.actor = actor
	defer func() { t.actor = prevActor }()
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

// commitDoubted follows a commit whose answer was lost: it may have landed,
// and it signaled nothing. The streams' window opens again at the table's
// head (changetail.go), and the streams wake to read it.
// commitTx sends a transaction's COMMIT, through the test seam that may
// answer in its place.
func (s *service) commitTx(tx *sql.Tx) error {
	if s.testCommitTx != nil {
		return s.testCommitTx(tx.Commit)
	}
	return tx.Commit()
}

// lockTokens takes, in a control-plane transaction that ends or changes the
// tokens of a repository (or who they act for), that repository's changelog
// lock, which every transaction moving a token holds: a COMMIT whose answer
// is lost is proved ended by its release (settleTokens). A transaction that
// touches several takes them in the order of their ids.
func lockTokens(ctx context.Context, tx *sql.Tx, repository string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`,
		Scope{Repository: repository}.lockKey(changelogLockKey))
	return err
}

// commitFencingTokens commits a control-plane transaction under the token
// cache's rule (tokencache.go): each repository whose tokens it ends or
// changes is fenced before the COMMIT and stays fenced until the outcome is
// proved, at once when the COMMIT answers, by settleTokens when it does not.
// The transaction holds each repository's lock (lockTokens).
func (s *service) commitFencingTokens(tx *sql.Tx, repositories ...string) error {
	for _, r := range repositories {
		s.tokens.fence(r)
	}
	err := s.commitTx(tx)
	for _, r := range repositories {
		if err != nil {
			s.settleTokens(r)
		} else {
			s.tokens.unfence(r)
		}
	}
	return err
}

// tokenProofAttempt bounds one wait for a doubted commit's changelog lock; a
// wait that runs out proves nothing, and the next attempt follows.
const tokenProofAttempt = time.Minute

// tokenProofRetryMax caps the pause between two failed attempts.
const tokenProofRetryMax = 5 * time.Second

// tokenProofWaits bounds the proofs that hold a connection of the
// maintenance pool at once, across repositories: the pool authenticates
// every request too. A proof waiting for a slot drops nothing; its fences
// stand until it gets one.
const tokenProofWaits = 2

// tokenProofs coalesces the proofs the token cache waits on: one waiter per
// repository, however many doubted commits fenced it.
type tokenProofs struct {
	mu sync.Mutex
	// pending counts, per repository, the fences awaiting proof; running
	// says whether its waiter runs.
	pending map[string]int
	running map[string]bool
	// slots bounds the waits held at once (tokenProofWaits).
	slots chan struct{}
}

// settleTokens lifts the repository's fence that a COMMIT whose answer was
// lost put up, once the database proves that transaction ended: every
// transaction that moves a token holds the repository's changelog lock until
// it ends (inTx, and the erase's own), so the lock granted to the waiter is
// that proof, as the tail's fenced head read relies on (changetail.go
// tailHead). The repository has one waiter (proveTokens), which this hands
// the fence to, or starts. A server shutting down leaves the fence standing.
func (s *service) settleTokens(repository string) {
	p := &s.proofs
	p.mu.Lock()
	if p.pending == nil {
		p.pending, p.running = map[string]int{}, map[string]bool{}
	}
	p.pending[repository]++
	if p.running[repository] {
		p.mu.Unlock()
		return
	}
	p.running[repository] = true
	p.mu.Unlock()
	s.spawn("prove a token commit ended", repository, func(ctx context.Context) {
		s.proveTokens(ctx, repository)
	})
}

// proveTokens is a repository's one waiter. An attempt proves the fences
// registered before it began: each doubted transaction sent its COMMIT, the
// changelog lock held, before its fence was handed here, so a lock asked for
// after that is granted after the transaction ended. Fences handed over
// while an attempt waits wait for the next one. A failed attempt (an error,
// a wait that ran out) proves nothing: it tries again, for as long as it
// takes, and the fences stand meanwhile; the wait is on the maintenance
// pool, so it outlives the repository's own.
func (s *service) proveTokens(ctx context.Context, repository string) {
	p := &s.proofs
	key := Scope{Repository: repository}.lockKey(changelogLockKey)
	pause := 50 * time.Millisecond
	for {
		p.mu.Lock()
		n := p.pending[repository]
		if n == 0 {
			delete(p.pending, repository)
			delete(p.running, repository)
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		if err := s.awaitLockFree(ctx, key); err == nil {
			p.mu.Lock()
			p.pending[repository] -= n
			p.mu.Unlock()
			for range n {
				s.tokens.unfence(repository)
			}
			pause = 50 * time.Millisecond
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(2*pause, tokenProofRetryMax)
	}
}

// awaitLockFree is one attempt at a proof: the lock granted, and let go at
// once, within one of the bounded slots.
func (s *service) awaitLockFree(ctx context.Context, key string) error {
	if s.testTokenProof != nil {
		if err := s.testTokenProof(); err != nil {
			return err
		}
	}
	select {
	case s.proofs.slots <- struct{}{}:
		defer func() { <-s.proofs.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(s.testTokenProofAttempt, tokenProofAttempt))
	defer cancel()
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, key); err != nil {
		return err
	}
	return tx.Commit()
}

func (ds *dataset) commitDoubted() {
	ds.tail.reset()
	ds.watch.nudge()
}

// nudge wakes every subscriber without moving the signaled seq. A commit in
// doubt may not have landed, and a seq claimed for it could be taken by the
// next write; the read the nudge wakes finds out which (changetail.go).
func (b *broadcaster) nudge() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.timer != nil {
		return
	}
	b.timer = time.AfterFunc(300*time.Millisecond, b.flush)
}

// lastSignaled is the highest seq a commit has signaled.
func (b *broadcaster) lastSignaled() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
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

// legacyAppNamespace is `app:` and the host of an app's client id, "" for
// any other actor or a client id that names no host.
func legacyAppNamespace(actor substrate.Actor, clientID string) string {
	if !substrate.IsAppActor(actor) || clientID == "" {
		return ""
	}
	u, err := url.Parse(clientID)
	if err != nil || u.Host == "" {
		return ""
	}
	return string(substrate.AppActor(u.Host))
}

// metaKeyAllowed enforces the label/annotation namespace rule: the person and
// the engine may write any well-formed key, which is what makes the `owner/…`
// convention theirs to write; every other actor — an agent, a function, a
// bundle — is bound to its own actor key as the namespace.
//
// An app writes under its own actor and, beside it, under `app:` and the host
// of its client id (clientID, substrate.ClientIDFrom): the namespace a binary
// before decision 0209 gave the same token, so keys it wrote then stay its to
// change once its actor is `app:local` or `app:<authority>`.
func metaKeyAllowed(ctx context.Context, actor substrate.Actor, clientID, key string) error {
	if problem := vocabulary.MetaKeyProblem(key); problem != "" {
		return fmt.Errorf("%w: %s", substrate.ErrValidation, problem)
	}
	if substrate.OwnersHand(ctx, actor) || actor == substrate.ActorSystem {
		return nil
	}
	ns := vocabulary.MetaKeyNamespace(key)
	if ns == actorNamespace(actor) || ns == legacyAppNamespace(actor, clientID) {
		return nil
	}
	return fmt.Errorf("%w: %s may not write key %q", substrate.ErrForbidden, handWords(actor), key)
}

// inRawTx runs fn in a plain scoped transaction: no actor, no fold, no
// changelog entry, the shape a re-key or a maintenance read needs. It
// settles nothing on purpose: fn must not fold.
func (ds *dataset) inRawTx(ctx context.Context, fn func(*txn) error) error {
	if ds.svc.readOnly {
		return ErrDirectoryReadOnly
	}
	if err := ds.svc.lease.err(); err != nil {
		return err
	}
	tx, wc, err := beginWrite(ctx, ds.db)
	if err != nil {
		return err
	}
	defer wc.release()
	// One rollback covers every exit, the commit's refusal included: a re-key
	// holds every sealed row FOR UPDATE, and a transaction left open there
	// would hold them until the context died.
	defer func() { _ = tx.Rollback() }()
	t := &txn{ctx: ctx, ds: ds, tx: tx, wc: wc, now: nowUTC(), internal: true}
	if err := fn(t); err != nil {
		return err
	}
	// The background re-derivation writes observed points outside the fold
	// (reprojection.go), and their rollups and latest rows follow them here.
	if err := t.settleObserved(); err != nil {
		return err
	}
	if len(t.folded) > 0 || len(t.pending) > 0 {
		return errors.New("substrate/engine: a raw transaction folded or appended; it may not")
	}
	return ds.commit(tx, t)
}
