# Vektix — Implementation Plan (v3)

> A local, privacy-first file locator with natural-language querying and Linux desktop integration.
> No external AI calls. Runs entirely on your machine. Read-only — never modifies your files.

---

## Goal

Build **Vektix**, a terminal application that already knows where everything is, so you never have to
go looking yourself. You ask in plain English; Vektix **fetches, reads, opens, or copies**.

1. **Indexes** local documents and source files into a hybrid search index
2. **Locates** files by name, content, or vague description
3. **Shows you the exact passage** — verbatim, with line numbers and a citation
4. **Opens** it in your editor or **copies** it to your clipboard

### Design principle: show, don't summarise

Vektix returns **the real bytes from your files**, never a paraphrase. The retrieved passage is
expanded to a natural boundary and printed with line numbers. A model that rewrites your notes
can quietly get them wrong, and a tool whose entire job is knowing exactly where what is cannot
afford that. An LLM is used only to *understand your phrasing* — never to author an answer.

A `[e]xplain` action exists as an explicit, opt-in escape hatch when you want prose. It is never
the default and it loads its model on demand.

### What Vektix does NOT do

- ❌ No editing, appending, deleting, or moving files
- ❌ No LLM-authored answers by default — excerpts are verbatim
- ❌ No external API calls — everything local
- ❌ No cloud, no telemetry, no data leaving your machine

---

## Architecture

**The router decides the *action*. The resolver decides the *target*.** Keeping these separate is
what lets the large majority of queries complete without loading a model at all.

```
input
  │
  ├─ ROUTER ── guarded regex (fires only on path-shaped args)   ~1ms
  │              └─ miss ─► qwen2.5:0.5b, JSON-Schema-constrained  ~300ms
  │                          → {action, query, path?, lines?}
  │
  └─ RESOLVER (only for actions that need a target)
        │   SCOPE = CWD subtree | --scope <path> | global
        │
        ├─ path index  (basename / stem / dir, fuzzy)   ~1ms   no model   ← exact scope filter
        ├─ BM25        (content + paths)                ~10ms  no model   ← exact scope filter
        └─ vector      (nomic-embed-text → chromem-go)  ~20ms  embedder   ← oversample + filter
                          │
                    RRF fusion → ranked candidates
                          │
        ┌─────────────────┴─────────────────┐
   unambiguous                          ambiguous
        │                                   │
      execute                     TUI picker → execute
        │
   locate │ read │ excerpt │ open │ copy │ list
                          │
                    [e]xplain (opt-in) ─► qwen2.5:3b, loaded on demand
```

```
┌─────────────────────────────────────────────────────────┐
│                Go Binary (Bubble Tea TUI)               │
│   router  ·  resolve  ·  excerpt  ·  fileops  ·  index  │
│                                                         │
│   ┌────────────────┐  ┌──────────┐  ┌───────────────┐   │
│   │  chromem-go    │  │  BM25 +  │  │ Ollama client │   │
│   │  (vectors)     │  │  paths   │  │ (HTTP local)  │   │
│   └────────────────┘  └──────────┘  └───────┬───────┘   │
└──────────────────────────────────────────────┼──────────┘
                                               │ localhost:11434
┌──────────────────────────────────────────────▼──────────┐
│                   Ollama (local service)                │
│   nomic-embed-text (embed)  ·  qwen2.5:0.5b (intent)    │
│   qwen2.5:3b (explain — pulled and loaded on demand)    │
└─────────────────────────────────────────────────────────┘
```

**No Python. Single Go binary. No CGO. Ollama handles all ML.**

---

## Tech Stack

| Component | Choice | Why |
|-----------|--------|-----|
| **Language** | Go 1.22+ | Single static binary, fast |
| **TUI** | Bubble Tea + Lip Gloss + Bubbles | Elm architecture, async streaming |
| **Vector store** | chromem-go | Pure Go, zero CGO, embedded |
| **Lexical index** | in-process BM25 | Exact tokens: identifiers, error codes, filenames |
| **Embeddings** | `nomic-embed-text` (137M, 768-dim) | 274 MB, fast on CPU, Apache 2.0 |
| **Intent LLM** | `qwen2.5:0.5b` | 380 MB, JSON-Schema-constrained decoding |
| **Explain LLM** | `qwen2.5:3b-instruct` | Opt-in only, loaded on demand |
| **Go code parsing** | stdlib `go/parser` + `go/ast` | Exact, zero dependencies, no CGO |
| **PDF parsing** | `ledongthuc/pdf` (pure Go) | Sandboxed behind `recover()` — see Safety |
| **Config** | BurntSushi/toml | Simple, readable |
| **IPC** | HTTP to localhost:11434 | Ollama's built-in API |

### Why not bge-m3

v2 specified bge-m3. It is a genuinely strong model, but at 568M params it is 1.2 GB on disk and
slow to index on CPU — 5–8 minutes for 5k chunks versus 40–60 seconds for `nomic-embed-text`.
Because BM25 carries exact-token matching in a hybrid design, the embedder only has to handle
fuzzy phrasing, and the smaller model is more than sufficient for that. Reconsider only if the
eval shows scoped `locate` recall@3 below 80%.

> ⚠️ **`nomic-embed-text` requires task prefixes.** Documents must be embedded as
> `search_document: <text>` and queries as `search_query: <text>`. Omitting them degrades
> retrieval **silently** — no error, just worse results. The prefix is applied inside
> `internal/ollama/embed.go` and never by callers, is recorded in the index manifest, and is
> asserted by an eval case.

---

## Memory & Disk Footprint

Target hardware: **CPU-only, 16 GB RAM.**

