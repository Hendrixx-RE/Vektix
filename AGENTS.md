# AGENTS.md

Guidance for an AI agent or new contributor working in this repo. `plan.md` is the authoritative
design spec (v3) — read it first. This file documents what actually exists in the tree today, the
rules that keep it correct, and the mistakes already made once so they aren't made again.

For user-facing docs (what Vektix is, how to build/run it, config keys, command status, project
status by phase), see [README.md](README.md). This file is contributor-facing.

## Repo layout

```
cmd/vektix/main.go      CLI entry point. setup/doctor/version are implemented; every other
                         subcommand (index, locate, read, excerpt, open, copy, list, sync,
                         status, eval) is a stub that prints "not yet implemented".
internal/
  config/                TOML config + defaults (DefaultConfig, Load, ExpandPath). No wiring
                         issues; every field maps 1:1 to a config.toml key.
  ollama/                HTTP client for Ollama's local API.
    client.go             Client + per-call-type timeouts (embed/intent/stream-idle).
    chat.go               Chat + ChatStream. checkRequiredOptions enforces num_ctx, num_predict,
                         temperature, seed on every call — see Hard rules.
    embed.go               Embed. Applies the search_document:/search_query: prefix — see Hard
                         rules. Never add the prefix anywhere else.
    budget.go               EstimateTokens (runes/4) and EnforceBudget, which drops the
                         lowest-ranked chunks to fit num_ctx.
    cache.go               LRU cache for query embeddings.
  index/
    walk.go               Symlink-safe walker: cycle detection via (dev, inode), binary sniff
                         (first 8KB, NUL byte or invalid UTF-8 rejected), extension + size
                         filtering.
    ignore.go               Three-layer exclusion: hardcoded > config excludes > .vektixignore,
                         evaluated in that order; .vektixignore rules are gitignore-style
                         (last matching rule wins, negation supported).
    manifest.go             Manifest struct + CheckValidity (embedding_model/dim/prefix_scheme/
                         chunker_version mismatch → ErrManifestMismatch), HasChanged
                         (mtime+size, SHA256 tiebreaker), ScopeFraction over dir_counts.
                         Nothing populates dir_counts yet — there's no indexing pipeline to
                         call these from.
    sync.go               Empty stub (package index only).
  chunker/
    dispatch.go             Chunk(path, content) dispatches by extension. Takes no config
                         argument — see Hard rules, [chunking] is dead.
    text.go               Prose chunker: sentence/paragraph/heading-aware, with overlap.
                         Hardcodes maxTokens=256, overlapTokens=50, minTokens=20.
    code.go               go/ast-based chunking for .go (one chunk per top-level Decl,
                         signature captured as Locator.Symbol); regex-heuristic chunking for
                         other languages. Oversized decls fall back to ChunkText windowing
                         with the signature re-prepended.
    structured.go           JSON/YAML/TOML: splits on top-level keys via regex, keeping the
                         key name as a chunk prefix.
  store/
    store.go               Thin wrapper over chromem-go: NewPersistentDB, AddDocuments,
                         QueryEmbedding, Delete, Count.
    document.go             Chunk type + Locator (LineRange | Page | Symbol) +
                         EncodeMetadata/DecodeMetadata. chromem-go metadata is
                         map[string]string, so this is the one place that (de)serializes
                         Locator fields — never stringify a Locator field ad hoc elsewhere.
  resolve/
    paths.go               Fuzzy path/trie arm (github.com/sahilm/fuzzy), scope-filtered by
                         exact prefix before fuzzy matching.
    bm25.go               In-memory BM25 (k1=1.5, b=0.75) over "path + content", scope-
                         filtered by exact prefix per posting.
    vector.go               chromem-go query with the LRU cache and adaptive oversampling
                         (see Invariants below).
    fuse.go               Reciprocal Rank Fusion: score = Σ 1/(rrf_k + rank), 1-indexed rank,
                         min_arms threshold, truncate to max_results.
    scope.go               ResolveScope: override > global > CWD-under-a-root > prompt signal.
  excerpt/
    expand.go               Grows a chunk to a natural boundary (paragraph/struct-key/Go decl
                         via go/ast/heuristic-decl for other code) before rendering, budgeted
                         by MaxLines.
    render.go               Line numbers, gutter, ANSI highlight of the matched span. Has
                         known rendering bugs — see README's Known gaps. Don't build on top of
                         it without fixing those first if the fix is in scope.
  fileops/
    safety.go               ResolvePath: EvalSymlinks + Abs, secrets denylist, root
                         confinement. The single choke point every path must go through.
    ops.go               ReadFile, Open (shells out with argv-array exec.Command, never a
                         shell string), splitEditorCmd (shell-quote-aware tokenizer).
  clipboard/copy.go       wl-copy → xclip → xsel → OSC 52 fallback chain.
  router/
    fastpath.go             Guarded regex Tier 1 + shape guards (pathShaped, globShaped,
                         pathShapedOrRef). Hijack regression cases live in
                         testdata/intent_eval.jsonl.
    llm.go               Tier 2: schema-constrained qwen2.5:0.5b classification via
                         ollama.Client.Chat.
    schema.go               The JSON Schema passed as Ollama's format parameter.
  parser/
    text.go               Line-numbered plain text/markdown parsing.
    pdf.go               ledongthuc/pdf, isolated in a goroutine with recover() and context-
                         aware cancellation — a malformed PDF must never abort a run.
  session/refs.go         Empty stub.
  eval/{runner,metrics}.go   Empty stubs. The real interim intent-eval tool is
                         scripts/eval_intent.go (see README's Development tooling).
  tui/*.go               Every file is a one-line package stub. Bubble Tea/Lip Gloss/Bubbles/
                         Glamour are not in go.mod yet.
scripts/                 Dev-only tooling, not part of the vektix binary. gen_cases.py
                         generates testdata/intent_eval.jsonl; eval_intent.go runs it against
                         a live Ollama.
testdata/intent_eval.jsonl   157 cases, only 19 tier-1. No locate_eval.jsonl or corpus/ yet.
```

