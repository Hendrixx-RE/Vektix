package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Setup basic flag sets for each subcommand if needed
	switch command {
	case "setup":
		setupCmd(os.Args[2:])
	case "doctor":
		doctorCmd(os.Args[2:])
	case "index":
		indexCmd(os.Args[2:])
	case "locate":
		locateCmd(os.Args[2:])
	case "read":
		readCmd(os.Args[2:])
	case "excerpt":
		excerptCmd(os.Args[2:])
	case "open":
		openCmd(os.Args[2:])
	case "copy":
		copyCmd(os.Args[2:])
	case "list":
		listCmd(os.Args[2:])
	case "sync":
		syncCmd(os.Args[2:])
	case "status":
		statusCmd(os.Args[2:])
	case "eval":
		evalCmd(os.Args[2:])
	case "version":
		fmt.Printf("vektix version %s\n", version)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`Vektix — A local, privacy-first file locator

Usage:
  vektix <command> [arguments]

Commands:
  setup     Initialize Vektix and prepare models
  doctor    Check system health and dependencies
  index     Index local files and directories
  locate    Find files by name or content
  read      Show exact file content or range
  excerpt   Find and show relevant passages
  open      Open file in your editor
  copy      Copy file content or path to clipboard
  list      List files in a directory
  sync      Update index and purge orphans
  status    Show indexing and system status
  eval      Run evaluation suite
  version   Print version information
`)
}

func checkOllamaReachable(host string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(host + "/")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func checkOllamaModel(host, modelName string) (bool, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var res struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return false, err
	}
	for _, m := range res.Models {
		if m.Name == modelName || m.Name == modelName+":latest" {
			return true, nil
		}
	}
	return false, nil
}

