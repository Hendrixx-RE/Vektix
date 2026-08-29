package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
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
	case "reindex":
		reindexCmd(os.Args[2:])
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
  reindex   Rebuild the index from scratch
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
	dryRun := fs.Bool("dry-run", false, "list what WOULD be indexed, without writing to the store")
	exclude := fs.String("exclude", "", "comma-separated glob patterns or paths to exclude")
	flagArgs, roots := splitFlags(fs, args)
	_ = fs.Parse(flagArgs)

	if len(roots) == 0 {
		fmt.Println("Usage: vektix index <path> [--dry-run] [--exclude pattern]")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	applyExcludeFlag(&cfg.Index.Exclude, *exclude)

	runIndex(cfg, roots, index.ModeIndex, *dryRun)
}

// splitFlags separates flag tokens from positional arguments so flags may
// appear anywhere on the command line — plan.md documents usage like
// `vektix index ~/Documents --dry-run`, with flags trailing the path, but
// the stdlib flag package stops parsing flags at the first positional
// argument and would silently leave --dry-run unset.
func splitFlags(fs *flag.FlagSet, args []string) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.ContainsRune(name, '=') {
			continue // --flag=value is self-contained
		}
		if fl := fs.Lookup(name); fl != nil {
			if bv, ok := fl.Value.(interface{ IsBoolFlag() bool }); !ok || !bv.IsBoolFlag() {
				if i+1 < len(args) {
					i++
					flags = append(flags, args[i])
				}
			}
		}
	}
	return flags, positional
}

// applyExcludeFlag folds a --exclude value into the config's exclude rules.
// A pattern containing a glob metacharacter is treated as a filename rule
// (matching the "*.pdf" example in plan.md); anything else is treated as a
// path prefix (matching the "~/Documents/archive" example).
func applyExcludeFlag(ex *config.ExcludeConfig, raw string) {
	for _, pattern := range strings.Split(raw, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.ContainsAny(pattern, "*?[") {
			ex.Files = append(ex.Files, pattern)
		} else {
			ex.Paths = append(ex.Paths, pattern)
		}
	}
}

// runIndex drives the index/sync pipeline and prints progress and a final
// summary. It is shared by indexCmd and syncCmd so both commands stay
// consistent with the manifest-refusal and quarantine behavior.
func runIndex(cfg config.Config, roots []string, mode index.Mode, dryRun bool) {
	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		fmt.Printf("Error resolving data_dir: %v\n", err)
		os.Exit(1)
	}

	var (
		st  index.VectorStore
		cli index.Embedder
	)
	if !dryRun {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			fmt.Printf("Error creating data_dir %s: %v\n", dataDir, err)
			os.Exit(1)
		}
		s, err := store.NewPersistentDB(index.StorePath(dataDir))
		if err != nil {
			fmt.Printf("Error opening store: %v\n", err)
			os.Exit(1)
		}
		st = s
		cli = ollama.NewClient(ollama.Options{
			Host:         cfg.Ollama.Host,
			EmbedTimeout: time.Duration(cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		})
	}

	engine := index.NewEngine(&cfg, st, cli, dataDir)
	engine.DryRun = dryRun

	var lastLen int
	engine.OnProgress = func(p index.Progress) {
		msg := fmt.Sprintf("scanned %d  indexed %d  chunks %d  quarantined %d",
			p.Scanned, p.Indexed, p.Chunks, p.Quarantined)
		fmt.Printf("\r%s\r%s", strings.Repeat(" ", lastLen), msg)
		lastLen = len(msg)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	res, err := engine.Run(ctx, roots, mode)
	if lastLen > 0 {
		fmt.Printf("\r%s\r", strings.Repeat(" ", lastLen))
	}

	if err != nil {
		var invalid *index.InvalidIndexError
		if errors.As(err, &invalid) {
			fmt.Println(invalid.Error())
			os.Exit(1)
		}
		if errors.Is(err, context.Canceled) {
			fmt.Println("Interrupted — index left in a consistent state.")
			if res != nil {
				fmt.Print(res.Summary())
			}
			os.Exit(130)
		}
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if dryRun {
		fmt.Printf("Would index %d file(s) — %d added, %d updated, %d unchanged:\n",
			len(res.Files), res.Added, res.Updated, res.Unchanged)
		for _, f := range res.Files {
			fmt.Printf("  %s\n", f)
		}
		return
	}

	fmt.Print(res.Summary())
	if len(res.Quarantined) > 0 {
		fmt.Printf("  %d file(s) quarantined — see 'vektix status'\n", len(res.Quarantined))
	}
}

func syncCmd(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// No roots given: re-walk whatever the manifest already knows about.
	runIndex(cfg, fs.Args(), index.ModeSync, false)
}

func reindexCmd(args []string) {
	fs := flag.NewFlagSet("reindex", flag.ExitOnError)
	_ = fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	runIndex(cfg, fs.Args(), index.ModeReindex, false)
}

func evalCmd(args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	dataset := fs.String("dataset", "", "dataset to run eval against")
	_ = fs.Parse(args)

	fmt.Printf("not yet implemented: eval (dataset=%s, args=%v)\n", *dataset, fs.Args())
}
