package embeddings

import (
	"context"
	"fmt"
	"net/http"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Known OpenAI embedding model dimensions.
var openaiModelDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

type openaiEmbedder struct {
	client    *openai.Client
	model     openai.EmbeddingModel
	dimension int
}

func newOpenAIEmbedder(model string, cfg Config) (*openaiEmbedder, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai embedder requires an API key")
	}

	dim, ok := openaiModelDimensions[model]
	if !ok {
		return nil, fmt.Errorf("unknown openai embedding model %q; known models: text-embedding-3-small, text-embedding-3-large", model)
	}

	clientCfg := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientCfg.BaseURL = cfg.BaseURL
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	clientCfg.HTTPClient = &http.Client{Timeout: timeout}

	return &openaiEmbedder{
		client:    openai.NewClientWithConfig(clientCfg),
		model:     openai.EmbeddingModel(model),
		dimension: dim,
	}, nil
}

func (e *openaiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: e.model,
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("openai embedding request failed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("openai returned no embeddings")
	}
	return resp.Data[0].Embedding, nil
}

func (e *openaiEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("openai batch embedding request failed: %w", err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("openai returned %d embeddings for %d inputs", len(resp.Data), len(texts))
	}

	results := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		results[i] = d.Embedding
	}
	return results, nil
}

func (e *openaiEmbedder) Dimension() int {
	return e.dimension
}
