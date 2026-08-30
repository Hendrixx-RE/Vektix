# Vektix

A local, privacy-first file locator with natural-language querying. You ask in plain English;
Vektix fetches, reads, opens, or copies the exact passage — verbatim, with real line numbers.
Nothing leaves your machine: every model runs locally through [Ollama](https://ollama.com).

Single static Go binary. No CGO. Go 1.23.0.

> **Status:** complete. All six phases of the design specification (`plan.md`) are implemented
> and tested: hybrid retrieval, chunking, excerpt rendering, 2-tier intent routing, pipelined
> indexing with background reconciliation and ephemeral scopes, read-only one-shot CLI commands,
> an interactive Bubble Tea TUI, and a full benchmark evaluation harness (`vektix eval`).

## What it is

Vektix indexes local documents and source files into a hybrid search index, locates them by name,
content, or vague description, and shows you the exact passage rather than a paraphrase. It never
edits, appends, deletes, or moves anything, and it never calls out to a cloud API.

### Show, don't summarise

Output is the real bytes from your files — the retrieved chunk expanded to a natural boundary
(paragraph, function, or top-level key) and printed with line numbers. No LLM-authored answers by
default. An `[e]xplain` action exists as an explicit, opt-in escape hatch in the interactive TUI
when you want prose, loading its model (`qwen2.5:3b-instruct`) only on demand.

## Core design idea

**The router decides the action. The resolver decides the target.** Keeping these separate means
most queries can complete without loading a model at all:

- **Router, Tier 1** — a guarded regex fast-path (`internal/router/fastpath.go`). A pattern like
  `^open\s+(.+)$` only fires if its captured argument passes a shape guard (looks like a path, a
  glob, or an explicit session reference via `session.IsExplicitRef`). The guard prevents an
  unguarded `^find\s+(.+)$` from turning *"find out what I wrote about docker"* into a glob of
  `out what I wrote about docker` — instant, confident, and wrong.
- **Router, Tier 2** — on a Tier-1 miss, `internal/router/llm.go` classifies intent with
  `qwen2.5:0.5b` under a full JSON Schema (`internal/router/schema.go`) passed as Ollama's `format`
  parameter, so the model's output is grammar-constrained to always be valid JSON.
- **Resolver** — for actions that need a target, three arms search in parallel
  (`internal/resolve/`): a fuzzy path/trie index, an in-memory BM25 index over content and paths,
  and a vector arm over `nomic-embed-text` embeddings in chromem-go. Results are fused by
  Reciprocal Rank Fusion.

## Hybrid retrieval

```
score(doc) = Σ_arms  1 / (rrf_k + rank_arm(doc))
```

Fusion is **rank-based, not score-based**, because BM25 scores and cosine similarities live on
incomparable scales — there's no principled way to normalize and combine them directly. RRF needs
no normalisation or tuning: each arm just contributes `1/(rrf_k + rank)`, and the arms that agree
on a document push it to the top.

Paths are first-class search targets, not just content: *"open my resume pdf"* is a path query, and
the path arm tokenises basename, stem, parent directories, and extension for fuzzy matching.

## Scoped fetching & ephemeral indexing

Launching Vektix inside a directory confines search to that subtree — a view over the
existing index, not a separate one. The path and BM25 arms filter **exactly** (`strings.HasPrefix`
on the chunk's path, restricted to in-scope documents before ranking). The vector arm cannot be
filtered that way: chromem-go's `where` clause is exact-match only, with no prefix or OR operator,
so a subtree filter isn't expressible as a `where` clause.

The workaround is adaptive oversampling with an in-memory prefix filter
(`internal/resolve/vector.go`):

```
nResults = clamp(k / max(scopeFraction, oversample_floor), k, collectionSize)
```

`scopeFraction` is an O(1) lookup against the manifest's `dir_counts` prefix tree
(`internal/index/manifest.go`). Query with `nResults`, filter the returned documents by path
prefix in Go, and if fewer than `k` survive, retry once at full `collectionSize` — an exhaustive
scan, since chromem-go is brute-force either way.

Scope resolution (`internal/resolve/scope.go`) implements the ladder: explicit `--scope` or
`--global`/`-g` override, else CWD-under-an-indexed-root, else a signal to prompt the caller
(or search globally with a clear notice in one-shot CLI mode).

### Ephemeral indexing & background reconcile

- **Ephemeral scope indexing**: When invoking search in an unindexed directory, passing `--index-now`
  in the CLI (or using `:index-here` in the TUI) indexes the directory on demand as a transient root.
- **LRU eviction policy**: Transient roots are retained for 7 days (`transient_retention_days`) up
  to a maximum of 10 transient roots (`max_transient_roots`). When `vektix sync` runs, expired or
  excess least-recently-used transient roots and their chunks are automatically purged from the
  store and manifest.
- **Background reconcile (TUI only)**: On startup, the TUI launches a non-blocking background check
  via an asynchronous Bubble Tea command; if any indexed root has changed, it reconciles the index in
  the background while keeping the query interface interactive.

## Models

| Purpose | Model | Notes |
|---|---|---|
| Embeddings | `nomic-embed-text` | 768-dim. Requires task prefixes — see below. |
| Intent (router Tier 2) | `qwen2.5:0.5b` | Schema-constrained decoding, `num_ctx=2048`. |
| Explain (opt-in) | `qwen2.5:3b-instruct` | Used by the TUI `[e]xplain` action; loaded into Ollama on demand. |

`nomic-embed-text` **requires** task prefixes — documents as `search_document: <text>`, queries as
`search_query: <text>`. Omitting them degrades retrieval silently, with no error. The prefix is
applied centrally in `internal/ollama/embed.go`'s `Embed` function and nowhere else; callers pass
raw text and an `IsQuery` bool.

## Manifest invalidation

An index manifest (`internal/index/manifest.go`) records `{embedding_model, dim, prefix_scheme,
chunker_version}` alongside the file → chunks map, the `dir_counts` prefix tree, and ephemeral
scope metadata. `CheckValidity` compares these fields and returns `ErrManifestMismatch` on any
mismatch — changing the embedding model must never silently mix incompatible vectors into one
collection. The CLI (`cmd/vektix/oneshots.go`), indexer (`internal/index/sync.go`), and TUI
(`internal/tui/app.go`) all verify manifest identity and refuse mismatched indexes with guidance
to run `vektix reindex`.

## Safety

- **Secrets denylist, enforced at read time** — `internal/fileops/safety.go`'s `ResolvePath` blocks
  `.ssh/`, `.gnupg/`, `.aws/credentials`, `*.pem`, `*.key`, and `.env*` regardless of where the path
  came from. Bypass is human-only: `allow_secrets = true` in `config.toml`, or the CLI's
  `--unsafe` flag. There is no code path for a model to set it.
- **Path confinement** — every path is resolved through `filepath.EvalSymlinks` + `filepath.Abs`
  and, when `confine_to_roots = true`, confined to the configured index roots plus the invocation
  CWD (overridable with `--unsafe`).
- **No write path** — the binary contains no code that creates, deletes, renames, or edits a user
  file. `internal/fileops` only reads and shells out to an editor / `xdg-open`. Indexing writes only
  to `data_dir` (`manifest.json`, `quarantine.json`, and chromem-go store).
- **Editor launch** — `$EDITOR` (or `cfg.General.Editor`) is tokenised with shell-quote-aware
  splitting (`internal/fileops/ops.go`'s `splitEditorCmd`) and passed to `exec.Command` as separate
  arguments, never through a shell, with a `--` separator before the path. Falls back to
  `xdg-open`, then to printing the path.
- **PDF parsing is sandboxed** — `internal/parser/pdf.go` parses each PDF in its own goroutine with
  `recover()` and respects the caller's context and timeout, so a malformed PDF (`ledongthuc/pdf` panics on
  these) cannot abort an indexing run.

## Installation

Requires Go 1.23.0+ and [Ollama](https://ollama.com) running locally.

```bash
git clone git@github.com:Hendrixx-RE/Vektix.git
cd Vektix
make build        # -> bin/vektix, version-stamped via -ldflags
# or
make install       # go install ./cmd/vektix
```

## First run

```bash
# 1. Initialize data/config directories and pull required Ollama models
vektix setup

# 2. Check system health and Ollama connectivity
vektix doctor

# 3. Index your files and documents
vektix index ~/Documents ~/notes ~/projects

# 4. Launch the interactive TUI (or run one-shot CLI commands)
vektix
```

## How to use

Vektix provides two primary workflows: an interactive terminal interface (TUI) for exploratory
search and natural-language interaction, and fast one-shot CLI commands for scripts and piping.

### Interactive TUI

Running `vektix` with no arguments when `stdout` is a terminal (TTY), or explicitly running `vektix tui`,
launches the full-screen interactive Bubble Tea interface:

```bash
vektix                         # launches TUI scoped to CWD (or global if outside indexed roots)
vektix tui --scope ~/projects  # launches TUI confined to a specific subtree
vektix tui --global            # launches TUI searching across all indexed roots
```

#### Keybinds (when the input prompt is empty)

| Key | Action | Description |
|---|---|---|
| `[o]` | Open | Open the active search result in your editor (`$EDITOR` or configured editor). |
| `[c]` | Copy | Copy the active result's verbatim excerpt (or path) to clipboard. |
| `[e]` | Explain | Stream an on-demand natural-language explanation of the passage via Ollama. |
| `[n]` | Next | Cycle forward to the next candidate match from the last search. |
| `[p]` | Prev | Cycle backward to the previous candidate match from the last search. |
| `[g]` | Toggle Global | Toggle between subtree-scoped search and global search across all roots. |
| `[tab]` | Picker | Open candidate picker modal to navigate and select from ambiguous matches. |
| `[esc]` | Dismiss / Quit | Close active picker or indexing view, or quit Vektix. |
| `Ctrl+C` | Quit | Immediate exit. |

#### Colon commands

Type `:` into the input box to run administrative and navigation commands:

| Command | Description |
|---|---|
| `:scope <path>` | Switch active search scope to `<path>` (or `:scope global` / `-g`). |
| `:global` | Switch active search scope to global. |
| `:index [dir]` | Run live indexing pass on configured roots or a specific directory. |
| `:index-here` (or `:index-transient`) | Index current working directory ephemerally as a transient root. |
| `:sync` | Re-walk indexed roots, update modified files, purge orphans, and expire LRU transient roots. |
| `:reindex` | Drop the existing collection and rebuild the index from scratch. |
| `:status` | Display active search scope and indexed chunk statistics. |
| `:clear` | Clear conversation history (capped at 200 entries) and reset session references. |
| `:help` (or `:?`) | Show interactive command and keybind reference dialog. |
| `:quit` (or `:q`, `:exit`) | Exit the TUI. |

#### Session references

The TUI maintains session context across consecutive queries (`internal/session/refs.go`), allowing
natural-language references to previous search results:

- Pronoun actions: `"open that"`, `"copy it"`, `"copy path"`, `"explain that"`
- Ordinal references: `"open the second one"`, `"copy the first one"`, `"the third one"`
- Direct index selectors: `#1`, `#2`, `2`, `last`, `"the last one"`
- Demonstrative file qualifiers: `"that pdf"`, `"the go file"`, `"that server.go"`

Changing the active scope (via `:scope`, `:global`, or `[g]`) automatically resets session references
so subsequent actions never point to stale, differently-scoped results.

#### On-demand explanation (`[e]xplain`)

Pressing `[e]` (or typing `"explain that"`) in the TUI triggers an opt-in explanation of the active
passage:
- Uses `qwen2.5:3b-instruct` (configured via `cfg.Ollama.ExplainModel`), loaded into Ollama only on demand.
- Stream idle timeout is configured via `cfg.Ollama.Timeouts.StreamIdleSeconds` (default: 30s; falls back to 60s if unset).
- Context size is controlled by `cfg.Ollama.Context.ExplainNumCtx` (default: 8192).

---

### One-shot CLI commands

Vektix commands can be run directly from the shell for fast lookups or script integration:

```bash
# Locate files by fuzzy path, keyword, or description
$ vektix locate "postgres connection"
scope: ~/projects/go/vektix (412 of 1,204 chunks) — --global searches everything
 1. pkg/db/pool.go  (path+bm25, rank 1)
 2. configs/database.json  (path, rank 2)

# Excerpt matching passages with line numbers and syntax highlighting
$ vektix excerpt "jwt authentication token"
scope: ~/projects/go/vektix (412 of 1,204 chunks) — --global searches everything
pkg/auth/jwt.go:14-26                                      (bm25+vec, rank 1)
   14 | func ValidateToken(tokenString string) (*Claims, error) {
   15 |     token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
   16 |         return jwtSecretKey, nil
   17 |     })
   18 |     if err != nil {
   19 |         return nil, err
   20 |     }
   21 |     if claims, ok := token.Claims.(*Claims); ok && token.Valid {
   22 |         return claims, nil
   23 |     }
   24 |     return nil, ErrInvalidToken
   25 | }

  open: vektix open pkg/auth/jwt.go   copy: vektix copy pkg/auth/jwt.go

# Open resolved file directly in your editor
$ vektix open "jwt validation"
opened /home/user/projects/go/vektix/pkg/auth/jwt.go

# Copy passage or path directly to system clipboard
$ vektix copy "jwt validation"
copied pkg/auth/jwt.go:14-26 (wl-copy)

# Read specific line ranges verbatim
$ vektix read --lines 14-26 pkg/auth/jwt.go

# Index an unindexed subtree ephemerally on the fly
$ vektix locate --index-now "docker compose"
```

## Configuration reference

Location: `~/.config/vektix/config.toml`. Any key you omit falls back to the default shown below
(`internal/config/config.go`'s `DefaultConfig`).

```toml
[general]
data_dir   = "~/.local/share/vektix"
editor     = ""                          # empty means fall back to $EDITOR, then xdg-open
scope_mode = "auto"                      # "auto" | "global" | "cwd"

[ollama]
host            = "http://localhost:11434"
embedding_model = "nomic-embed-text"     # 768-dim; changing this needs a reindex
intent_model    = "qwen2.5:0.5b"
explain_model   = "qwen2.5:3b-instruct"  # used on demand by the TUI [e]xplain keybind
keep_alive      = "5m"

[ollama.timeouts]
embed_batch_seconds = 180                # a large CPU embed batch can be slow
intent_seconds      = 15
stream_idle_seconds = 30                 # idle timeout, not a total wall-clock cap

[ollama.context]
intent_num_ctx  = 2048
explain_num_ctx = 8192

[index]
index_dirs               = ["~/Documents", "~/notes", "~/projects"]
extensions               = [".txt", ".md", ".pdf",
                            ".go", ".py", ".js", ".ts", ".rs", ".sh", ".c", ".java",
                            ".json", ".yaml", ".yml", ".toml"]
max_file_size_mb         = 50
follow_symlinks          = false
transient_retention_days = 7             # days to keep ephemeral scope indexes before LRU expiry
max_transient_roots      = 10            # maximum number of ephemeral roots retained

[index.exclude]
dirs  = ["node_modules", ".git", "__pycache__", ".venv", "venv", ".cache",
          ".trash", "dist", "build", "target", ".next", "vendor"]
files = ["*.min.js", "*.min.css", "*.map", "*.lock", "*.sum", "*.exe", "*.bin",
          "*.so", "*.dylib", "*.o", "*.pyc", "*.class", "*.wasm",
          "package-lock.json", "yarn.lock"]
paths = ["~/Documents/archive/old-backups"]

[search]
max_results       = 8                    # candidates kept after RRF fusion
rrf_k             = 60                   # RRF constant
min_arms          = 1                    # minimum arms a result must appear in to survive
oversample_floor  = 0.01                 # scoped vector oversampling clamp

[chunking]
max_tokens     = 256                     # maximum tokens per chunk
overlap_tokens = 50                      # token overlap between adjacent chunks
min_tokens     = 20                      # minimum tokens for a trailing chunk

[safety]
confine_to_roots = true                  # deny reads outside index_dirs + CWD
allow_secrets    = false                 # .ssh/, *.pem, .env* etc. require --unsafe
```

Hardcoded exclusions that cannot be overridden by config (`internal/index/ignore.go`): common
binary/media/compiled extensions, `.git/`/`.hg/`/`.svn/`, OS junk files, and the secrets group
(`.ssh/`, `.gnupg/`, `.aws/credentials`, `*.pem`, `*.key`, `.env*`).

## Command reference

Every subcommand `main()` dispatches to (`cmd/vektix/main.go` and `cmd/vektix/oneshots.go`):

| Command | Status | Notes |
|---|---|---|
| `tui` | Implemented | Interactive Bubble Tea interface. Default when invoked with no arguments on a TTY. Flags: `--scope`, `--global`/`-g`. |
| `setup` | Implemented | Checks Ollama reachability, pulls `embedding_model` and `intent_model` if missing, creates data/config dirs. |
| `doctor` | Implemented | Checks config load, data dir existence, Ollama reachability, required models. |
| `version` | Implemented | Prints the `-ldflags`-stamped version (or `-v`/`--version`). |
| `index <path>` | Implemented | Walks, parses, chunks, embeds in batches, and indexes files into the store. Flags: `--dry-run`, `--exclude`. |
| `locate <query>` | Implemented | Fast model-free search (fuzzy path + BM25, vector fallback); prints ranked files. Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`, `--limit`. |
| `read <path\|query>` | Implemented | Prints verbatim file content or line range (`path:A-B` or `--lines A-B`). Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`. |
| `excerpt <query>` | Implemented | Natural boundary passage retrieval with line numbers, ANSI highlighting, and non-ASCII alignment. Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`, `--no-color`, `--limit`. |
| `open <path\|query>` | Implemented | Resolves target and launches `$EDITOR`/`cfg.General.Editor` or `xdg-open`. Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`. |
| `copy [path] <target>` | Implemented | Copies excerpt or path to clipboard via `wl-copy` → `xclip` → `xsel` → OSC 52. Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`. |
| `list [path]` | Implemented | Lists directory contents with file sizes and indexed chunk counts. Flags: `--scope`, `--global`/`-g`, `--json`, `--unsafe`, `--index-now`. |
| `sync [paths...]` | Implemented | Re-walks roots, indexes added/changed files, purges orphaned chunks of deleted or excluded files, and evicts expired LRU transient roots. |
| `reindex [paths...]` | Implemented | Drops existing chunks under roots and rebuilds the collection from scratch. |
| `status` | Implemented | Displays data directory, chunk/file counts, model identity, last sync timestamp, indexed roots, active scope, and quarantine list (text or `--json`). |
| `eval` | Implemented | Runs intent classification or locate retrieval benchmark evaluations against dataset files (`--dataset <path>`). Flags: `--dataset`, `--corpus`, `--data-dir`, `--json`, `--limit`. |

All one-shot commands (`locate`, `read`, `excerpt`, `open`, `copy`, `list`, `status`, `eval`) support `--json`
for pipe-clean machine-readable automation with zero ANSI escape codes on `stdout`.

## Evaluation harness

Vektix includes a built-in evaluation framework (`internal/eval`) for measuring intent classification
and hybrid retrieval accuracy:

```bash
# Run locate retrieval benchmarks against testdata/locate_eval.jsonl using testdata/corpus/
vektix eval

# Run intent classification evaluation against testdata/intent_eval.jsonl
vektix eval --dataset testdata/intent_eval.jsonl

# Output metrics in JSON format for automated evaluation pipelines
vektix eval --json
```

- **Auto-detection**: `vektix eval` inspects the dataset schema to determine whether it contains intent
  classification queries (`input`, `expected`, `tier`) or locate retrieval queries (`query`, `expected_files`, `tier`).
- **Isolated test corpus**: Locate evaluations index `testdata/corpus/` (~20 representative source, markdown, and config files)
  into an isolated temporary store, ensuring benchmarks run independently of user data.
- **Strict requirements**: Locate evaluations require a live Ollama service (`nomic-embed-text`); if indexing fails or produces
  zero chunks, the evaluation fails immediately with a clear error rather than reporting misleading 0% metrics.
- **Metrics reported**:
  - **Intent**: Action Accuracy, Parameter Accuracy, Tier 1 Fast-Path Accuracy, Tier 2 LLM Accuracy, and Confusion Matrix.
  - **Locate**: Hit@1, Hit@3, Hit@5, MRR@5, Mean Latency, and Arm Breakdown (Path, BM25, Vector, Fused).

## Project status

Mapped against `plan.md`'s six phases:

- **Phase 1 — Foundation & onboarding: done.** Config loading/defaults, Ollama HTTP client with
  per-type timeouts and explicit `num_ctx`, context budgeting (`internal/ollama/budget.go`),
  `vektix setup`, `vektix doctor`, `vektix version`.
- **Phase 2 — Index: done.** Symlink-safe walker with binary sniffing and cycle detection
  (`internal/index/walk.go`), three-layer exclusion system (`internal/index/ignore.go`), text and
  panic-sandboxed PDF parsers (`internal/parser/`), prose/code/structured chunkers with full config
  threading (`internal/chunker/`), chromem-go store wrapper (`internal/store/`), full walk → parse →
  chunk → embed → store pipeline (`internal/index/sync.go`), batched embedding calls (up to 100 texts/call),
  quarantine tracking (`quarantine.json`), background reconciliation, ephemeral scope indexing with
  LRU expiry, and manifest maintenance with the `dir_counts` prefix tree. `vektix index`, `sync`,
  and `reindex` are fully operational.
- **Phase 3 — Resolve, scope & excerpt: done.** Fuzzy path/trie index (`internal/resolve/paths.go`),
  BM25 index (`internal/resolve/bm25.go`), vector arm with adaptive oversampling (`internal/resolve/vector.go`),
  RRF fusion (`internal/resolve/fuse.go`), scope resolution ladder (`internal/resolve/scope.go`),
  natural boundary excerpt expansion (`internal/excerpt/expand.go`), ANSI rendering with display width
  runewidth calculations, tab expansion, PDF page gutter rendering, and `--no-color` support with non-TTY
  auto-detection (`internal/excerpt/render.go`), and path confinement / secrets denylist safety enforcement (`internal/fileops/safety.go`).
  Fully wired to CLI one-shot commands (`locate`, `read`, `excerpt`, `open`, `copy`, `list`) and the TUI.
- **Phase 4 — Router: done.** Guarded regex fast-path (Tier 1) with shape guards and session reference
  detection reuse (`internal/router/fastpath.go`), 100+ hijack regression tests across all patterns
  (`internal/router/router_test.go`), schema-constrained Tier-2 classification via `qwen2.5:0.5b`
  (`internal/router/llm.go`, `internal/router/schema.go`), and clipboard fallback chain (`internal/clipboard/copy.go`).
  Fully integrated into the TUI query loop for automatic action routing (`open`, `read`, `list`, `locate`, `copy`, `excerpt`).
- **Phase 5 — TUI: done.** Interactive Bubble Tea TUI (`internal/tui/`), query loop with 2-tier router
  integration, verbatim excerpt rendering, ambiguous candidate picker (`[tab]`), active scope status bar,
  indexing progress card with `bubbles/spinner` and live stats, on-demand passage explanation (`[e]` via `explain_model`),
  next/prev match cycling (`[n]`/`[p]`), capped chat history window (200 entries), batch chunk loading via `store.GetByIDs`,
  and session ordinal reference resolution.
- **Phase 6 — Sync & polish: done.** `internal/index/sync.go` (reconciliation pass, orphan chunk purging,
  ephemeral scope LRU expiry, quarantine persistence) and `internal/session/refs.go` (session ordinal
  references with scope-change invalidation) are complete. `internal/eval/` (`runner.go`, `metrics.go`),
  `testdata/corpus/`, `testdata/locate_eval.jsonl`, `testdata/intent_eval.jsonl`, and the `vektix eval` CLI command
  are fully implemented.

## Development tooling

`scripts/` holds dev-only tools for generating datasets:

- `scripts/gen_cases.py` generates `testdata/intent_eval.jsonl`. Run with `python3 scripts/gen_cases.py` from the repo root.
- `scripts/eval_intent.go` is a legacy standalone harness that runs `testdata/intent_eval.jsonl` through the router.
  It is largely superseded by the built-in CLI command: `vektix eval --dataset testdata/intent_eval.jsonl`.

## Build & test

```bash
go build ./...
go vet ./...
go test ./... -race -count=1   # Ollama is mocked over httptest; no live service required
```

See [AGENTS.md](AGENTS.md) for repo layout, hard rules, and invariants if you're contributing.
