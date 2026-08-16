package substrate

import "context"

// Embedder is the vector provider the embed queue drains through: one
// vector per input text, in input order, each Dimension() wide.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// EmbeddingsReporter is the optional seam a Service implements to say whether
// it has an Embedder at all. Service stays frozen, so this is asserted at
// runtime (discovery lists the `embeddings` feature only where it answers
// true) and both sides name this one symbol: a service that does not
// implement it reports no embeddings, which is the safe answer.
type EmbeddingsReporter interface {
	EmbeddingsEnabled() bool
}
