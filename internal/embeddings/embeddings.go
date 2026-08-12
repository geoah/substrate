// Package embeddings turns text into vectors over an OpenAI-wire embeddings
// endpoint — the same wire every gateway that copied it speaks, so the base
// URL is what selects one.
package embeddings

import (
	"context"
	"time"
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	// Embed generates an embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension returns the dimension of the embedding vectors.
	Dimension() int
}

// Config for creating an embedder.
type Config struct {
	APIKey  string
	BaseURL string        // Optional, for custom endpoints
	Timeout time.Duration // Optional, defaults to 30s
}

// New creates an Embedder for a model id. A gateway alias
// (`openai/text-embedding-3-small`) is accepted: the dimension lookup reads
// the base name after the last slash.
func New(model string, cfg Config) (Embedder, error) {
	return newOpenAIEmbedder(model, cfg)
}