| Component | Disk | RAM (active) | RAM (idle) |
|-----------|------|--------------|------------|
| Go binary (vektix) | ~15 MB | ~40 MB | ~40 MB |
| Ollama binary | ~50 MB | — | — |
| `nomic-embed-text` | 274 MB | ~350 MB | 0 (unloaded) |
| `qwen2.5:0.5b` (intent) | 380 MB | ~400 MB | 0 (unloaded) |
| `qwen2.5:3b` (explain, on demand) | ~2 GB | ~2.2 GB | 0 (unloaded) |
| Index @ 50k chunks (768-dim + content + BM25) | ~350 MB | ~400 MB | ~400 MB |
| **Typical use (no explain)** | **~1.1 GB** | **~800 MB** | **~440 MB** |
| **Peak (explain active)** | ~3.1 GB | ~3.0 GB | — |

Index scales roughly linearly: **50k chunks ≈ 400 MB resident.** A chunk's vector is
768 × 4 B = 3 KB; content and BM25 postings roughly double that.

> **On idle memory:** models unload on Ollama's `keep_alive` timer, but the *index* stays resident
> for as long as Vektix runs — that is the ~400 MB floor. Vektix is a foreground tool, not a daemon;
> when you exit, it all goes away.

### The keep_alive tradeoff (stated honestly)

v2 claimed both a ~60 MB idle footprint *and* `keep_alive: "30m"`. Those are mutually exclusive.
The real choice:

| `keep_alive` | Idle RAM | First query after idle |
|---|---|---|
| `"30m"` | +400 MB (0.5b stays resident) | ~300 ms |
| `"5m"` **(default)** | baseline | ~1.5 s (model reload) |
| `"0"` | baseline | ~1.5 s every time |

Default is `5m`, configurable. Most queries never reach the LLM at all, which is what makes the
shorter timer affordable.

---

## Latency (CPU)

| Operation | Time | Model needed |
|-----------|------|--------------|
| **Path match** (`open main.go`, `find *.pdf`) | ~1-5 ms | none |
| **BM25 lookup** (exact token, identifier, error code) | ~10-20 ms | none |
| **Hybrid locate** (fuzzy phrasing, scoped) | ~40-80 ms | embedder |
| **Hybrid locate** (fuzzy phrasing, global @ 50k) | ~80-150 ms | embedder |
| **NL intent parse** (router Tier 2) | ~300-500 ms | 0.5b |
| **Excerpt render** (after resolve) | ~2-5 ms | none |
| **`--explain`** (first call, cold) | ~4-8 s | 3b, on-demand load |
| **Index 1 file** | ~15-30 ms | embedder |
| **Index 5,000 chunks** | ~40-60 s | embedder |
| **Cold start** (models unloaded) | ~1.5-2 s | — |
| **Warm start** | ~120 ms + index load | — |

**The headline number:** a query answered by the path or BM25 arm — which is most "where is X"
queries — completes in **under 20 ms with no model loaded**. That is the design working.

---

## Project Structure

```
vektix/
├── cmd/vektix/main.go               # Entry point, CLI flags, subcommands
├── internal/
│   ├── tui/
│   │   ├── app.go                  # Root Bubble Tea model
│   │   ├── chat.go                 # Query/result view
│   │   ├── picker.go               # Ambiguous-match chooser
│   │   ├── status.go               # Status bar (SHOWS ACTIVE SCOPE)
│   │   ├── index.go                # Indexing progress view
│   │   └── styles.go               # Lip Gloss theme
│   ├── router/
│   │   ├── fastpath.go             # Guarded regex matcher
│   │   ├── llm.go                  # Schema-constrained classification
│   │   └── schema.go               # JSON Schema for the action set
│   ├── resolve/
│   │   ├── scope.go                # Scope resolution + per-arm filtering
│   │   ├── paths.go                # Filename/path trie, fuzzy scoring
│   │   ├── bm25.go                 # Lexical index
│   │   ├── vector.go               # chromem-go query + oversampling
│   │   └── fuse.go                 # Reciprocal rank fusion
│   ├── excerpt/
│   │   ├── expand.go               # Chunk → natural boundary
│   │   └── render.go               # Line numbers, gutter, highlight
│   ├── index/
│   │   ├── walk.go                 # Symlink-safe walker, binary sniff
│   │   ├── ignore.go               # .vektixignore + config excludes
│   │   ├── sync.go                 # Reconcile: add/update/purge orphans
│   │   └── manifest.go             # Model identity + dir chunk-count tree
│   ├── chunker/
│   │   ├── text.go                 # Prose: sentence-aware + overlap
│   │   ├── code.go                 # Symbol-aware (go/ast + heuristics)
│   │   └── structured.go           # JSON/YAML/TOML: top-level keys
│   ├── store/
│   │   ├── store.go                # chromem-go wrapper
│   │   └── document.go             # Chunk model, Locator, metadata codec
│   ├── ollama/
│   │   ├── client.go               # HTTP client, per-type timeouts
│   │   ├── embed.go                # Embedding + REQUIRED task prefixes
│   │   ├── chat.go                 # Chat/stream calls
│   │   ├── budget.go               # Context budgeting (num_ctx guard)
│   │   └── cache.go                # LRU query-embedding cache
│   ├── fileops/
│   │   ├── ops.go                  # read, open (read-only)
│   │   └── safety.go               # Path confinement, secrets denylist
│   ├── clipboard/copy.go           # wl-copy → xclip → xsel → OSC 52
│   ├── session/refs.go             # Last results, ordinal references
│   ├── parser/
│   │   ├── text.go                 # Plain text / markdown
│   │   └── pdf.go                  # PDF extraction, panic-sandboxed
│   ├── eval/
│   │   ├── runner.go               # Eval harness
│   │   └── metrics.go              # Accuracy, recall, latency p50/p95
│   └── config/config.go            # Config loading + defaults
├── testdata/
│   ├── intent_eval.jsonl           # Intent cases + hijack suite
│   ├── locate_eval.jsonl           # Locate accuracy, scoped and global
│   └── corpus/                     # Small fixed corpus for eval
├── go.mod  go.sum  Makefile  plan.md  README.md
```

