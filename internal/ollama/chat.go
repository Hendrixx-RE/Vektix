package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest defines the request to Ollama's chat API.
type ChatRequest struct {
	Model     string         `json:"model"`
	Messages  []Message      `json:"messages"`
	Format    any            `json:"format,omitempty"` // Full JSON schema allowed
	Options   map[string]any `json:"options"`          // Explicit options required
	Stream    *bool          `json:"stream,omitempty"`
	KeepAlive string         `json:"keep_alive,omitempty"`
}

// ChatResponse defines a response from Ollama's chat API.
type ChatResponse struct {
	Model     string  `json:"model"`
	CreatedAt string  `json:"created_at"`
	Message   Message `json:"message"`
	Done      bool    `json:"done"`
}

func checkRequiredOptions(opts map[string]any) error {
	if opts == nil {
		return fmt.Errorf("options map is required")
	}
	required := []string{"num_ctx", "num_predict", "temperature", "seed"}
	for _, k := range required {
		if _, ok := opts[k]; !ok {
			return fmt.Errorf("explicit option required: %s", k)
		}
	}
	return nil
}

// Chat calls Ollama's /api/chat endpoint synchronously.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if err := checkRequiredOptions(req.Options); err != nil {
		return nil, err
	}

	f := false
	req.Stream = &f
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat request: %w", err)
	}

	var chatCtx context.Context
	var cancel context.CancelFunc
	if c.intentTimeout > 0 {
		chatCtx, cancel = context.WithTimeout(ctx, c.intentTimeout)
	} else {
		chatCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(chatCtx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create chat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama chat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama chat error: status %d", resp.StatusCode)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode chat response: %w", err)
	}

	return &chatResp, nil
}

// ChatStreamFunc is called for each streaming chunk.
type ChatStreamFunc func(chunk string) error

// idleTimerReader wraps an io.Reader and cancels a context if Read takes too long.
type idleTimerReader struct {
	r       io.Reader
	timeout time.Duration
	cancel  context.CancelFunc
}

func (itr *idleTimerReader) Read(p []byte) (n int, err error) {
	if itr.timeout > 0 {
		timer := time.AfterFunc(itr.timeout, func() {
			itr.cancel()
		})
		defer timer.Stop()
	}
	return itr.r.Read(p)
}

// ChatStream calls Ollama's /api/chat endpoint in streaming mode, with an idle timeout.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onChunk ChatStreamFunc) error {
	if err := checkRequiredOptions(req.Options); err != nil {
		return err
	}

	t := true
	req.Stream = &t

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal chat stream request: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create chat stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama chat stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama chat stream error: status %d", resp.StatusCode)
	}

	reader := &idleTimerReader{
		r:       resp.Body,
		timeout: c.streamIdleTimeout,
		cancel:  cancel,
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var chunkResp ChatResponse
		if err := json.Unmarshal(line, &chunkResp); err != nil {
			return fmt.Errorf("failed to decode stream chunk: %w", err)
		}
		if err := onChunk(chunkResp.Message.Content); err != nil {
			return err
		}
		if chunkResp.Done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream read error: %w", err)
	}

	return nil
}
