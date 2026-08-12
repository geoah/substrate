package substrate

import "context"

// Embedder is the vector provider the embed queue drains through
// (LiteLLM-backed in prod, fake in tests).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}
