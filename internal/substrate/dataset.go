package substrate

import (
	"context"
	"time"
)

// Dataset is one repository's fully isolated dataset — every operation of the
// five-mutation write surface and four-query read surface, plus the
// system seams (tokens, GC, embeddings) the service loops need.
// Implementations are safe for concurrent use.
//
// THE LIBRARY CONTRACT MEANT TO FREEZE AT v1 IS THIS CORE ONLY.
// The EXTENSION TIER — vocabulary apply, triggers, functions, agents,
// bundles, blobs — is NOT on this interface: it is the fast-moving surface,
// and each part of it is a named OPTIONAL EXTENSION interface in this package
// that an implementation may also satisfy: VocabularyApplier, AutomationOps,
// TriggerDispatcher, AgentOps, ResolutionSweeper, BundleOps, BundleInstaller,
// BundleUpgradePlanner, OAuthMaintainer, BlobStore, ChangeFeedOps. A consumer
// that needs one type-asserts THAT interface, never a concrete engine type,
// and every implementation asserts each seam it satisfies at compile time
// (`var _ substrate.BundleOps = (*dataset)(nil)`) — otherwise renaming a
// method turns a whole endpoint family into a 501 with a green build.
// The five mutations and four reads below are the part meant to freeze at v1;
// the HTTP paths that serve them are not frozen yet, which is why no discovery
// feature reports stable (stability.go).
type Dataset interface {
	Repository() RepositoryInfo

	// --- kind registry (builtin + installed) ---
	Kinds(ctx context.Context) ([]KindInfo, error)
	// KindByRef resolves a kind REFERENCE ("tasks.substrate.reamde.dev/task", or a bare
	// "task"), or an unambiguous local name. A REST collection segment IS the
	// kind name, so the two segments a request addresses spell the reference
	// this resolves — routing needs no plural lookup (decision 0033).
	KindByRef(ctx context.Context, ref string) (KindInfo, error)

	// --- the five mutations ---
	// Record identity is the FULL (type, id) pair: every
	// addressed mutation names the type beside the id — a bare id names
	// nothing. `typ` accepts a full type identity or an unambiguous local
	// name, exactly like PutInput.Type.
	Put(ctx context.Context, actor Actor, in PutInput) (*Record, error)
	Patch(ctx context.Context, actor Actor, typ, id string, in PatchInput) (*Record, error)
	Delete(ctx context.Context, actor Actor, typ, id string) (*Record, error)
	// Merge/Split return the command-as-record record
	// (core.substrate.reamde.dev/recordmerge / core.substrate.reamde.dev/recordsplit); creating the
	// record performs the operation. Merge joins two records of ONE type.
	Merge(ctx context.Context, actor Actor, typ, winner, loser string) (*Record, error)
	Split(ctx context.Context, actor Actor, mergeID string) (*Record, error)

	// --- reads ---
	// Get returns the full record by its (type, id) identity: properties,
	// labels, annotations and machine states. A reference property carries the
	// records this one points at; what points BACK is Incoming. A former id
	// resolves within the type.
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

	// Incoming pages the reference properties naming one record, narrowable to
	// one property or one source kind so a drill-down expands a group without
	// pulling the rest.
	Incoming(ctx context.Context, typ, id string, opts IncomingOptions) (*IncomingPage, error)

	// --- background loops (service wiring calls these) ---
	// RunGC performs one owner-reference mark-and-collect sweep for
	// records tombstoned with no remaining finalizers; returns collected.
	RunGC(ctx context.Context) (int, error)
	// ProcessEmbedQueue drains up to batch pending embed items through the
	// repository's own embeddings provider. A repository that names none
	// drains nothing and returns 0.
	ProcessEmbedQueue(ctx context.Context, batch int) (int, error)
	// Reembed enqueues every embeddable property whose stored vectors did not
	// come from the repository's currently resolved embeddings provider and
	// model. It buys nothing itself: the queue is the work, and the drain is
	// what pays for it, so an interrupted re-embed resumes on the next pass.
	// all enqueues every embeddable property regardless of what produced its
	// vectors, which is the answer to a gateway swapped behind an unchanged
	// provider row and model name.
	Reembed(ctx context.Context, all bool) (ReembedReport, error)
}

// ReembedReport is what one Reembed enqueued: the pair every vector will name
// once the queue drains, and how many properties are waiting.
type ReembedReport struct {
	// Provider is the llmprovider row id and Model the model it names.
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Enqueued counts the (record, property) pairs now waiting, including any
	// that were already queued.
	Enqueued int `json:"enqueued"`
	// All reports whether the scan ignored the stored provenance.
	All bool `json:"all"`
}
