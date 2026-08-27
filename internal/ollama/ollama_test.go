package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestEmbed_Prefixing(t *testing.T) {
	var capturedInput []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("expected /api/embed, got %s", r.URL.Path)
		}
		
		var payload struct {
			Input []string `json:"input"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &payload)
		capturedInput = payload.Input
		
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"embeddings": [[0.1, 0.2], [0.3, 0.4]]}`))
	}))
	defer ts.Close()

	client := NewClient(Options{Host: ts.URL})

	// Test Document
	reqDoc := EmbedRequest{
		Model:   "nomic",
		Texts:   []string{"doc1", "doc2"},
		IsQuery: false,
	}
	_, err := client.Embed(context.Background(), reqDoc)
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if !reflect.DeepEqual(capturedInput, []string{"search_document: doc1", "search_document: doc2"}) {
		t.Errorf("Document prefixing failed: %v", capturedInput)
	}

	// Test Query
	reqQuery := EmbedRequest{
		Model:   "nomic",
		Texts:   []string{"query1"},
		IsQuery: true,
	}
	_, err = client.Embed(context.Background(), reqQuery)
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}
	if !reflect.DeepEqual(capturedInput, []string{"search_query: query1"}) {
		t.Errorf("Query prefixing failed: %v", capturedInput)
	}
}

func TestEmbed_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(`{"embeddings": []}`))
	}))
	defer ts.Close()

	client := NewClient(Options{
		Host:         ts.URL,
		EmbedTimeout: 10 * time.Millisecond,
	})

	_, err := client.Embed(context.Background(), EmbedRequest{})
	if err == nil {
		t.Errorf("Expected timeout error, got nil")
	}
}

func TestChat_ExplicitOptionsRequired(t *testing.T) {
	client := NewClient(Options{})

	// Missing options map
	_, err := client.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Errorf("Expected error for missing options, got nil")
	}

	// Missing specific options
	opts := map[string]any{"num_ctx": 2048, "temperature": 0.0}
	_, err = client.Chat(context.Background(), ChatRequest{Options: opts})
	if err == nil {
		t.Errorf("Expected error for missing options, got nil")
	}
	
	// Valid options
	optsValid := map[string]any{
		"num_ctx": 2048,
		"num_predict": 64,
		"temperature": 0.0,
		"seed": 42,
	}
	
	// Fast fail because server is down, but shouldn't fail on options check
	_, err = client.Chat(context.Background(), ChatRequest{Options: optsValid})
	if err != nil && err.Error() == "explicit option required: num_predict" {
		t.Errorf("Should not have failed on options check")
	}
}

func TestChat_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Format any `json:"format"`
			Stream *bool `json:"stream"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)

		if req.Stream == nil || *req.Stream != false {
			t.Errorf("Expected stream=false")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": {"role": "assistant", "content": "response"}}`))
	}))
	defer ts.Close()

	client := NewClient(Options{Host: ts.URL})
	resp, err := client.Chat(context.Background(), ChatRequest{
		Options: map[string]any{
			"num_ctx": 2048, "num_predict": 64, "temperature": 0.0, "seed": 42,
		},
		Format: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Message.Content != "response" {
		t.Errorf("Expected 'response', got %q", resp.Message.Content)
	}
}

func TestChatStream_IdleTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"message": {"content": "chunk1"}, "done": false}` + "\n"))
		w.(http.Flusher).Flush()

		time.Sleep(100 * time.Millisecond) // Will trigger the 10ms idle timeout

		w.Write([]byte(`{"message": {"content": "chunk2"}, "done": true}` + "\n"))
		w.(http.Flusher).Flush()
	}))
	defer ts.Close()

	client := NewClient(Options{
		Host:              ts.URL,
		StreamIdleTimeout: 10 * time.Millisecond,
	})

	var chunks []string
	err := client.ChatStream(context.Background(), ChatRequest{
		Options: map[string]any{
			"num_ctx": 2048, "num_predict": 64, "temperature": 0.0, "seed": 42,
		},
	}, func(chunk string) error {
		chunks = append(chunks, chunk)
		return nil
	})

	if err == nil {
		t.Errorf("Expected idle timeout error, got nil")
	}
	
	if len(chunks) != 1 || chunks[0] != "chunk1" {
		t.Errorf("Expected to receive only 'chunk1' before timeout, got %v", chunks)
	}
}
