// Package embed provides the substrate.Embedder the embed queue drains
// through: the host's OpenAI-wire embeddings endpoint, over
// github.com/geoah/substrate/internal/embeddings.
package embed

import (
	"context"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/embeddings"
)

// Dim is the width of the embeddings column; a model of any other
// dimensionality is a configuration error, not something to coerce.
const Dim = 1536

// DefaultModel is cheap, fast and 1536-dim.
const DefaultModel = "text-embedding-3-small"

// Config configures the embedder.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Client implements substrate.Embedder.
type Client struct {
	inner embeddings.Embedder
}

// New builds the embedder. It returns (nil, nil) when no endpoint or API key
// is configured, so the service runs without semantic search rather than
// refusing to boot. There is no default endpoint: a deployment names its own
// gateway (SUBSTRATE_LLM_BASE_URL) or goes without.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		return nil, nil
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	inner, err := embeddings.New(cfg.Model, embeddings.Config{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if got := inner.Dimension(); got != Dim {
		return nil, fmt.Errorf("embed: model %q has dim %d; the embeddings column is vector(%d)", cfg.Model, got, Dim)
	}
	return &Client{inner: inner}, nil
}

// Embed returns one vector per input text, in order.
func (e *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return e.inner.EmbedBatch(ctx, texts)
}

// Dimension returns the model's output width.
func (e *Client) Dimension() int { return e.inner.Dimension() }
