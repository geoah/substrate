// Package embed is the substrate.Embedder the embed queue drains through: an
// OpenAI-wire embeddings endpoint. Every gateway that copied that wire speaks
// it, so the base URL is what selects one, and the caller resolves that URL
// from a repository's llmprovider row rather than from the process.
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
// It is the whole model set: decision record 0026 fixes the stored width at
// Dim, so a model this table does not carry is refused where the provider row
// is written rather than guessed at when the vectors arrive.
var modelDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

// BaseModel strips a gateway's routing prefix
// ("openai/text-embedding-3-small"), which is the name the dimension table is
// keyed by.
func BaseModel(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// ModelDim reports a model's output width. The second result is false for a
// model the table does not carry, which is the caller's cue to refuse rather
// than to assume Dim.
func ModelDim(model string) (int, bool) {
	dim, ok := modelDimensions[BaseModel(model)]
	return dim, ok
}

// KnownModels lists the models ModelDim answers for, sorted, for an error
// message that tells the reader what to name instead.
func KnownModels() []string {
	return slices.Sorted(maps.Keys(modelDimensions))
}

// Config configures the embedder: one resolved llmprovider row's endpoint,
// key, headers and model. An empty Timeout means 30s.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
	Timeout time.Duration
}

// headerTransport rides the gateway's own headers on every request. The
// llmprovider row declares them (attribution headers, a proxy's routing
// header) and internal/llm sends the same set on completions.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The request is the caller's; cloning is what keeps a retry from seeing
	// headers this transport added to the previous attempt.
	clone := req.Clone(req.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// Client implements substrate.Embedder.
type Client struct {
	client    *openai.Client
	model     string
	dimension int
}

// New builds the embedder for one resolved provider row. It refuses a config
// with no endpoint, no key or no model: there is no default endpoint and no
// host-wide key, so a repository that has named no embeddings provider has no
// embedder at all, and the caller answers that by not calling New.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("embed: no baseURL")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("embed: no apiKey")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("embed: no model")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	dim, ok := ModelDim(cfg.Model)
	if !ok {
		return nil, fmt.Errorf("embed: unknown embedding model %q (base: %q); known: %s",
			cfg.Model, BaseModel(cfg.Model), strings.Join(KnownModels(), ", "))
	}
	if dim != Dim {
		return nil, fmt.Errorf("embed: model %q has dim %d; the embeddings column is vector(%d)", cfg.Model, dim, Dim)
	}
	clientCfg := openai.DefaultConfig(cfg.APIKey)
	clientCfg.BaseURL = cfg.BaseURL
	httpClient := &http.Client{Timeout: cfg.Timeout}
	if len(cfg.Headers) > 0 {
		httpClient.Transport = &headerTransport{headers: cfg.Headers}
	}
	clientCfg.HTTPClient = httpClient
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
		// The declared width is what the storage column holds, and a gateway
		// serving something else under a known model name is the one way past
		// the write-time check (decision record 0026). Refuse here rather than
		// let Postgres refuse the insert with no model in the message.
		if len(d.Embedding) != e.dimension {
			return nil, fmt.Errorf("embed: model %q returned a %d-wide vector; %d is the declared width",
				e.model, len(d.Embedding), e.dimension)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

// Dimension returns the model's output width.
func (e *Client) Dimension() int { return e.dimension }

// Model returns the model id as sent, which is what stamps a stored vector's
// provenance.
func (e *Client) Model() string { return e.model }
