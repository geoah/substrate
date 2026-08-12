package embeddings

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// modelDimensions maps base embedding model names to their vector dimensions.
var modelDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

type litellmEmbedder struct {
	client    *openai.Client
	model     string
	dimension int
}

// newLiteLLMEmbedder creates an embedder that routes through a LiteLLM proxy.
// The model should be in LiteLLM format: "provider/model-name"
// (e.g. "openai/text-embedding-3-small").
func newLiteLLMEmbedder(model string, cfg Config) (*litellmEmbedder, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("litellm embedder requires an API key (LITELLM_MASTER_KEY)")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("litellm embedder requires a base URL")
	}

	// Extract base model name for dimension lookup: "openai/text-embedding-3-small" -> "text-embedding-3-small"
	baseModel := model
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		baseModel = model[idx+1:]
	}

	dim, ok := modelDimensions[baseModel]
	if !ok {
		return nil, fmt.Errorf("unknown embedding model %q (base: %q); known: text-embedding-3-small, text-embedding-3-large", model, baseModel)
	}

	clientCfg := openai.DefaultConfig(cfg.APIKey)
	clientCfg.BaseURL = cfg.BaseURL

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	clientCfg.HTTPClient = &http.Client{Timeout: timeout}

	return &litellmEmbedder{
		client:    openai.NewClientWithConfig(clientCfg),
		model:     model,
		dimension: dim,
	}, nil
}

func (e *litellmEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("litellm embedding request failed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("litellm returned no embeddings")
	}
	return resp.Data[0].Embedding, nil
}

func (e *litellmEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("litellm batch embedding request failed: %w", err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("litellm returned %d embeddings for %d inputs", len(resp.Data), len(texts))
	}

	results := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		results[i] = d.Embedding
	}
	return results, nil
}

func (e *litellmEmbedder) Dimension() int {
	return e.dimension
}
