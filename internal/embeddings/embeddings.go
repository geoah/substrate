// Package embeddings provides a unified interface for generating vector
// embeddings from text, with support for multiple providers.
package embeddings

import (
	"context"
	"fmt"
	"strings"
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

// New creates an Embedder. Model format: "provider:model-name"
// e.g. "openai:text-embedding-3-small", "ollama:nomic-embed-text"
func New(model string, cfg Config) (Embedder, error) {
	parts := strings.SplitN(model, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid model format %q: expected 'provider:model'", model)
	}

	provider, modelName := parts[0], parts[1]

	switch provider {
	case "openai":
		return newOpenAIEmbedder(modelName, cfg)
	case "ollama":
		return newOllamaEmbedder(modelName, cfg)
	case "litellm":
		return newLiteLLMEmbedder(modelName, cfg)
	default:
		return nil, fmt.Errorf("unknown embedding provider: %q", provider)
	}
}
