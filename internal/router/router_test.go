package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/ollama"
)

func TestParseFastPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Intent
	}{
		// Path shaped success
		{"open file", "open main.go", &Intent{Action: "open", Path: "main.go"}},
		{"open dir", "open /tmp", &Intent{Action: "open", Path: "/tmp"}},
		{"read file", "read config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"show file", "show config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"cat file", "cat config.yaml", &Intent{Action: "read", Path: "config.yaml"}},
		{"ls dir", "ls /usr", &Intent{Action: "list", Path: "/usr"}},
		{"head with num", "head 30 main.go", &Intent{Action: "read", Lines: "30", Path: "main.go"}},
		{"tail with -num", "tail -100 main.go", &Intent{Action: "read", Lines: "-100", Path: "main.go"}},
		{"find glob", "find *.go", &Intent{Action: "locate", Query: "*.go"}},
		{"copy ref", "copy it", &Intent{Action: "copy", Path: "it"}},
		{"copy ordinal ref", "copy #2", &Intent{Action: "copy", Path: "#2"}},
		{"copy the first one", "copy the first one", &Intent{Action: "copy", Path: "the first one"}},

		// Fail guards (fallthrough)
		{"unguarded open", "open the pod bay doors hal", nil},
		{"unguarded find", "find out what I wrote about docker", nil},
		{"unguarded show", "show me what's in the docker file", nil},
		{"copy unstructured text", "copy this text here", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFastPath(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("ParseFastPath(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseLLM(t *testing.T) {
	importJSON := `{"message": {"content": "{\"action\":\"excerpt\",\"query\":\"docker\"}"}}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(importJSON))
	}))
	defer ts.Close()

	client := ollama.NewClient(ollama.Options{
		Host:              ts.URL,
		IntentTimeout:     15 * time.Second,
		EmbedTimeout:      180 * time.Second,
		StreamIdleTimeout: 30 * time.Second,
	})

	intent, err := ParseLLM(context.Background(), client, "qwen2.5:0.5b", "find out what I wrote about docker")
	if err != nil {
		t.Fatalf("ParseLLM failed: %v", err)
	}

	expected := &Intent{Action: "excerpt", Query: "docker"}
	if !reflect.DeepEqual(intent, expected) {
		t.Errorf("ParseLLM() = %v, want %v", intent, expected)
	}
}
