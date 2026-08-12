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

type openaiEmbedder struct {
	client    *openai.Client
	model     string
	dimension int
}

// newOpenAIEmbedder creates an embedder against an OpenAI-wire endpoint. The
// model may carry a gateway's routing prefix ("openai/text-embedding-3-small")
// — the dimension lookup reads the base name after the last slash.
func newOpenAIEmbedder(model string, cfg Config) (*openaiEmbedder, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("the embeddings endpoint needs an API key")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("the embeddings endpoint needs a base URL")
	}

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

	return &openaiEmbedder{
		client:    openai.NewClientWithConfig(clientCfg),
		model:     model,
		dimension: dim,
	}, nil
}

func (e *openaiEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: []string{text},
	})
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("the embeddings endpoint returned no embeddings")
	}
	return resp.Data[0].Embedding, nil
}

func (e *openaiEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("batch embedding request failed: %w", err)
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("the embeddings endpoint returned %d embeddings for %d inputs", len(resp.Data), len(texts))
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