func pullModel(host, modelName string) error {
	reqBody, _ := json.Marshal(map[string]string{"name": modelName})
	resp, err := http.Post(host+"/api/pull", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	decoder := json.NewDecoder(resp.Body)
	var lastLen int
	for {
		var chunk struct {
			Status    string `json:"status"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
		}
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		msg := fmt.Sprintf("Pulling %s: %s", modelName, chunk.Status)
		if chunk.Total > 0 {
			percent := float64(chunk.Completed) / float64(chunk.Total) * 100
			msg = fmt.Sprintf("Pulling %s: %s (%.1f%%)", modelName, chunk.Status, percent)
		}

		fmt.Printf("\r%s\r%s", strings.Repeat(" ", lastLen), msg)
		lastLen = len(msg)
	}
	fmt.Printf("\n✓ Pulled %s successfully\n", modelName)
	return nil
}

func setupCmd(args []string) {
	fmt.Println("Running setup...")

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Warning: Failed to load config: %v\n", err)
	} else {
		fmt.Println("✓ Config loaded successfully")
	}

	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		fmt.Printf("Error resolving data_dir: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Printf("Error creating data_dir %s: %v\n", dataDir, err)
		os.Exit(1)
	}
	fmt.Printf("✓ Data directory ready (%s)\n", dataDir)

	configDir, err := config.GetConfigDir()
	if err == nil {
		if err := os.MkdirAll(configDir, 0755); err == nil {
			configPath := filepath.Join(configDir, "config.toml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				fmt.Printf("✓ Created default config file at %s\n", configPath)
			}
		}
	}

	fmt.Println("Checking Ollama...")
	if err := checkOllamaReachable(cfg.Ollama.Host); err != nil {
		fmt.Printf("✗ Ollama not reachable at %s\n", cfg.Ollama.Host)
		fmt.Println("  Please install Ollama (https://ollama.com) and start it before running setup.")
		os.Exit(1)
	}
	fmt.Printf("✓ Ollama reachable at %s\n", cfg.Ollama.Host)

	fmt.Println("Checking required models...")
	models := []string{cfg.Ollama.EmbeddingModel, cfg.Ollama.IntentModel}
	for _, model := range models {
		has, err := checkOllamaModel(cfg.Ollama.Host, model)
		if err != nil {
			fmt.Printf("Error checking model %s: %v\n", model, err)
			os.Exit(1)
		}
		if !has {
			if err := pullModel(cfg.Ollama.Host, model); err != nil {
				fmt.Printf("\nError pulling %s: %v\n", model, err)
				os.Exit(1)
			}
		} else {
			fmt.Printf("✓ Model %s is already present\n", model)
		}
	}

	fmt.Println("\nSetup complete! Suggested default index roots:")
	for _, root := range cfg.Index.IndexDirs {
		fmt.Printf("  - %s\n", root)
	}
	fmt.Println("\nRun 'vektix index <path>' to start indexing your files.")
}

func doctorCmd(args []string) {
	fmt.Println("Vektix System Check")
	fmt.Println("===================")

	cfg, err := config.Load()
	exitCode := 0
	if err != nil {
		fmt.Printf("✗ Config: Failed to load: %v\n", err)
		exitCode = 1
	} else {
		fmt.Println("✓ Config: Loaded successfully")
	}

	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		fmt.Printf("✗ Data Directory: Error resolving path: %v\n", err)
		exitCode = 1
	} else if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		fmt.Printf("✗ Data Directory: %s does not exist\n", dataDir)
		exitCode = 1
	} else {
		fmt.Printf("✓ Data Directory: %s exists\n", dataDir)
	}

	if err := checkOllamaReachable(cfg.Ollama.Host); err != nil {
		fmt.Printf("✗ Ollama: Not reachable at %s\n", cfg.Ollama.Host)
		fmt.Println("  Please install Ollama (https://ollama.com) and ensure it is running.")
		exitCode = 1
	} else {
		fmt.Printf("✓ Ollama: Reachable at %s\n", cfg.Ollama.Host)

		models := []string{cfg.Ollama.EmbeddingModel, cfg.Ollama.IntentModel}
		for _, model := range models {
			has, err := checkOllamaModel(cfg.Ollama.Host, model)
			if err != nil {
				fmt.Printf("✗ Models: Error checking %s: %v\n", model, err)
				exitCode = 1
			} else if !has {
				fmt.Printf("✗ Models: Missing %s. Run 'vektix setup' to install.\n", model)
				exitCode = 1
			} else {
				fmt.Printf("✓ Models: Found %s\n", model)
			}
		}
	}

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func indexCmd(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "list what WOULD be indexed")
	exclude := fs.String("exclude", "", "exclude pattern")
	_ = fs.Parse(args)

	fmt.Printf("not yet implemented: index (dryRun=%v, exclude=%s, args=%v)\n", *dryRun, *exclude, fs.Args())
}

func locateCmd(args []string) {
	fs := flag.NewFlagSet("locate", flag.ExitOnError)
	scope := fs.String("scope", "", "explicit scope override")
	global := fs.Bool("global", false, "force full index")
	fs.BoolVar(global, "g", false, "force full index")
	_ = fs.Parse(args)

	fmt.Printf("not yet implemented: locate (scope=%s, global=%v, args=%v)\n", *scope, *global, fs.Args())
}

func readCmd(args []string) {
	fmt.Printf("not yet implemented: read (args=%v)\n", args)
}

func excerptCmd(args []string) {
	fmt.Printf("not yet implemented: excerpt (args=%v)\n", args)
}

func openCmd(args []string) {
	fmt.Printf("not yet implemented: open (args=%v)\n", args)
}

func copyCmd(args []string) {
	fmt.Printf("not yet implemented: copy (args=%v)\n", args)
}

func listCmd(args []string) {
	fmt.Printf("not yet implemented: list (args=%v)\n", args)
}

func syncCmd(args []string) {
	fmt.Printf("not yet implemented: sync (args=%v)\n", args)
}

func statusCmd(args []string) {
	fmt.Printf("not yet implemented: status (args=%v)\n", args)
}

func evalCmd(args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	dataset := fs.String("dataset", "", "dataset to run eval against")
	_ = fs.Parse(args)

	fmt.Printf("not yet implemented: eval (dataset=%s, args=%v)\n", *dataset, fs.Args())
}