---

## The Action Set

Deliberately narrow — six actions covering fetch, read, open, copy. A tight schema is a large part
of why a 0.5B model can classify reliably under grammar-constrained decoding.

| Action | Verb | NL examples | Model needed |
|--------|------|-------------|--------------|
| **locate** | fetch | "where is my resume", "the docker notes file" | none for path/exact; embedder for fuzzy |
| **read** | read | "show config.yaml", "first 30 lines of server.go" | none |
| **excerpt** | read | "what's my postgres connection string" | embedder |
| **open** | open | "open main.go", "open that kubernetes guide" | none |
| **copy** | copy | "copy that", "copy the path" | none |
| **list** | fetch | "what's in ~/projects" | none |

`locate` returns ranked paths. `excerpt` returns ranked **passages**. `read` prints a whole file or
line range. All are read-only; there is no write path in the binary at all.

### Excerpt output

```
> whats my postgres connection string

~/notes/infra.md:41-47                                    (bm25+vec, rank 1)
  41 │ ## Local Postgres
  42 │
  43 │ DATABASE_URL=postgres://dev@localhost:5432/appdb
  44 │ pool max 20, idle 5m
  45 │ migrations: ./db/migrate

  [o]pen  [c]opy  [e]xplain  [n]ext match
```

The line numbers are real. The text is byte-for-byte what is in the file.

---

## Scoped Fetching

**Launching Vektix inside a directory confines every search to that subtree.** Open it in
`~/projects/go/vektix` and it will never surface a match from `~/notes` or another project.

Scope is a **view over the existing index**, not a separate index. Nothing is re-indexed.

### Scope resolution at startup

```
CWD is under an indexed root    → scope = CWD subtree        (the normal case)
CWD is an indexed root itself   → scope = that root
CWD is outside every root       → prompt: index it now, or continue global
--scope <path>                  → explicit override
--global / -g                   → force the full index
```

Config: `scope_mode = "auto" | "global" | "cwd"`, default `auto` (the ladder above).

### How each arm is filtered

| Arm | Filtering | Cost when scoped |
|---|---|---|
| Path index | Ours, in-memory — exact prefix match on the trie | Free; *faster* than global |
| BM25 | Ours, in-memory — posting walk restricted to in-scope IDs | Proportional to scope size |
| Vector (chromem-go) | **Cannot be pushed down** — see below | Unchanged (brute force) |

chromem-go's `QueryEmbedding(ctx, emb, nResults, where, whereDocument)` supports **exact metadata
matches only**, and `where` is an AND of equalities. There is no prefix operator and no OR over
values, so a subtree filter is not expressible as a `where` clause.

**Approach — adaptive oversampling with an in-memory prefix filter:**

1. The manifest keeps a directory → chunk-count prefix tree, making `scopeFraction` an O(1) lookup.
2. Query with `nResults = clamp(k / max(scopeFraction, 0.01), k, collectionSize)`.
3. Prefix-filter the returned documents by path in Go.
4. If fewer than `k` survive, retry once at full `collectionSize` — an exhaustive scan, still only
   ~40 ms at 50k chunks, since chromem-go is brute-force either way.

Because the path and BM25 arms are filtered **exactly** and feed the same RRF fusion, a lossy
vector arm degrades the final ranking far less than it would in a vector-only design. This is a
concrete payoff from choosing hybrid retrieval.

**Escape hatch if it measures slow:** partition into one chromem-go collection per indexed root,
so a scoped query only ever scans its own root. Deferred to v2 — it complicates `sync` and the
manifest, and should not be paid for before it is measured.

### An honest note on "reducing load"

Accuracy is the real and immediate win: removing cross-project matches from the candidate set is
exactly what makes rank-1 correct more often. **Load reduction is more modest than it looks.** The
path and BM25 arms genuinely get cheaper; the vector arm scans the whole collection whether scoped
or not, until per-root partitioning lands. The latency table above reflects this.

### Ephemeral scope indexing

If CWD is unindexed, offer to index just that subtree on the spot. With `nomic-embed-text` a
typical project is seconds — which is the point: you should not have to prepare a repo before
asking about it. Stored in the same collection under `transient = true`, so `vektix sync` can expire
it on an LRU basis.

### UX requirements — the main risk of this feature

A silently-scoped tool that "can't find" a file you know exists is deeply confusing. Therefore:

- The status bar **always** shows the active scope and its chunk count:
  `scope: ~/projects/go/vektix (412 chunks)` vs `scope: global (48,203)`.
- Zero results while scoped **always** names the scope and offers the global retry inline.
- In-TUI `:scope <path>` and `:scope global`, plus a keybind to re-run the last query globally
  without retyping it.
- Changing scope **invalidates session ordinal refs** — "open the first one" must never point into
  a stale, differently-scoped result set.

---

## Intent Router — Two Tiers

### Tier 1: Guarded regex fast-path (<1 ms)

```go
var fastPatterns = []Pattern{
    {Re: `^open\s+(.+)$`,           Action: "open",    Guard: pathShaped},
    {Re: `^(?:read|show|cat)\s+(.+)$`, Action: "read", Guard: pathShaped},
    {Re: `^(?:ls|list)\s+(.+)$`,    Action: "list",    Guard: pathShaped},
    {Re: `^head\s+(-?\d+)\s+(.+)$`, Action: "read",    Guard: pathShaped},
    {Re: `^tail\s+(-?\d+)\s+(.+)$`, Action: "read",    Guard: pathShaped},
    {Re: `^find\s+(.+)$`,           Action: "locate",  Guard: globShaped},
    {Re: `^copy\s+(.+)$`,           Action: "copy",    Guard: pathShapedOrRef},
}
```