## Hard rules

- **`go.mod` must stay `go 1.23.0`.** `go mod tidy` has bumped this to `1.24.x` at least twice in
  this repo's history (see Bug history). If it happens again, revert the `go` line by hand and
  re-run `go mod tidy` to confirm it doesn't re-bump against the currently resolved dependency
  graph.
- **Embedding prefixes are applied only in `internal/ollama/embed.go`.** `Embed` prepends
  `search_document: ` or `search_query: ` based on `EmbedRequest.IsQuery`. Callers pass raw,
  unprefixed text. Never add the prefix at a call site — doing it twice or in the wrong place
  degrades retrieval silently, with no error surfaced anywhere.
- **`internal/ollama/chat.go`'s `checkRequiredOptions` enforces that `num_ctx`, `num_predict`,
  `temperature`, and `seed` are all explicitly present in `ChatRequest.Options` on every call to
  `Chat` and `ChatStream`.** Do not weaken or bypass this. Ollama truncates an over-long prompt
  from the *head*, silently dropping the system prompt and schema first — leaving the model
  well-formed-looking but uninstructed. An explicit, deliberate `num_ctx` is the only guard against
  that failure mode, and it's currently paired with `internal/ollama/budget.go`'s `EnforceBudget`
  for callers that assemble multi-chunk prompts.
- **Never commit stray files at the repo root.** `scratch.go`, `*.bak`, test PDFs, and untracked
  `testdata/` fixtures have each been caught pre-commit before. Dev-only tooling belongs in
  `scripts/` (see `eval_intent.go` / `gen_cases.py`), not the repo root.
- **`gh pr create` needs both `--title` and `--body`**, or it fails non-interactively.
- **`internal/chunker`'s `[chunking]` config is currently dead** (see README's Known gaps). If you
  wire it up, thread it through `chunker.Chunk`/`ChunkText`/`ChunkCode`/`ChunkStructured` rather
  than reading global config from inside the package — none of those functions take a config
  argument today, and adding a package-level config read would make them harder to test in
  isolation (every existing chunker test constructs inputs directly, with no config fixture).

## Build, test, lint

```bash
go build ./...
go vet ./...
go test ./... -race -count=1
```

