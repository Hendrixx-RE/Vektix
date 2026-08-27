package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// EmbedRequest defines the input for an embedding call.
type EmbedRequest struct {
	Model     string
	Texts     []string
	IsQuery   bool // If true, apply 'search_query:', else 'search_document:'
	KeepAlive string
}

// EmbedResponse contains the resulting embeddings.
type EmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed calls Ollama's /api/embed endpoint.
// It ALWAYS applies the required task prefix to the input texts.
func (c *Client) Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	prefix := "search_document: "
	if req.IsQuery {
		prefix = "search_query: "
	}

	prefixedTexts := make([]string, len(req.Texts))
	for i, text := range req.Texts {
		prefixedTexts[i] = prefix + text
	}

	payload := map[string]any{
		"model": req.Model,
		"input": prefixedTexts,
	}
	if req.KeepAlive != "" {
		payload["keep_alive"] = req.KeepAlive
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embed request: %w", err)
	}

	// Apply embed batch timeout
	var embedCtx context.Context
	var cancel context.CancelFunc
	if c.embedTimeout > 0 {
		embedCtx, cancel = context.WithTimeout(ctx, c.embedTimeout)
	} else {
		embedCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(embedCtx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed error: status %d", resp.StatusCode)
	}

	var embResp EmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode embed response: %w", err)
	}

	return &embResp, nil
}
