package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, input string) ([]float64, error)
	Model() string
}

type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Embedding  []float64   `json:"embedding"`
}

func NewOllamaEmbedder(baseURL string, model string) OllamaEmbedder {
	return OllamaEmbedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (e OllamaEmbedder) Model() string {
	return e.model
}

func (e OllamaEmbedder) Embed(ctx context.Context, input string) ([]float64, error) {
	if strings.TrimSpace(e.baseURL) == "" || strings.TrimSpace(e.model) == "" {
		return nil, errors.New("ollama embedder not configured")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("embedding input is empty")
	}

	body, err := json.Marshal(ollamaEmbedRequest{
		Model: e.model,
		Input: input,
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := e.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama embed returned %d", response.StatusCode)
	}

	var payload ollamaEmbedResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Embeddings) > 0 && len(payload.Embeddings[0]) > 0 {
		return payload.Embeddings[0], nil
	}
	if len(payload.Embedding) > 0 {
		return payload.Embedding, nil
	}
	return nil, errors.New("ollama embed response missing vector")
}