Ollama is mocked over `httptest` in `internal/ollama/ollama_test.go` — the full suite runs with no
live services. `scripts/eval_intent.go` is the one thing in this repo that needs a real, running
Ollama with `qwen2.5:0.5b` pulled; it is not part of `go test`.

No CI is configured yet. **Suggested addition:** a check that greps `go.mod` for `^go 1\.23\.0$`
and fails otherwise, given the repeated accidental bumps below.

## Invariants a change must not break

- **RRF is rank-based, not score-based, by design.** `internal/resolve/fuse.go` never compares
  BM25 scores to cosine similarities directly — they're on incomparable scales. If you touch
  `Fuse`, keep the `1/(rrf_k + rank)` formula and 1-indexed ranks per arm.
- **`min_arms` in `Fuse` is a flat threshold on arm count** (`sc.Arms >= minArms`), not the
  rank-conditional rule plan.md's prose describes ("a result appearing in only one arm at low rank
  is dropped"). If you change this to be rank-aware, update both `plan.md`'s wording and this note
  — right now the code is simpler than the spec text and that's a known, accepted simplification,
  not a bug.
- **Scoping must stay exact for the path and BM25 arms.** Both filter with `strings.HasPrefix` on
  the full chunk path before ranking — never after, and never approximately. The vector arm is the
  one arm allowed to be lossy, because it's structurally incapable of an exact `where` push-down
  (chromem-go's `where` is AND-of-equalities only, no prefix/OR operator). If chromem-go ever adds
  one, `internal/resolve/vector.go`'s oversampling logic should be replaced with a real filter, not
  kept alongside it.
- **The oversampling clamp is `nResults = clamp(k / max(scopeFraction, oversampleFloor), k,
  collectionSize)`, with one exhaustive retry at `collectionSize` if fewer than `k` results survive
  the scope filter.** Don't drop the retry — without it, a low `scopeFraction` estimate silently
  returns too few results instead of falling back to a full scan.
- **The manifest is the sole source of truth for index validity.** `CheckValidity` must keep
  comparing all four of `{embedding_model, dim, prefix_scheme, chunker_version}` — a mismatch on
  any one of them means vectors of different provenance could be sitting in the same collection.
  Never make this check advisory (e.g., log-and-continue); it exists to refuse, not warn.
- **No write path outside `internal/store` and `internal/index`, and those only write into
  `data_dir`.** `internal/fileops`, `internal/excerpt`, and `internal/clipboard` must never gain an
  `os.Create`/`os.WriteFile`/`os.Remove`/`os.Rename` call. Vektix is read-only by construction; this
  is meant to be enforced by a CI grep per plan.md, but that CI doesn't exist yet (see Build/test
  above) — until it does, this is enforced by review only.
- **`--unsafe` can only originate from CLI flag parsing**, never from a model output or a value
  the router/resolver constructs. `internal/fileops/safety.go`'s `ResolvePath` takes
  `explicitUnsafe bool` as a plain parameter for exactly this reason — keep it that way rather than,
  say, threading it through `config.Config` where an LLM-influenced code path could plausibly reach
  it.

## Bug history (don't reintroduce these)

- **`ledongthuc/pdf` was mislabeled `// indirect` in `go.mod`** even though `internal/parser`
  imports it directly (fixed in commit `03cd105`). If `go mod tidy` ever re-adds that comment to a
  package actually imported by name in the tree, treat it as a tidy bug and remove the comment by
  hand.
- **The Go directive has been bumped past `1.23.0` twice** by `go mod tidy` reacting to a
  dependency's own `go.mod` floor (commits `1bf972c`, `a38043c`) — both times the fix was to pin
  `go 1.23.0` back and, where needed, downgrade the offending dependency rather than raise the
  floor. If `go mod tidy` wants to bump the directive again, look at which dependency is forcing it
  before accepting the bump.
- **Secrets/`--unsafe` semantics were previously unclear about who can set the bypass** (cleaned up
  in commit `6ff633e`). The rule, restated: `allow_secrets = true` in `config.toml` and the CLI's
  `--unsafe` flag are the only two ways past the secrets denylist, both human-controlled, both
  outside any path an LLM output touches.