> ⚠️ **The guard is not optional — it is the whole point.**
> An unguarded `^find\s+(.+)$` turns *"find out what I wrote about docker"* into a glob of
> `out what I wrote about docker`. An unguarded `^show\s+(.+)$` turns *"show me what's in the
> docker file"* into a path of `me what's in the docker file`. Instant, confident, and wrong — the
> worst possible failure for a locator.

A pattern fires **only if its captured argument passes the guard**:

```go
// pathShaped: contains '/', has a known extension, resolves to an
// existing file, or is a single token with no spaces.
// globShaped: pathShaped, or contains '*', '?', or '['.
```

Anything else falls through to Tier 2. Every guard has a regression fixture in
`testdata/intent_eval.jsonl` asserting the LLM path was taken.

### Tier 2: Schema-constrained LLM classification (~300-500 ms)

Ollama's `format` parameter accepts a **full JSON Schema**, which grammar-constrains decoding. The
output is guaranteed to be valid JSON matching the schema — a 0.5B model cannot emit a malformed
action, only a semantically wrong one.

```go
resp := client.Chat(ChatRequest{
    Model:    cfg.IntentModel,
    Messages: msgs,
    Format:   actionSchema,          // full JSON Schema, not the string "json"
    Options: map[string]any{
        "num_ctx":     2048,         // EXPLICIT — never leave this to the default
        "num_predict": 64,           // hard cap; the output is ~25 tokens
        "temperature": 0,            // deterministic
        "seed":        1,            // reproducible evals
    },
})
```

```json
{
  "type": "object",
  "properties": {
    "action": {"enum": ["locate","read","excerpt","open","copy","list"]},
    "query":  {"type": "string"},
    "path":   {"type": "string"},
    "lines":  {"type": "string", "pattern": "^\\d+(-\\d+)?$"}
  },
  "required": ["action"]
}
```

System prompt: *"You are Vektix's intent classifier. You perform read-only operations only. Map the
user's phrasing to one action. Do not answer the question itself."*

---

## Retrieval

### Hybrid: BM25 + vector, fused with RRF

Vector search alone misses exact tokens — identifiers, error codes, CLI flags, filenames.
`ERR_CONN_REFUSED` has no useful semantic neighbourhood. BM25 alone misses paraphrase. Running both
and fusing by rank captures each one's strengths; published comparisons put fused recall@10 near
91% against roughly 78% for vector-only.

```
score(doc) = Σ_arms  1 / (60 + rank_arm(doc))
```

Rank-based fusion is used deliberately over score-based: BM25 scores and cosine similarities live
on incomparable scales, and RRF needs no normalisation or tuning.

### Paths are first-class search targets

*"open my resume pdf"* is a **path** query. v2 embedded only file *content*, so nothing would have
matched. The path index tokenises basename, stem, parent directories, and extension into a trie
with fuzzy scoring, and joins the same RRF fusion. This alone answers a large share of "where is X"
queries in ~1 ms with no model loaded.

### On `similarity_threshold`

v2 set `0.3`. With L2-normalised embeddings, unrelated text routinely scores 0.4–0.6, so that
threshold filters nothing at all. Under RRF, **rank cutoffs replace score thresholds** — keep the
top `k` after fusion and apply a minimum-arms rule (a result appearing in only one arm at low rank
is dropped). Any residual score floor is calibrated from `locate_eval.jsonl`, not guessed.

### Honest empty results

If nothing clears the cutoff, say so, name the active scope, and offer the nearest weak matches
plus the global retry. Never let a model fill the gap.

---

## Excerpts

A retrieved chunk is a ~256-token window that frequently cuts mid-thought. `excerpt/expand.go`
grows it outward to a natural boundary before rendering:

| Source | Expanded to |
|---|---|
| Prose (`.md`, `.txt`) | Enclosing paragraph(s), up to a line budget |
| Code | The **enclosing function or type declaration**, signature included |
| Structured (`.json`, `.yaml`, `.toml`) | The enclosing top-level key |
| PDF | Enclosing paragraph within the page |

`render.go` prints line numbers, a gutter, and highlights the matched span. This is the product
surface — it is what the user actually reads — so it gets its own package and its own tests.

### The Locator type

v2's `{start_line, end_line}` is meaningless for PDFs. Chunks carry a discriminated locator:

```go
type Locator struct {
    Kind   LocatorKind // LineRange | Page | Symbol
    Start  int         // line or page
    End    int
    Symbol string      // for code: "func (e *Engine) Index"
}
```

Because chromem-go metadata is `map[string]string`, `store/document.go` owns explicit
encode/decode helpers. No numeric metadata is stringified ad hoc at call sites.

---

## Chunking

Strategy is selected by extension.

### Prose — `.md`, `.txt`, `.pdf`

- **Max**: ~256 tokens · **Overlap**: ~50 · **Min**: 20 (skip fragments)
- Sentence-aware: split on `. ` or `\n\n`, never mid-word
- Markdown: prefer heading boundaries when one falls in range

### Code — `.go`, `.py`, `.js`, `.ts`, `.rs`, `.sh`, `.c`, `.java`

Split on **symbol boundaries**, so an excerpt is a whole function rather than a window that cuts
mid-body.

- **Go** → stdlib `go/parser` + `go/ast`. Exact, zero dependencies, no CGO.
- **Everything else** → heuristics on column-0 declarations
  (`func`, `def`, `class`, `function`, `fn`, `type`, `impl`, `pub fn`).
- Oversized functions fall back to windowed splitting, retaining the signature in every chunk so
  each one remains self-describing.

> A pure-Go tree-sitter runtime (`odvcencio/gotreesitter`, 205 embedded grammars, no CGO) now
> exists and is the principled answer for broad language coverage. It is young and
> single-maintainer, so it is a **v2 upgrade path**, not a v1 dependency. The CGO tree-sitter
> bindings are disqualified outright — they would break the single-binary promise.

### Structured — `.json`, `.yaml`, `.toml`

