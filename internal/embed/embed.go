// Package embed is the substrate.Embedder the embed queue drains through: an
// OpenAI-wire embeddings endpoint — the same wire every gateway that copied it
// speaks, so the base URL is what selects one.
package embed

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Dim is the width of the embeddings column; a model of any other
// dimensionality is a configuration error, not something to coerce.
const Dim = 1536

// DefaultModel is cheap, fast and 1536-dim.
const DefaultModel = "text-embedding-3-small"

// modelDimensions maps base embedding model names to their vector dimensions.
var modelDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

// Config configures the embedder. An empty Timeout means 30s.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Timeout time.Duration
}

// Client implements substrate.Embedder.
type Client struct {
	client    *openai.Client
	model     string
	dimension int
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
	// The model may carry a gateway's routing prefix
	// ("openai/text-embedding-3-small"), so the dimension lookup reads the
	// base name after the last slash.
	baseModel := cfg.Model
	if idx := strings.LastIndex(cfg.Model, "/"); idx >= 0 {
		baseModel = cfg.Model[idx+1:]
	}
	dim, ok := modelDimensions[baseModel]
	if !ok {
		return nil, fmt.Errorf("embed: unknown embedding model %q (base: %q); known: %s",
			cfg.Model, baseModel, strings.Join(slices.Sorted(maps.Keys(modelDimensions)), ", "))
	}
	if dim != Dim {
		return nil, fmt.Errorf("embed: model %q has dim %d; the embeddings column is vector(%d)", cfg.Model, dim, Dim)
	}
	clientCfg := openai.DefaultConfig(cfg.APIKey)
	clientCfg.BaseURL = cfg.BaseURL
	clientCfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	return &Client{
		client:    openai.NewClientWithConfig(clientCfg),
		model:     cfg.Model,
		dimension: dim,
	}, nil
}

// Embed returns one vector per input text, in order.
func (e *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: embedding request failed: %w", err)
	}
	// The queue pairs each vector with its input BY POSITION, so a short or
	// long answer is a failure, never something to zip against.
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embed: the embeddings endpoint returned %d embeddings for %d inputs", len(resp.Data), len(texts))
	}
	// The response's index field names each vector's input position —
	// arrival order carries no meaning on this wire.
	out := make([][]float32, len(resp.Data))
	seen := make([]bool, len(resp.Data))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(out) || seen[d.Index] {
			return nil, fmt.Errorf("embed: the embeddings endpoint returned an out-of-range or duplicate index %d", d.Index)
		}
		seen[d.Index] = true
		out[d.Index] = d.Embedding
	}
	return out, nil
}

// Dimension returns the model's output width.
func (e *Client) Dimension() int { return e.dimension }
