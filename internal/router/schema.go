package router

// ActionSchema is the JSON Schema for the action set, exactly as specified in plan.md.
// It is passed to Ollama's format parameter for grammar-constrained decoding.
var ActionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action": map[string]any{
			"enum": []string{"locate", "read", "excerpt", "open", "copy", "list"},
		},
		"query": map[string]any{
			"type": "string",
		},
		"path": map[string]any{
			"type": "string",
		},
		"lines": map[string]any{
			"type": "string",
			"pattern": "^\\d+(-\\d+)?$",
		},
	},
	"required": []string{"action"},
}
