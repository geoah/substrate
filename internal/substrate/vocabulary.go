package substrate

import "context"

// VocabularyApplier is the batch vocabulary verb (kinds, traits and property
// types are records), an optional Dataset extension (see Dataset): one
// transaction, every document admitted or none, activation on commit.
type VocabularyApplier interface {
	ApplyVocabularyDocuments(ctx context.Context, actor Actor, docs []map[string]any) ([]*Record, error)
}