Split on top-level keys, retaining the key path as the chunk prefix.

### On token counting

There is no Qwen or nomic tokenizer in Go, so **all token counts are estimates**:
`estimatedTokens = len([]rune(s)) / 4`, accurate to roughly ±20% on English prose and worse on
code. Budgets carry a 25% safety margin and the estimator lives in one place
(`ollama/budget.go`) so it can be swapped. The plan does not pretend 256 is exact.

---

## Indexing & Freshness

### Pipelined indexing

Stages overlap via goroutines and channels: **walk → parse → chunk → embed (batched) → store**.
Embedding is the bottleneck, so it runs in batches of 64–100 texts per `/api/embed` call, which
eliminates per-chunk HTTP overhead. Backpressure comes from bounded channels; the walker blocks
rather than buffering an entire tree in memory.

### The manifest

A manifest sits beside the vector DB and is the source of truth for reconciliation and validity:

```json
{
  "embedding_model": "nomic-embed-text",
  "dim": 768,
  "prefix_scheme": "nomic-v1.5",
  "chunker_version": 3,
  "roots": ["~/Documents", "~/notes", "~/projects"],
  "files": { "<path>": {"mtime": 0, "size": 0, "hash": "", "chunks": ["id"]} },
  "dir_counts": { "<dir>": 412 }
}
```

`dir_counts` is the prefix tree that makes `scopeFraction` an O(1) lookup for scoped queries.

> ⚠️ **Index invalidation.** Changing `embedding_model` mixes incompatible vectors into one
> collection — a dimension mismatch at best, silently garbage rankings at worst. On any mismatch
> of `{embedding_model, dim, prefix_scheme, chunker_version}`, Vektix **refuses to query** and prints
> the exact `vektix reindex` command. It never degrades quietly.

### `vektix sync` — and why orphan purging is mandatory

Delete or move a file and its chunks live on forever, so Vektix cites paths that no longer exist.
For a tool whose entire value is knowing where things are, that is credibility-ending.

`sync` re-walks the roots and reconciles against the manifest, using
`Collection.Delete(ctx, where, whereDocument, ids...)` to remove orphans:

```
$ vektix sync
  scanned    1,204 files
  unchanged  1,180  (mtime + size match — skipped)
  updated        9  (re-chunked, re-embedded)
  added         12
  removed        3  (orphan chunks purged)
  1.4s
```

Change detection is `mtime + size`, with a content hash as a tiebreaker when mtime is unreliable
(copied trees, restored backups). A cheap reconcile runs in the background on TUI startup.

---

## Exclusion Rules

*(Carried forward from v2 substantially unchanged — this section was already well specified.)*

Three layers, evaluated in order.

### 1. Config-level exclusions (`config.toml`)

Global rules applying to all indexed directories — `[index.exclude]`, shown in the config below.

### 2. `.vektixignore` (per-directory, `.gitignore` syntax)

Drop a `.vektixignore` in any folder to exclude files or subdirectories within it:

```gitignore
# ~/Documents/.vektixignore
drafts/
temp/
secrets.txt
*.bak
WIP-*
archive/*
!archive/important-notes.md
```

Checked at every directory level during the walk; a `.vektixignore` in a subdirectory applies only
to that subtree.

### 3. Hardcoded defaults (never indexed, cannot be overridden)

```
Binary/media:    *.jpg *.png *.gif *.mp4 *.mp3 *.zip *.tar *.gz *.7z
Compiled:        *.o *.so *.exe *.bin *.dylib *.class *.pyc *.wasm
Version control: .git/ .hg/ .svn/
OS junk:         .DS_Store Thumbs.db desktop.ini
Secrets:         .ssh/ .gnupg/ .aws/credentials *.pem *.key .env*
```

> The secrets group is new in v3. See **Safety** — it is also enforced at read time, not just at
> index time.

### Evaluation order

```
File: ~/Documents/projects/myapp/node_modules/lodash/README.md
  1. Hardcoded?        → no
  2. Config exclude?   → YES ("node_modules") → SKIP THE ENTIRE DIRECTORY (don't walk in)

File: ~/Documents/notes/drafts/half-finished.md
  1. Hardcoded?        → no
  2. Config exclude?   → no
  3. .vektixignore?     → YES (~/Documents/notes/.vektixignore has "drafts/") → SKIP

File: ~/Documents/notes/meeting-notes.md
  1-3. no match
  4. Extension allowed?      → yes (.md)
  5. Under max_file_size_mb? → yes
  6. Binary sniff (first 8KB NUL / invalid UTF-8)? → clean
     → INDEX ✅
```

### CLI

```bash
vektix index ~/Documents --exclude "~/Documents/archive"
vektix index ~/notes --exclude "*.pdf"
vektix index ~/Documents --dry-run       # list what WOULD be indexed
vektix config show-excludes              # all active rules + .vektixignore files found
```

---

## Configuration

Location: `~/.config/vektix/config.toml`

```toml
[general]
data_dir   = "~/.local/share/vektix"
editor     = ""                          # auto-detect: $EDITOR, then xdg-open
scope_mode = "auto"                      # "auto" | "global" | "cwd"

[ollama]
host           = "http://localhost:11434"
embedding_model = "nomic-embed-text"     # 768-dim; changing this requires a reindex
intent_model    = "qwen2.5:0.5b"
explain_model   = "qwen2.5:3b-instruct"  # pulled on first --explain, not at setup
keep_alive      = "5m"

# Separate timeouts — one global value cannot cover all three
[ollama.timeouts]
embed_batch_seconds = 180                # a 100-chunk CPU batch can be slow
intent_seconds      = 15
stream_idle_seconds = 30                 # IDLE, not total — streams have no wall-clock cap

# Explicit context windows — never left to the Ollama default
[ollama.context]
intent_num_ctx  = 2048
explain_num_ctx = 8192

[index]
index_dirs       = ["~/Documents", "~/notes", "~/projects"]   # renamed from watch_dirs
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
max_results  = 8                         # candidates after RRF fusion
rrf_k        = 60                        # RRF constant
min_arms     = 1                         # drop single-arm results below rank 3
oversample_floor = 0.01                  # scoped vector oversampling clamp

[chunking]
max_tokens     = 256                     # ESTIMATED (runes/4, ±20%)
overlap_tokens = 50
min_tokens     = 20

[safety]
confine_to_roots = true                  # deny reads outside index_dirs + CWD
allow_secrets    = false                 # .ssh/, *.pem, .env* etc. require --unsafe
```

