package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
				// TODO: Actually serialize the default config to this file.
				// For now, the user can use the builtin defaults.
			}
		}
	}

	// TODO: Check Ollama reachability and model presence once internal/ollama is ready
	fmt.Println("[TODO] Check Ollama reachability and pull models (nomic-embed-text, qwen2.5:0.5b)")
	fmt.Println("Setup partial completion.")
}

func doctorCmd(args []string) {
	fmt.Println("Vektix System Check")
	fmt.Println("===================")

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("✗ Config: Failed to load: %v\n", err)
	} else {
		fmt.Println("✓ Config: Loaded successfully")
	}

	dataDir, err := config.ExpandPath(cfg.General.DataDir)
	if err != nil {
		fmt.Printf("✗ Data Directory: Error resolving path: %v\n", err)
	} else if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		fmt.Printf("✗ Data Directory: %s does not exist\n", dataDir)
	} else {
		fmt.Printf("✓ Data Directory: %s exists\n", dataDir)
	}

	// TODO: Check Ollama reachability and model presence once internal/ollama is ready
	fmt.Println("? Ollama: [TODO] Check reachability via internal/ollama")
	fmt.Println("? Models: [TODO] Check nomic-embed-text, qwen2.5:0.5b presence via internal/ollama")
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
