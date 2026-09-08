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
}

// VocabularyApply is what the caller decides about a batch apply beyond the
// documents themselves.
type VocabularyApply struct {
	// Confirm is the caller's consent to a lossy conversion plan, or nil.
	Confirm *ConversionConfirm
}