> `watch_dirs` was renamed to `index_dirs`. Nothing watches anything — the old name promised a
> daemon that does not exist. A real fsnotify watcher is a v2 consideration.

---

## TUI

```
┌──────────────────────────────────────────────────────────────┐
│ 🔷 VIXOR    scope: ~/projects/go/vektix (412)   [g] global    │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  > where do we handle the retry backoff                      │
│                                                              │
│  internal/ollama/client.go:88-104          func retryWithBackoff │
│    88 │ func retryWithBackoff(ctx context.Context,           │
│    89 │     fn func() error, max int) error {                │
│    90 │     d := 100 * time.Millisecond                      │
│    91 │     for i := 0; i < max; i++ {                       │
│    92 │         if err := fn(); err == nil { return nil }     │
│                                                              │
│    [o]pen  [c]opy  [e]xplain  [n]ext  2 more matches         │
│                                                              │
│  > open it                                                   │
│  ✓ opened internal/ollama/client.go:88 in nvim               │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│ > _                                                          │
└──────────────────────────────────────────────────────────────┘
```

- **Scope is always visible** in the status bar, with a one-key global toggle.
- **Session refs** (`internal/session/`) keep the last N result sets so *"open the first one"*,
  *"open it"*, *"#2"*, and *"that pdf"* resolve. Required by the TUI-first design, and cleared
  whenever scope changes.
- `[c]opy` yields the excerpt; `copy path` yields the path.
- `[e]xplain` is the only path that loads the 3B model, and it says so before it does
  (*"pulling qwen2.5:3b (2 GB)…"* on first use).

---

## Safety & Correctness

Read-only is a property that has to be built, not assumed.

### No write path exists

The binary contains no `os.Create`, `os.WriteFile`, `os.Remove`, or `os.Rename` outside
`internal/store` and `internal/index` (which write only into `data_dir`). Enforced by a CI grep
over `internal/fileops`, `internal/excerpt`, and `internal/clipboard`.

### Path confinement

The LLM emits paths, and an indexed document can contain text steering it. The blast radius is
local and read-only, but `read ~/.ssh/id_rsa` rendering a private key into scrollback is still
unacceptable. Every path is resolved through `filepath.EvalSymlinks` + `filepath.Abs` and confined
to `index_dirs` plus the invocation CWD, with the secrets denylist enforced at read time.
`--unsafe` is required to cross it, and there is no way for the model to set that flag.

### Launching the editor

`$EDITOR` is commonly `"code -w"` or `"nvim -p"`, so it is tokenised with `shlex`-style splitting
and passed to `exec.Command` as separate arguments — **never through a shell**. A `--` separator
precedes the path so filenames beginning with `-` are not parsed as flags. Falls back to
`xdg-open`, and degrades to printing the path when neither is available (headless / SSH).

### Walker robustness

Directory symlinks are not followed by default (`follow_symlinks = false`), and visited
`(dev, inode)` pairs are tracked, so symlink loops cannot cause infinite recursion. Files whose
first 8 KB contain NUL bytes or invalid UTF-8 are skipped regardless of extension.

### PDF parsing is sandboxed

`ledongthuc/pdf` **panics** on malformed files (`malformed PDF: reading at offset 0: stream not
present`). Unhandled, one bad PDF aborts an entire index run. Each PDF is therefore parsed in an
isolated goroutine with `recover()` and a per-file timeout; failures go to a quarantine list
surfaced by `vektix status`; the run always continues.

### Context truncation is guarded, not trusted

Ollama truncates an over-long prompt **from the head** — the system prompt and output schema are
the first things dropped, leaving the model with raw document text and no instructions, producing
confident nonsense with no error anywhere. So: `num_ctx` is set explicitly on every request,
`ollama/budget.go` estimates prompt size and drops the lowest-ranked chunks to fit before sending,
and any drop is logged. Nothing is left to the runtime default.

### KV cache reuse

Ollama reuses the KV cache for a shared prompt prefix, so a stable system prompt is effectively
free after the first call. Interleaving *different* system prompts on the *same* model evicts that
prefix each time — which is a further reason intent and explain use **separate models**: each keeps
its own warm prefix and they never contend.

---

## Phased Roadmap

Sequenced so a genuinely useful tool exists at the end of **Phase 3**, before the router or the TUI.

### Phase 1 — Foundation & onboarding
- [ ] Go module, project structure, Makefile with `-ldflags` version stamping
- [ ] Config loading + defaults
- [ ] Ollama client: per-type timeouts, explicit `num_ctx`, context budgeting
- [ ] **`vektix setup`** — detect Ollama, install guidance, pull embed + intent models, propose roots
- [ ] **`vektix doctor`** — Ollama reachable? models present? manifest consistent?

### Phase 2 — Index
- [ ] Symlink-safe walker with binary sniffing
- [ ] `.vektixignore`, config excludes, hardcoded excludes
- [ ] Text / PDF (panic-sandboxed) parsers
- [ ] Chunkers: prose, code (`go/ast` + heuristics), structured
- [ ] Batched embedding with the **required `search_document:` prefix**
- [ ] chromem-go store + manifest, including the `dir_counts` prefix tree
- [ ] Pipelined goroutine stages with backpressure
- [ ] `vektix index <path>`, `--dry-run`, `--exclude`

