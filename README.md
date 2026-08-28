# Vektix

A local, privacy-first file locator with natural-language querying. You ask in plain English;
Vektix fetches, reads, opens, or copies the exact passage — verbatim, with real line numbers.
Nothing leaves your machine: every model runs locally through [Ollama](https://ollama.com).

Single static Go binary. No CGO. Go 1.23.0.

> **Status:** early. The core retrieval, router, chunking, excerpt and safety packages are
> implemented and unit-tested (see [Project status](#project-status)), but they are not yet wired
> into a working CLI or TUI. Almost every `vektix` subcommand besides `setup`, `doctor` and
> `version` is currently a stub. See [Known gaps](#known-gaps) before relying on this.

## What it is

Vektix indexes local documents and source files into a hybrid search index, locates them by name,
content, or vague description, and shows you the exact passage rather than a paraphrase. It never
edits, appends, deletes, or moves anything, and it never calls out to a cloud API.

### Show, don't summarise

Output is the real bytes from your files — the retrieved chunk expanded to a natural boundary
(paragraph, function, or top-level key) and printed with line numbers. No LLM-authored answers by
default. An `[e]xplain` action is meant to exist as an explicit, opt-in escape hatch when you want
prose, loading its model only on demand — this is planned but not implemented yet.

## Core design idea

**The router decides the action. The resolver decides the target.** Keeping these separate means
most queries can complete without loading a model at all:

- **Router, Tier 1** — a guarded regex fast-path (`internal/router/fastpath.go`). A pattern like
  `^open\s+(.+)$` only fires if its captured argument passes a shape guard (looks like a path, a
  glob, or a session reference). The guard exists because an unguarded `^find\s+(.+)$` would turn
  *"find out what I wrote about docker"* into a glob of `out what I wrote about docker` — instant,
  confident, and wrong.
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

## Scoped fetching

Launching Vektix inside a directory is meant to confine search to that subtree — a view over the
existing index, not a separate one. The path and BM25 arms filter **exactly** (`strings.HasPrefix`
on the chunk's path, restricted to in-scope documents before ranking). The vector arm cannot be
filtered that way: chromem-go's `where` clause is exact-match only, with no prefix or OR operator,
so a subtree filter isn't expressible as a `where` clause.

The workaround is adaptive oversampling with an in-memory prefix filter
(`internal/resolve/vector.go`):

```
nResults = clamp(k / max(scopeFraction, oversample_floor), k, collectionSize)
```

`scopeFraction` is meant to be an O(1) lookup against the manifest's `dir_counts` prefix tree
(`internal/index/manifest.go`). Query with `nResults`, filter the returned documents by path
prefix in Go, and if fewer than `k` survive, retry once at full `collectionSize` — an exhaustive
scan, since chromem-go is brute-force either way.

Scope resolution itself (`internal/resolve/scope.go`) implements the ladder: explicit `--scope` or
`--global` override, else CWD-under-an-indexed-root, else a signal to prompt the caller. The CLI
flags and TUI status bar that are supposed to surface this are not wired up yet — see
[Known gaps](#known-gaps).

## Models

| Purpose | Model | Notes |
|---|---|---|
| Embeddings | `nomic-embed-text` | 768-dim. Requires task prefixes — see below. |
| Intent (router Tier 2) | `qwen2.5:0.5b` | Schema-constrained decoding, `num_ctx=2048`. |
| Explain (opt-in) | `qwen2.5:3b-instruct` | Not implemented yet; pulled on demand only when built. |

`nomic-embed-text` **requires** task prefixes — documents as `search_document: <text>`, queries as
`search_query: <text>`. Omitting them degrades retrieval silently, with no error. The prefix is
applied centrally in `internal/ollama/embed.go`'s `Embed` function and nowhere else; callers pass
raw text and an `IsQuery` bool.

## Manifest invalidation

An index manifest (`internal/index/manifest.go`) records `{embedding_model, dim, prefix_scheme,
chunker_version}` alongside the file → chunks map and the `dir_counts` prefix tree. `CheckValidity`
compares these fields and returns `ErrManifestMismatch` on any mismatch — changing the embedding
model must never silently mix incompatible vectors into one collection. Nothing in the tree yet
calls `CheckValidity` from a live code path (no indexing pipeline exists to invoke it from); the
check itself is implemented and unit-tested.

## Safety

- **Secrets denylist, enforced at read time** — `internal/fileops/safety.go`'s `ResolvePath` blocks
  `.ssh/`, `.gnupg/`, `.aws/credentials`, `*.pem`, `*.key`, and `.env*` regardless of where the path
  came from. Bypass is human-only: `allow_secrets = true` in `config.toml`, or the CLI's
  `--unsafe` flag (not yet wired to any subcommand). There is no code path for a model to set it.
- **Path confinement** — every path is resolved through `filepath.EvalSymlinks` + `filepath.Abs`
  and, when `confine_to_roots = true`, confined to the configured index roots plus the invocation
  CWD.
- **No write path** — the binary contains no code that creates, deletes, renames, or edits a user
  file. `internal/fileops` only reads and shells out to an editor / `xdg-open`.
- **Editor launch** — `$EDITOR` (or `cfg.General.Editor`) is tokenised with shell-quote-aware
  splitting (`internal/fileops/ops.go`'s `splitEditorCmd`) and passed to `exec.Command` as separate
  arguments, never through a shell, with a `--` separator before the path. Falls back to
  `xdg-open`, then to printing the path.
- **PDF parsing is sandboxed** — `internal/parser/pdf.go` parses each PDF in its own goroutine with
  `recover()` and respects the caller's context, so a malformed PDF (`ledongthuc/pdf` panics on
  these) cannot abort a run.

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
vektix setup   # checks Ollama is reachable, pulls nomic-embed-text and qwen2.5:0.5b if missing,
               # creates ~/.local/share/vektix and a default ~/.config/vektix/config.toml
vektix doctor  # checks config load, data dir, Ollama reachability, and required models
```

Both are fully implemented today; everything past this point in the CLI is a stub (see
[Command reference](#command-reference)).

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
explain_model   = "qwen2.5:3b-instruct"  # not yet used anywhere in the tree
keep_alive      = "5m"

[ollama.timeouts]
embed_batch_seconds = 180                # a large CPU embed batch can be slow
intent_seconds      = 15
stream_idle_seconds = 30                 # idle timeout, not a total wall-clock cap

[ollama.context]
intent_num_ctx  = 2048
explain_num_ctx = 8192

[index]
index_dirs       = ["~/Documents", "~/notes", "~/projects"]
extensions       = [".txt", ".md", ".pdf",
                    ".go", ".py", ".js", ".ts", ".rs", ".sh", ".c", ".java",
                    ".json", ".yaml", ".yml", ".toml"]
max_file_size_mb = 50
follow_symlinks  = false

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
max_tokens     = 256                     # DEAD — see Known gaps, has no effect
overlap_tokens = 50                      # DEAD — see Known gaps, has no effect
min_tokens     = 20                      # DEAD — see Known gaps, has no effect

[safety]
confine_to_roots = true                  # deny reads outside index_dirs + CWD
allow_secrets    = false                 # .ssh/, *.pem, .env* etc. require --unsafe
```

Hardcoded exclusions that cannot be overridden by config (`internal/index/ignore.go`): common
binary/media/compiled extensions, `.git/`/`.hg/`/`.svn/`, OS junk files, and the secrets group
(`.ssh/`, `.gnupg/`, `.aws/credentials`, `*.pem`, `*.key`, `.env*`).

## Command reference

Every subcommand `main()` dispatches to (`cmd/vektix/main.go`):

| Command | Status | Notes |
|---|---|---|
| `setup` | Implemented | Checks Ollama, pulls `embedding_model` and `intent_model` if missing, creates data/config dirs. |
| `doctor` | Implemented | Checks config load, data dir existence, Ollama reachability, required models. |
| `version` | Implemented | Prints the `-ldflags`-stamped version. |
| `index <path>` | Stub | Flags `--dry-run`, `--exclude` are parsed but not acted on. |
| `locate` | Stub | Flags `--scope`, `--global`/`-g` are parsed but not acted on. |
| `read`, `excerpt`, `open`, `copy`, `list` | Stub | Print `not yet implemented` with the parsed args. |
| `sync` | Stub | No indexing pipeline exists yet to reconcile against. |
| `status` | Stub | |
| `eval` | Stub | Flag `--dataset` is parsed but not acted on; `internal/eval` is an empty package. See [scripts/](#development-tooling) for the interim way to run the intent eval. |

None of these stub commands touch `internal/resolve`, `internal/chunker`, `internal/excerpt`, or
`internal/store` — those packages are implemented and unit-tested in isolation but nothing in
`cmd/vektix` calls into them yet.

## Project status

Mapped against `plan.md`'s six phases:

- **Phase 1 — Foundation & onboarding: done.** Config loading/defaults, the Ollama HTTP client with
  per-type timeouts and explicit `num_ctx`, context budgeting (`internal/ollama/budget.go`),
  `vektix setup`, `vektix doctor`.
- **Phase 2 — Index: partially done.** The individual building blocks are implemented and tested —
  symlink-safe walker with binary sniffing and cycle detection (`internal/index/walk.go`), the
  three-layer `.vektixignore`/config/hardcoded exclusion system (`internal/index/ignore.go`),
  text and panic-sandboxed PDF parsers (`internal/parser/`), the prose/code/structured chunkers
  (`internal/chunker/`), the chromem-go store wrapper (`internal/store/`), and the manifest type
  with `dir_counts`. **Missing:** nothing wires walk → parse → chunk → embed → store into an actual
  pipeline, batched embedding calls are not orchestrated, and `vektix index` is a stub. The
  manifest's `dir_counts` is never populated by anything in the tree today.
- **Phase 3 — Resolve, scope & excerpt: mostly done as a library.** Path/trie fuzzy index, BM25,
  vector arm with adaptive oversampling, RRF fusion, scope resolution, excerpt expansion
  (paragraph/symbol/key boundaries) and rendering, path confinement and the secrets denylist are
  all implemented with tests. **Missing:** none of it is reachable from the CLI — no `locate`,
  `read`, `excerpt`, `open`, `copy`, or `list` command actually calls these packages.
- **Phase 4 — Router: done as a library, not wired up.** Guarded regex fast-path with hijack
  regression fixtures, schema-constrained Tier-2 classification. Clipboard
  (`wl-copy`→`xclip`→`xsel`→OSC 52) is implemented. **Missing:** the CLI never calls the router; the
  only caller today is `scripts/eval_intent.go` and the test suite.
- **Phase 5 — TUI: not started.** Every file under `internal/tui/` is a one-line `package tui`
  stub. The Bubble Tea / Lip Gloss / Bubbles / Glamour dependencies aren't even in `go.mod` yet.
- **Phase 6 — Sync & polish: not started.** `internal/index/sync.go` and `internal/session/refs.go`
  are one-line stubs. `internal/eval/` (`runner.go`, `metrics.go`) is an empty package — the real
  intent eval currently lives in `scripts/eval_intent.go` instead. No `locate_eval.jsonl` or
  `testdata/corpus/` exist yet, only `testdata/intent_eval.jsonl`.

## Known gaps

Documented honestly because they're real and currently open:

1. **The `[chunking]` config block is dead.** `internal/chunker/text.go` hardcodes
   `const (maxTokens = 256; overlapTokens = 50; minTokens = 20)`, and the dispatch entry point
   `chunker.Chunk(path, content string)` (`internal/chunker/dispatch.go`) takes no config argument
   at all. Editing `[chunking]` in `config.toml` has zero effect on chunking behavior.
2. **`internal/excerpt/render.go` has three rendering bugs.** It emits ANSI highlight codes
   (`\x1b[33m`) unconditionally, with no non-TTY or `--no-color` guard. It does not expand tabs
   before computing gutter alignment. It pads the header to a target width using `len(headerLeft)`
   — a byte length, not a display width — so any non-ASCII path misaligns the right-aligned rank
   info. Separately, the `LocatorPage` case renders a **blank** gutter (`fmt.Sprintf(" %*s | ",
   gutterWidth, "")`) instead of a page number, since PDF chunks don't carry line numbers.
3. **`testdata/intent_eval.jsonl` is skewed.** It has 157 cases, but only 19 are tier-1
   (fast-path); the remaining 138 are tier-2, so fast-path guard regressions have thin coverage.

## Development tooling

`scripts/` holds two dev-only tools, not part of the `vektix` binary:

- `scripts/gen_cases.py` generates `testdata/intent_eval.jsonl`. Run with `python3
  scripts/gen_cases.py` from the repo root.
- `scripts/eval_intent.go` is a standalone `package main` that runs every case in
  `testdata/intent_eval.jsonl` through the real router (Tier 1 fast-path, falling through to a
  **live** Tier 2 LLM call) and prints action/parameter accuracy. It requires Ollama running
  locally with `qwen2.5:0.5b` pulled. Run with `go run ./scripts/eval_intent.go` from the repo
  root (it opens `testdata/intent_eval.jsonl` via a relative path). This exists because
  `internal/eval` and `vektix eval` are not implemented yet.

## Build & test

```bash
go build ./...
go vet ./...
go test ./... -race -count=1   # Ollama is mocked over httptest; no live service required
```

See [AGENTS.md](AGENTS.md) for repo layout, hard rules, and invariants if you're contributing.
