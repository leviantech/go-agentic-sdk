package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAIEmbedder calls the OpenAI embeddings API (or any compatible
// endpoint such as 9router, which exposes POST /v1/embeddings).
type OpenAIEmbedder struct {
	APIKey  string
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewOpenAIEmbedder creates an embedder; point BaseURL at an
// OpenAI-compatible gateway (9router, Ollama, etc.) via WithBaseURL.
func NewOpenAIEmbedder(apiKey, model string) *OpenAIEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Model:   model,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// WithBaseURL points the embedder at another gateway, e.g.
// 9router: http://localhost:20128/v1 (9router supports /v1/embeddings)
func (e *OpenAIEmbedder) WithBaseURL(u string) *OpenAIEmbedder {
	if u != "" {
		e.BaseURL = strings.TrimSuffix(u, "/")
	}
	return e
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	payload := map[string]any{
		"model": e.Model,
		"input": text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.BaseURL+"/embeddings", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned %d", resp.StatusCode)
	}

	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("embedding API returned no data")
	}
	return out.Data[0].Embedding, nil
}
