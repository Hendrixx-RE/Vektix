package router

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Hendrixx-RE/Vektix/internal/ollama"
)

const systemPrompt = "You are Vektix's intent classifier. You perform read-only operations only. Map the user's phrasing to one action. Do not answer the question itself."

// ParseLLM calls the local Ollama instance with a JSON Schema-constrained format to classify the intent.
func ParseLLM(ctx context.Context, client *ollama.Client, model, input string) (*Intent, error) {
	req := ollama.ChatRequest{
		Model: model,
		Messages: []ollama.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: input},
		},
		Format: ActionSchema,
		Options: map[string]any{
			"num_ctx":     2048,
			"num_predict": 64,
			"temperature": 0,
			"seed":        1,
		},
	}

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("intent classification failed: %w", err)
	}

	var intent Intent
	if err := json.Unmarshal([]byte(resp.Message.Content), &intent); err != nil {
		return nil, fmt.Errorf("failed to parse intent JSON: %w", err)
	}

	return &intent, nil
}
