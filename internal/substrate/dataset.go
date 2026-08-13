package substrate

import (
	"context"
	"time"
)

// Dataset is one repository's fully isolated dataset — every operation of the
// seven-mutation write surface and four-query read surface, plus the
// system seams (tokens, GC, embeddings) the service loops need.
// Implementations are safe for concurrent use.
//
// THE FROZEN LIBRARY CONTRACT IS THIS CORE ONLY.
// The EXTENSION TIER — schema apply, triggers, functions, agents, bundles,
// blobs — is NOT on this interface: it is the fast-moving surface and lives
// as concrete methods on the engine's *dataset (ApplyVocabularyDocuments,
// InstallBundleClosure, the trigger/function/agent/bundle/blob operations,
// the chat/call agent wire). A library consumer that needs the bundle
// tier type-asserts the concrete engine type or its own narrow interface;
// the seven mutations and four reads below are the part frozen at v1.
type Dataset interface {
	Repository() RepositoryInfo

	// --- kind registry (builtin + installed) ---
	Kinds(ctx context.Context) ([]KindInfo, error)
	// KindByRef resolves a kind REFERENCE ("tasks.substrate.geoah.me/task", or a bare
	// "task"), or an unambiguous local name.
	KindByRef(ctx context.Context, ref string) (KindInfo, error)
	// KindByPlural resolves a REST collection segment within an authority; an
	// empty authority resolves a repository-local kind's plural.
	KindByPlural(ctx context.Context, authority, plural string) (KindInfo, error)

	// --- the seven mutations ---
	// Record identity is the FULL (type, id) pair: every
	// addressed mutation names the type beside the id — a bare id names
	// nothing. `typ` accepts a full type identity or an unambiguous local
	// name, exactly like PutInput.Type.
	Put(ctx context.Context, actor Actor, in PutInput) (*Record, error)
	Patch(ctx context.Context, actor Actor, typ, id string, in PatchInput) (*Record, error)
	Delete(ctx context.Context, actor Actor, typ, id string) (*Record, error)
	// Link/Unlink address the source by (srcType, src); the target reference
	// is `{authority, type, id}` — bare `{id}` is legal only where the edge
	// declaration pins a single target type.
	Link(ctx context.Context, actor Actor, srcType, src, rel string, to EdgeRef, props map[string]any) error
	Unlink(ctx context.Context, actor Actor, srcType, src, rel string, to EdgeRef) error
	// Merge/Split return the command-as-record record
	// (core.substrate.reamde.dev/recordmerge / core.substrate.reamde.dev/recordsplit); creating the
	// record performs the operation. Merge joins two records of ONE type.
	Merge(ctx context.Context, actor Actor, typ, winner, loser string) (*Record, error)
	Split(ctx context.Context, actor Actor, mergeID string) (*Record, error)

	// --- reads ---
	// Get returns the full record by its (type, id) identity: properties,
	// labels, annotations, machine states, and one hop of edges (both
	// directions are the API layer's concern; Edges holds declared outgoing
	// rels). A former id resolves within the type.
	Get(ctx context.Context, typ, id string) (*Record, error)
	List(ctx context.Context, q Query) (*Page, error)
	Search(ctx context.Context, in SearchInput) ([]Hit, error)
	Changes(ctx context.Context, after int64, f ChangeFilter, limit int) ([]Change, error)
	// WatchSignal delivers a coalesced head-change signal: each value is the
	// highest committed changelog seq known when the signal fired, coalesced
	// over a short window (~300ms) so a burst of writes wakes a consumer once.
	// It is a hint to refetch Changes from your cursor, never a delivery of
	// the changes themselves, and it is level-triggered — a consumer that
	// missed intermediate values still catches up from its cursor. The
	// channel closes when ctx ends. This is the SEMANTIC contract; how the
	// signal is fanned out across replicas is an implementation detail, not
	// promised here.
	WatchSignal(ctx context.Context) <-chan int64

	// --- tokens (RECORDS in this repository; the secret is hashed) ---
	// MintToken writes one token record and returns its secret, shown
	// exactly once. expiresAt is optional; nil lives until the record is
	// deleted. Revoking is deleting the record — through these endpoints or
	// the ordinary record delete, which is the same write either way.
	MintToken(ctx context.Context, label string, expiresAt *time.Time) (TokenInfo, string, error)
	Tokens(ctx context.Context) ([]TokenInfo, error)

	// Incoming pages what points at one record — edges and reference
	// properties alike, narrowable to one relationship or one source kind so a
	// drill-down expands a group without pulling the rest. A reference with no
	// declared target kind names nothing the registry can search for, so those
	// are outside what this answers.
	Incoming(ctx context.Context, typ, id string, opts IncomingOptions) (*IncomingPage, error)

	// --- background loops (service wiring calls these) ---
	// RunGC performs one owner-reference mark-and-collect sweep for
	// records tombstoned with no remaining finalizers; returns collected.
	RunGC(ctx context.Context) (int, error)
	// ProcessEmbedQueue drains up to batch pending embed items through e.
	ProcessEmbedQueue(ctx context.Context, e Embedder, batch int) (int, error)
}
