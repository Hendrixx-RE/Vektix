package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	General  GeneralConfig  `toml:"general"`
	Ollama   OllamaConfig   `toml:"ollama"`
	Index    IndexConfig    `toml:"index"`
	Search   SearchConfig   `toml:"search"`
	Chunking ChunkingConfig `toml:"chunking"`
	Safety   SafetyConfig   `toml:"safety"`
}

type GeneralConfig struct {
	DataDir   string `toml:"data_dir"`
	Editor    string `toml:"editor"`
	ScopeMode string `toml:"scope_mode"`
}

type OllamaConfig struct {
	Host           string         `toml:"host"`
	EmbeddingModel string         `toml:"embedding_model"`
	IntentModel    string         `toml:"intent_model"`
	ExplainModel   string         `toml:"explain_model"`
	KeepAlive      string         `toml:"keep_alive"`
	Timeouts       OllamaTimeouts `toml:"timeouts"`
	Context        OllamaContext  `toml:"context"`
}

type OllamaTimeouts struct {
	EmbedBatchSeconds int `toml:"embed_batch_seconds"`
	IntentSeconds     int `toml:"intent_seconds"`
	StreamIdleSeconds int `toml:"stream_idle_seconds"`
}

type OllamaContext struct {
	IntentNumCtx  int `toml:"intent_num_ctx"`
	ExplainNumCtx int `toml:"explain_num_ctx"`
}

type IndexConfig struct {
	IndexDirs              []string      `toml:"index_dirs"`
	Extensions             []string      `toml:"extensions"`
	MaxFileSizeMB          int           `toml:"max_file_size_mb"`
	FollowSymlinks         bool          `toml:"follow_symlinks"`
	Exclude                ExcludeConfig `toml:"exclude"`
	TransientRetentionDays int           `toml:"transient_retention_days"`
	MaxTransientRoots      int           `toml:"max_transient_roots"`
}

type ExcludeConfig struct {
	Dirs  []string `toml:"dirs"`
	Files []string `toml:"files"`
	Paths []string `toml:"paths"`
}

type SearchConfig struct {
	MaxResults      int     `toml:"max_results"`
	RRFK            int     `toml:"rrf_k"`
	MinArms         int     `toml:"min_arms"`
	OversampleFloor float64 `toml:"oversample_floor"`
}

type ChunkingConfig struct {
	MaxTokens     int `toml:"max_tokens"`
	OverlapTokens int `toml:"overlap_tokens"`
	MinTokens     int `toml:"min_tokens"`
}

type SafetyConfig struct {
	ConfineToRoots bool `toml:"confine_to_roots"`
	AllowSecrets   bool `toml:"allow_secrets"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		General: GeneralConfig{
			DataDir:   "~/.local/share/vektix",
			Editor:    "",
			ScopeMode: "auto",
		},
		Ollama: OllamaConfig{
			Host:           "http://localhost:11434",
			EmbeddingModel: "nomic-embed-text",
			IntentModel:    "qwen2.5:0.5b",
			ExplainModel:   "qwen2.5:3b-instruct",
			KeepAlive:      "5m",
			Timeouts: OllamaTimeouts{
				EmbedBatchSeconds: 180,
				IntentSeconds:     15,
				StreamIdleSeconds: 30,
			},
			Context: OllamaContext{
				IntentNumCtx:  2048,
				ExplainNumCtx: 8192,
			},
		},
		Index: IndexConfig{
			IndexDirs: []string{"~/Documents", "~/notes", "~/projects"},
			Extensions: []string{
				".txt", ".md", ".pdf",
				".go", ".py", ".js", ".ts", ".rs", ".sh", ".c", ".java",
				".json", ".yaml", ".yml", ".toml",
			},
			MaxFileSizeMB:          50,
			FollowSymlinks:         false,
			TransientRetentionDays: 7,
			MaxTransientRoots:      10,
			Exclude: ExcludeConfig{
				Dirs: []string{
					"node_modules", ".git", "__pycache__", ".venv", "venv", ".cache",
					".trash", "dist", "build", "target", ".next", "vendor",
				},
				Files: []string{
					"*.min.js", "*.min.css", "*.map", "*.lock", "*.sum", "*.exe", "*.bin",
					"*.so", "*.dylib", "*.o", "*.pyc", "*.class", "*.wasm",
					"package-lock.json", "yarn.lock",
				},
				Paths: []string{"~/Documents/archive/old-backups"},
			},
		},
		Search: SearchConfig{
			MaxResults:      8,
			RRFK:            60,
			MinArms:         1,
			OversampleFloor: 0.01,
		},
		Chunking: ChunkingConfig{
			MaxTokens:     256,
			OverlapTokens: 50,
			MinTokens:     20,
		},
		Safety: SafetyConfig{
			ConfineToRoots: true,
			AllowSecrets:   false,
		},
	}
}

// ExpandPath expands `~` to the user's home directory.
func ExpandPath(path string) (string, error) {
	if len(path) > 0 && path[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, path[1:]), nil
	}
	return path, nil
}

// GetConfigDir returns the standard configuration directory.
func GetConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home dir
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configDir, "vektix"), nil
}

// Load reads the config from ~/.config/vektix/config.toml, filling in with defaults.
func Load() (Config, error) {
	cfg := DefaultConfig()

	configDir, err := GetConfigDir()
	if err != nil {
		return cfg, err
	}

	configPath := filepath.Join(configDir, "config.toml")
	
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		// No config file found, return default
		return cfg, nil
	}

	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
