package substrate

import "context"

// Embedder is the vector provider the embed queue drains through: one
// vector per input text, in input order, each Dimension() wide.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}