### Phase 3 — Resolve, scope & excerpt *(core value lands here)*
- [ ] Path index (trie, fuzzy scoring)
- [ ] BM25 lexical index
- [ ] Vector search + LRU query-embedding cache
- [ ] RRF fusion
- [ ] **Scope resolution + per-arm filtering + adaptive oversampling**
- [ ] Excerpt expansion (paragraph / symbol / key) and rendering
- [ ] Path confinement + secrets denylist
- [ ] CLI one-shots: `locate`, `read`, `excerpt`, `open`, `copy`, `list`
- [ ] `--scope`, `--global`, `--json`

### Phase 4 — Router
- [ ] Guarded regex fast-path + guard predicates
- [ ] Hijack regression fixtures
- [ ] Schema-constrained classification via Ollama `format`
- [ ] Clipboard: `wl-copy` → `xclip` → `xsel` → OSC 52

### Phase 5 — TUI
- [ ] Bubble Tea app, query loop, result rendering
- [ ] Ambiguous-match picker
- [ ] **Scope indicator, `:scope` commands, global toggle**
- [ ] Session ordinal refs, invalidated on scope change
- [ ] `[o]/[c]/[e]/[n]` keybinds; streaming for `--explain`
- [ ] Indexing progress; graceful shutdown

### Phase 6 — Sync & polish
- [ ] `vektix sync` with orphan purge
- [ ] Background reconcile on startup
- [ ] Ephemeral scope indexing + transient LRU expiry
- [ ] `vektix status` (chunks, roots, scope, last sync, quarantine, DB load time)
- [ ] Eval harness + datasets
- [ ] Error handling, README, install instructions

### v2
- [ ] Per-root collection partitioning (if scoped vector search measures slow)
- [ ] `gotreesitter` for broad symbol-aware chunking
- [ ] Optional fsnotify watcher
- [ ] LoRA fine-tune of the intent model on collected usage
- [ ] Write operations behind confirmation gates

---

## Evaluation

### Intent classification

```jsonl
{"input": "open main.go", "expected": {"action": "open", "path": "main.go"}, "tier": 1}
{"input": "find out what I wrote about docker", "expected": {"action": "excerpt", "query": "docker"}, "tier": 2}
{"input": "show me what's in the docker file", "expected": {"action": "read", "query": "docker"}, "tier": 2}
```

~150 cases across all six actions, ambiguous phrasing, typos, and edge cases — **plus a hijack
suite** covering every fast-path verb used in a conversational sentence, asserting `tier: 2` was
taken. Targets: action accuracy >92%, parameter extraction >85%.

### Locate accuracy

```jsonl
{"query": "my resume", "expect_path": "~/Documents/resume_2024.pdf", "scope": "global"}
{"query": "retry backoff", "expect_path": "internal/ollama/client.go", "scope": "~/projects/go/vektix"}
```

Metrics: **recall@1** and **recall@3**. Targets: >75% @1, >90% @3.

### Excerpt correctness

Replaces v2's "answer faithfulness" — nothing generates answers any more. Does the returned line
range **actually contain** the expected string? A binary, unambiguous check.

### Retrieval ablation

BM25-only vs. vector-only vs. RRF on the same set. This proves hybrid is earning its complexity,
and it is also the tripwire for a silently-dropped `search_query:` prefix — a prefix regression
shows up as the vector arm collapsing while BM25 holds steady.

### Scoped vs. global comparison

Run the same locate cases from inside a project directory and globally. This is the measurement
that substantiates the accuracy claim for scoped fetching, and the regression test for
oversampling: **if scoped recall drops below global recall, the oversample factor is too low or
the exhaustive fallback is not firing.**

```bash
vektix eval --dataset testdata/locate_eval.jsonl
# locate recall@1   78.0%   locate recall@3   91.5%
# scoped recall@1   86.5%   scoped recall@3   96.0%   (+8.5 / +4.5)
# ablation: bm25 62.0  vector 74.5  rrf 91.5
# latency p50 41ms   p95 118ms
```

`temperature: 0` and a fixed `seed` everywhere, so eval runs are reproducible.

### When to run

After any model, prompt, chunking, or fusion change; before any release.

---

## Inference Optimizations

| Optimization | What it does | Effect |
|---|---|---|
| **Model-free hot path** | Path + BM25 arms answer most locate queries | <20 ms, no model loaded |
| **Batch embedding** | 64-100 texts per `/api/embed` call | ~50-60% faster indexing |
| **Pipelined indexing** | Overlap walk/parse/chunk/embed/store | ~2-3x faster indexing |
| **Query embedding LRU** | Skip re-embedding repeated queries (500 entries ≈ 1.5 MB) | ~15 ms per repeat |
| **Token caps** | `num_predict: 64` intent, `300` explain | Prevents runaway generation |
| **Separate models** | Intent and explain never evict each other's KV prefix | Stable ~300 ms intent |
| **Scoped filtering** | Path/BM25 arms restricted to subtree | Better accuracy; some load saved |

> The single largest performance decision in this plan is **not** an optimization — it is the
> architecture. Because the router and resolver are separate, and because path and BM25 arms need
> no model, the common case never touches Ollama at all.

---

## Verification

### Pre-implementation checks

Cheap empirical checks to run **before Phase 2 commits to these numbers**:

1. **Embedding prefix** — after `ollama pull nomic-embed-text`, embed the same sentence with and
   without `search_query:` and confirm the vectors differ. Guards the silent-degradation risk.
2. **Context truncation** — send an oversized prompt with a distinctive instruction in the system
   message and confirm it is dropped, establishing the real `num_ctx` default on this machine.
3. **chromem-go API** — a ~20-line program calling `NewPersistentDB`, `AddDocuments`,
   `QueryEmbedding`, and `Delete` against the version that actually resolves. **Also confirm
   `where` is exact-match only** — if a prefix or OR operator has since been added, scoped vector
   filtering can be pushed down and the oversampling design should be replaced with it.
