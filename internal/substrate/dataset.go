package substrate

import (
	"context"
	"time"
)

// Dataset is one repository's fully isolated dataset: every operation the
// HTTP layer, the console and the background loops reach for, from the five
// mutations and the reads to vocabulary apply, triggers, functions, agents,
// bundles and blobs. Implementations are safe for concurrent use.
//
// It is ONE interface, not a core plus optional extensions a consumer
// type-asserts: a verb declared here is a verb every implementation owes,
// which is the engine's dataset and the API's hand-written fake
// (internal/api/fake_test.go). A consumer calls the method; no endpoint has a
// "this deployment cannot" branch to take.
//
// THE WHOLE INTERFACE IS WHAT v1 FREEZES: a method here is part of the
// library contract and moves under the rules the REST surface moves under
// (decision 0053, additive within v1, a break announced). Every feature
// discovery lists (stability.go) is served from here, which is why that list
// is a literal, and a feature's own stability stamp is what says how far its
// shape has settled.
type Dataset interface {
	Repository() RepositoryInfo

	// --- kind registry (builtin + installed) ---
	Kinds(ctx context.Context) ([]KindInfo, error)
	// KindByRef resolves a kind REFERENCE ("samples.substrate.reamde.dev/tasks/task", or a bare
	// "task"), or an unambiguous local name. A REST collection segment IS the
	// kind name, so the segments a request addresses spell the reference this
	// resolves, with nothing to look up in between (decision 0033).
	KindByRef(ctx context.Context, ref string) (KindInfo, error)

	// --- the five mutations ---
	// Record identity is the FULL (type, id) pair: every
	// addressed mutation names the type beside the id — a bare id names
	// nothing. `typ` accepts a full type identity or an unambiguous local
	// name, exactly like PutInput.Type.
	Put(ctx context.Context, actor Actor, in PutInput) (*Record, error)
	Patch(ctx context.Context, actor Actor, typ, id string, in PatchInput) (*Record, error)
	// Delete tombstones one record. The input carries the optional version
	// precondition; a zero DeleteInput is an unconditional delete.
	Delete(ctx context.Context, actor Actor, typ, id string, in DeleteInput) (*Record, error)
	// Merge/Split return the command-as-record record
	// (substrate.reamde.dev/core/recordmerge / substrate.reamde.dev/core/recordsplit); creating the
	// record performs the operation. Merge joins two records of ONE type, the
	// one MergeInput.Kind names. Each input carries its optional version
	// preconditions; a zero one is unconditional.
	Merge(ctx context.Context, actor Actor, in MergeInput) (*Record, error)
	Split(ctx context.Context, actor Actor, in SplitInput) (*Record, error)

	// --- reads ---
	// Get returns the full record by its (type, id) identity: properties,
	// labels, annotations and machine states. A reference property carries the
	// records this one points at; what points BACK is a List under
	// Filter.Referencing. A former id resolves within the type.
	Get(ctx context.Context, typ, id string) (*Record, error)
	List(ctx context.Context, q Query) (*Page, error)
	Search(ctx context.Context, in SearchInput) (SearchResult, error)
	Changes(ctx context.Context, after int64, f ChangeFilter, limit int) ([]Change, error)
	// Head is the changelog's highest committed seq and its history
	// generation, the pair a `from` cursor is held to before it resumes.
	Head(ctx context.Context) (ChangelogHead, error)
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

	// --- background loops and the operator's re-embed ---
	// RunGC performs one owner-reference mark-and-collect sweep for
	// records tombstoned with no remaining finalizers; returns collected.
	RunGC(ctx context.Context) (int, error)
	// ProcessEmbedQueue drains up to batch pending embed items through the
	// repository's own embeddings provider. A repository that names none
	// drains nothing and returns 0.
	ProcessEmbedQueue(ctx context.Context, batch int) (int, error)
	// Reembed enqueues every embeddable property whose stored vectors did not
	// come from the repository's currently resolved embeddings provider and
	// model. Its one caller is the operator's `substratectl repository
	// reembed`, over the DSN; no HTTP route reaches it. It buys nothing
	// itself: the queue is the work, and the drain is what pays for it, so an
	// interrupted re-embed resumes on the next pass.
	// all enqueues every embeddable property regardless of what produced its
	// vectors, which is the answer to a gateway swapped behind an unchanged
	// provider row and model name.
	Reembed(ctx context.Context, all bool) (ReembedReport, error)

	// --- the changefeed's backward page and per-row trigger stance ---
	ChangesBefore(ctx context.Context, before int64, f ChangeFilter, limit int) ([]Change, error)
	ChangeTriggers(ctx context.Context, changes []Change) (map[int64][]ChangeTrigger, error)

	// --- vocabulary (kinds, traits and property types are records) ---
	// ApplyVocabularyDocuments admits a batch in ONE transaction: every
	// document or none, activation on commit. A batch whose conversion plan is
	// lossy is refused here, under ErrLossyConversion; it takes the confirmed
	// form below.
	ApplyVocabularyDocuments(ctx context.Context, actor Actor, docs []map[string]any) ([]*Record, error)
	// PlanVocabularyApply stages the batch read-only and answers what admitting
	// it would refuse and what it would rewrite.
	PlanVocabularyApply(ctx context.Context, actor Actor, docs []map[string]any) (VocabularyPlan, error)
	// PlanVocabularyApplyWith is the preview of ApplyVocabularyDocumentsWith:
	// the same batch under the same decisions (the Origin a rehomed sample
	// claims; a Confirm is ignored, a preview has nothing to consent to), so
	// the hash it answers is the one the confirmed apply recomputes.
	PlanVocabularyApplyWith(ctx context.Context, actor Actor, docs []map[string]any, opts VocabularyApply) (VocabularyPlan, error)
	// ApplyVocabularyDocumentsWith carries the caller's decisions, the
	// confirmation a lossy plan needs among them (decision 0067).
	ApplyVocabularyDocumentsWith(ctx context.Context, actor Actor, docs []map[string]any, opts VocabularyApply) ([]*Record, error)
	// PlanShippedUpgrade previews the boot upgrade: one entry per shipped
	// package this repository holds as shipped vocabulary. It writes nothing.
	PlanShippedUpgrade(ctx context.Context) ([]ShippedUpgrade, error)

	// --- triggers and callables ---
	// The trigger verbs: status is computed, a replay is a cursor reset, a run
	// is one synthesized delivery, a wake is an immediate scan, and
	// CallFunction is the callable invocation API (`mode: call`).
	TriggerStatuses(ctx context.Context) ([]TriggerStatus, error)
	ReplayTrigger(ctx context.Context, id string, from int64) error
	RunTrigger(ctx context.Context, id, recordKind, recordID string) (int, error)
	WakeTrigger(ctx context.Context, id string) (int, error)
	TriggerFailures(ctx context.Context, id string) ([]TriggerFailure, error)
	RetryTriggerFailure(ctx context.Context, id string, failureID int64) (int, error)
	CallFunction(ctx context.Context, name string, args any) (any, int, error)
	// ProcessTriggers is the dispatcher pass the service loop drives: each
	// enabled trigger drains its changelog backlog to head or fires its due
	// occurrence. Nothing reachable from the network calls it.
	ProcessTriggers(ctx context.Context) (int, error)

	// --- agents ---
	// CallAgent is the call API's agent half; ChatAgent is the same loop with a
	// live client attached.
	CallAgent(ctx context.Context, name string, input any) (*AgentResult, error)
	ChatAgent(ctx context.Context, actor Actor, name, threadID, message string, emit func(AgentEvent)) (*AgentResult, error)
	// SweepResolutions is the resume-recovery pass the service loop drives:
	// settled threads whose newest resolution row postdates their settlement
	// get their dropped continuation back.
	SweepResolutions(ctx context.Context) (int, error)

	// --- bundles ---
	// The bundle lifecycle: status is computed; disable/enable and uninstall
	// are reversible runtime state; purge tombstones the owned package's data
	// through the finalizer flow; StartOAuth begins the host connect flow for
	// one account record.
	BundleStatuses(ctx context.Context) ([]BundleStatus, error)
	BundleStatus(ctx context.Context, id string) (BundleStatus, error)
	// BundlePackage resolves the package a bundle owns (from the live registry
	// or stored rows) for the lifecycle scope gate.
	BundlePackage(ctx context.Context, id string) (string, error)
	DisableBundle(ctx context.Context, id string) error
	EnableBundle(ctx context.Context, id string) error
	// BindBundleInput points a bundle's input at a chosen record (empty record
	// clears the choice), the explicit step of input resolution.
	BindBundleInput(ctx context.Context, id, input, record string) error
	UninstallBundle(ctx context.Context, id string) error
	PurgeBundle(ctx context.Context, id string) (int, error)
	StartOAuth(ctx context.Context, actor Actor, recordID string) (string, error)
	TypesImplementing(ctx context.Context, trait string) ([]KindInfo, error)
	// InstallBundleClosure admits the vocabulary closure AND the shipped
	// delivery wiring as ONE repository transaction, so a data-document failure
	// rolls the vocabulary apply back with it.
	InstallBundleClosure(ctx context.Context, actor Actor, vocabularyDocs []map[string]any, dataDocs []PutInput, opts BundleInstall) ([]*Record, error)
	// PlanBundleUpgrade is the read-only preview beside InstallBundleClosure:
	// what installing the shipped closure over the stored declarations would
	// move, the guard lines the install would refuse it on, and the conversion
	// plan it would run.
	PlanBundleUpgrade(ctx context.Context, vocabularyDocs []map[string]any) (BundleUpgrade, error)
	// The OAuth upkeep passes the service loop drives: refresh keeps stored
	// tokens fresh, the finalizer pass revokes and releases deleted accounts
	// ahead of GC.
	RefreshOAuthTokens(ctx context.Context) (int, error)
	ProcessOAuthFinalizers(ctx context.Context) (int, error)

	// --- blobs ---
	// The content-addressed byte store, repository-scoped: store bytes
	// (deriving the digest, minting the blob manifest) and stream them back by
	// digest.
	PutBlob(ctx context.Context, actor Actor, up BlobUpload, data []byte, wantDigest string) (*BlobInfo, error)
	GetBlob(ctx context.Context, digest string) (*BlobInfo, []byte, error)

	// --- the owner's recovery export ---
	// Export is the repository's directory as of one committed point, streamed
	// as a tar in the layout a data root has and the format an operator's
	// `repository snapshot` writes (decision 0069). The bearer token is the
	// whole credential, because a token already reads every record and every
	// blob the stream carries, and the sealed files in it are ciphertext under
	// a key the stream does not hold.
	//
	// Export pins the committed point and returns the export that streams
	// it. Pinning is short and serializes with the repository's writes; the
	// streaming does not, so writes go on while a client downloads and the
	// stream stays the point it pinned. The context is the stream's too: a
	// caller that goes away ends it.
	Export(ctx context.Context) (Export, error)
}

// ReembedReport is what one Reembed enqueued: the pair every vector will name
// once the queue drains, and how many properties are waiting.
type ReembedReport struct {
	// Provider is the llm/provider row id and Model the model it names.
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Enqueued counts the (record, property) pairs now waiting, including any
	// that were already queued.
	Enqueued int `json:"enqueued"`
	// All reports whether the scan ignored the stored provenance.
	All bool `json:"all"`
}
