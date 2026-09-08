package substrate

import "context"

// VocabularyApplier is the batch vocabulary verb (kinds, traits and property
// types are records), an optional Dataset extension (see Dataset): one
// transaction, every document admitted or none, activation on commit. A batch
// whose conversion plan is lossy is refused here, under ErrLossyConversion: it
// takes VocabularyPlanner's confirmed form.
type VocabularyApplier interface {
	ApplyVocabularyDocuments(ctx context.Context, actor Actor, docs []map[string]any) ([]*Record, error)
}

// VocabularyPlanner is the preview and the confirmed form of the batch verb,
// an optional Dataset extension (see Dataset). PlanVocabularyApply stages the
// batch read-only and answers what admitting it would refuse and what it
// would rewrite; ApplyVocabularyDocumentsWith is ApplyVocabularyDocuments
// carrying the caller's decisions, the confirmation a lossy plan needs among
// them (decision 0067).
type VocabularyPlanner interface {
	PlanVocabularyApply(ctx context.Context, actor Actor, docs []map[string]any) (VocabularyPlan, error)
	// PlanVocabularyApplyWith is the preview of ApplyVocabularyDocumentsWith:
	// the same batch under the same decisions (the Origin a rehomed sample
	// claims; a Confirm is ignored, a preview has nothing to consent to), so
	// the hash it answers is the one the confirmed apply recomputes.
	PlanVocabularyApplyWith(ctx context.Context, actor Actor, docs []map[string]any, opts VocabularyApply) (VocabularyPlan, error)
	ApplyVocabularyDocumentsWith(ctx context.Context, actor Actor, docs []map[string]any, opts VocabularyApply) ([]*Record, error)
}

// VocabularyPlan is the preview of one batch apply: the guard lines the apply
// would refuse on and the conversion plan it would run.
type VocabularyPlan struct {
	ConversionPlan
	// Blockers are the refuse-breakage guard lines, with the live-row counts,
	// and a plan above the work ceiling. Empty means the apply would be
	// admitted, confirmed where Lossy says so.
	Blockers []string `json:"blockers,omitempty"`
	// DiscardsEdits reports the batch, which claims an origin, replaces a
	// copy edited since its stamp (BundleUpgrade.DiscardsEdits, decision
	// record 0070). It runs only with a confirmation carrying this plan's
	// PlanHash and ChangelogSeq.
	DiscardsEdits bool `json:"discardsEdits,omitempty"`
}

// VocabularyApply is what the caller decides about a batch apply beyond the
// documents themselves.
type VocabularyApply struct {
	// Confirm is the caller's consent to a lossy conversion plan, or nil.
	Confirm *ConversionConfirm
	// Origin is the shipped bundle id the batch is a copy of
	// ("samples.substrate.reamde.dev/tasks"), when the caller rehomed a
	// shipped sample by hand (`substratectl apply --as`) and says so
	// (decision record 0070). The engine records the claim as it records the
	// import door's: the origin, the batch's package version and a digest of
	// what landed, stamped on the package row whose name the origin's package
	// segment spells; the batch must carry that package's document. A batch
	// naming an origin over a copy edited since its stamp replaces the edits,
	// and runs only with a Confirm bound to the preview that said so. Empty
	// is the ordinary apply, which stamps nothing.
	Origin string
}