4. **PDF panic** — run `ledongthuc/pdf` against a truncated PDF and confirm `recover()` catches it.
5. **Persistence shape** — index ~5k chunks, count files under `data_dir`, and measure cold load
   time. chromem-go writes one gob file per document; if 50k chunks proves painful, switch to
   gzip + `ExportToFile` snapshots.

### Automated tests

```bash
go test ./internal/... -race -count=1        # unit; Ollama mocked over httptest
go test ./internal/... -tags=integration     # requires Ollama running
go build -o /dev/null ./cmd/vektix/
golangci-lint run ./...
vektix eval --dataset testdata/intent_eval.jsonl
vektix eval --dataset testdata/locate_eval.jsonl
```

### Manual verification

1. **No Ollama** → helpful install guidance, not a stack trace
2. **`vektix index ~/test-dir`** with .txt/.md/.pdf/.go → chunk counts match `--dry-run`
3. **Malformed PDF in the tree** → quarantined, run completes
4. **Locate by path** — "my resume" → correct file, no model loaded (verify via `ollama ps`)
5. **Locate by content** — "retry backoff" → correct function, excerpt is the whole function
6. **Exact token** — an error code or identifier → found via BM25 where vector alone would miss
7. **Scoped** — same query from `~/projects/foo` and `~/notes` → disjoint, correct results
8. **Scope visibility** — status bar shows scope; zero-result message names it and offers global
9. **Session refs** — "open the first one" works; changing scope invalidates it
10. **Copy** — excerpt reaches the clipboard under Wayland, X11, and over SSH (OSC 52)
11. **Read-only** — no command modifies any file; `--unsafe` required to read `~/.ssh/`
12. **Stale index** — delete an indexed file, run `sync`, confirm it stops being cited
13. **Model change** — edit `embedding_model`, confirm Vektix refuses and names `vektix reindex`
14. **Latency** — model-free locate <20 ms; NL intent <1 s
15. **Memory** — RSS ~800 MB active at 50k chunks; ~440 MB with models unloaded

---

## Dependencies

### Go modules
```
github.com/charmbracelet/bubbletea      # TUI framework
github.com/charmbracelet/bubbles        # textinput, viewport, progress
github.com/charmbracelet/lipgloss       # TUI styling
github.com/charmbracelet/glamour        # Markdown rendering
github.com/philippgille/chromem-go      # Embedded vector store (MPL-2.0, pure Go)
github.com/ledongthuc/pdf               # PDF text extraction (pure Go, panic-sandboxed)
github.com/BurntSushi/toml              # Config
github.com/sahilm/fuzzy                 # Path fuzzy scoring
```
Standard library only for Go symbol parsing (`go/parser`, `go/ast`). **No CGO anywhere.**

### External
```
ollama                          # user installs; vektix setup guides this
  ├── nomic-embed-text          # embeddings          274 MB   (setup)
  ├── qwen2.5:0.5b              # intent              380 MB   (setup)
  └── qwen2.5:3b-instruct       # --explain only      ~2 GB    (on demand)
```

---

## Appendix — What changed from v2, and why

| # | v2 | v3 | Reason |
|---|---|---|---|
| 1 | RAG prose answers | **Verbatim excerpts**; `--explain` opt-in | A locator must not paraphrase your files |
| 2 | bge-m3, "384 dims", "670 MB" | **nomic-embed-text**, 768-dim, 274 MB | v2's figures were wrong (bge-m3 is 1024-dim / 1.2 GB) and it is slow on CPU |
| 3 | Vector search only | **Hybrid BM25 + vector, RRF** | Vector alone misses identifiers, error codes, filenames |
| 4 | Content only | **Paths are searchable** | "open my resume pdf" is a path query and would never have matched |
| 5 | *(absent)* | **Scoped fetching** | Launching in a subdirectory should confine the search |
| 6 | *(absent)* | **`copy` action** | Explicitly required; wl-copy/xclip/xsel/OSC 52 |
| 7 | Unguarded regex fast-path | **Guarded** | "find out what I wrote about docker" became a glob |
| 8 | `Format: "json"` | **Full JSON Schema** | Grammar-constrains a 0.5B model into always-valid actions |
| 9 | `num_ctx` never set | **Explicit + budgeted** | Ollama truncates from the head, silently dropping the system prompt |
| 10 | No delete path | **`vektix sync` + orphan purge** | Deleted files were cited forever; chromem-go does support `Delete` |
| 11 | No index identity | **Manifest + refuse on mismatch** | Changing the embedder silently corrupted rankings |
| 12 | One 30 s timeout | **Three timeouts, stream is idle-based** | A 100-chunk CPU batch exceeds 30 s |
| 13 | `similarity_threshold = 0.3` | **Rank cutoffs, calibrated** | 0.3 filters nothing with normalised embeddings |
| 14 | `{start_line, end_line}` | **`Locator` (line / page / symbol)** | Line numbers are meaningless for PDFs |
| 15 | 60 MB idle *and* `keep_alive: 30m` | **One stated tradeoff** | Mutually exclusive claims |
| 16 | `watch_dirs` | **`index_dirs`** | Nothing watched anything |
| 17 | Docs only | **Docs + code, symbol-aware** | Excerpt should be a whole function |
| 18 | Setup in Phase 6 | **Setup in Phase 1** | New users hit Ollama errors for five phases |
| 19 | TUI Phase 4, file ops Phase 5 | **Useful CLI at end of Phase 3** | Core value should not land last |
| 20 | `temperature: 0.1` | **`0` + fixed seed** | Evals must be reproducible |

Preserved from v2: the exclusion-rule design and `.vektixignore` syntax, the fine-tuning roadmap,
pipelined indexing, batch embedding, and the embedding cache.
