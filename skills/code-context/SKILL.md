---
name: code-context
description: 'Code context system for AI agents and LLMs. Index codebases structurally with tree-sitter plus document-aware graph enrichment, provide efficient symbol search, dependency analysis, git-aware context, interactive graph exploration, and hybrid semantic search. Use when analyzing codebase structure, repository knowledge graphs, generating LLM context, or analyzing code dependencies.'
license: MIT
allowed-tools: Bash, Grep, Glob, Read, Edit, LSP
---

# Code Context System

## Overview

A code context system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. Designed to help AI understand codebases quickly.

## Why Use This Skill

- **For Code Analysis**: Quickly understand unfamiliar codebases with `map`, `explain`, `context`
- **For Graph Navigation**: Explore repository structure with `graph`, `graph path`, `graph neighbors`, `graph subgraph`, and interactive `graph html`
- **For Dependency Understanding**: Trace imports and find impact with `impact`, `impact-git`, `diff-impact`, and `trace`
- **For Git-aware Context**: Analyze changes with `git-files`, `snapshot-git`, `impact-git`, `review-context`, and graph-guided follow-up recommendations
- **For Semantic Search**: Use hybrid search combining keyword and semantic similarity
- **For LLM Context**: Generate focused context packages with `snapshot`
- **For Docs + Code Knowledge Graphs**: Bring `.md` / `.txt` documents into the same graph as files, symbols, modules, and packages

## Supported Languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| TypeScript | `.ts`, `.tsx` |
| JavaScript | `.js`, `.jsx`, `.mjs` |
| Python | `.py` |
| Rust | `.rs` |
| Java | `.java` |
| Markdown | `.md`, `.markdown` |
| Text | `.txt` |

## Quick Start

```bash
# 1. Index the codebase (do this first)
code-context index

# 2. Explore structure
code-context map

# 3. Search symbols
code-context search "Handler"

# 4. Check embedding cache coverage when embeddings are configured
code-context embedding-status
code-context embedding-plan
code-context embedding-namespaces
code-context embedding-prune --model text-embedding-old --dimensions 768  # dry run
code-context embedding-backfill          # dry run
code-context embedding-backfill --apply  # writes cache entries
code-context provider-doctor

# 5. Search provider-backed vectors when Helix + embeddings are configured
code-context vector-search "handler health check"

# 6. Fuse text/vector/graph when advanced capabilities are available
code-context hybrid-search "handler health check"

# 7. Build provider-neutral answer context without external model calls
code-context answer-templates
code-context answer-profiles
code-context answer "How is status served?" --profile project-review --context-only --json
code-context answer "How is status served?" --context-only
code-context answer "How is status served?" --context-only --dedupe --max-per-file 2 --max-context-chars 6000 --json
code-context answer "How is status served?" --template explain --format markdown
code-context answer "How is status served?" --profile review-change --format markdown
code-context answer "How is status served?" --require-citations --json
code-context answer "How is status served?" --min-citation-coverage 0.5 --json
code-context answer "How is status served?" --evaluate --min-evaluation-score 0.7 --json

# 8. Get detailed context
code-context context Engine

# 9. Explore graph relationships
code-context graph neighbors Engine

# 10. Generate project-wide or focused LLM context
code-context snapshot
code-context snapshot "authentication"

# 11. Open the interactive visual graph
code-context graph html > graph.html
```

## Configuration

Create `.code-context.yaml` in project root:

```yaml
root: .
db: .code-context/index.db
store:
  backend: sqlite
  sqlite:
    db: .code-context/index.db
  helix:
    url: http://localhost:6969
    api_key_env: HELIX_API_KEY
    project_id: my-repo
    timeout: 30s
    read_retry_attempts: 2
    read_retry_backoff: 50ms
    write_retry_attempts: 3
    write_retry_backoff: 50ms
embedding:
  provider: none
  # provider: openai-compatible
  # base_url: http://localhost:11434/v1
  # model: nomic-embed-text
  api_key_env: EMBEDDING_API_KEY
  timeout: 30s
  batch_size: 64
answer:
  provider: none
  # provider: openai-compatible
  # base_url: http://localhost:11434/v1
  # model: qwen2.5-coder
  api_key_env: ANSWER_API_KEY
  reranker: local
  timeout: 60s
  max_tokens: 1024
  temperature: 0.2
server:
  port: 9090
watch:
  enabled: false
  interval: 2s
  debounce: 250ms
```

`watch.*` settings apply both to the standalone `watch` command and to `serve --watch` background refresh.

For ready-to-copy recipes covering SQLite-only, Helix, local models, hosted providers, Answer
profiles, embedding migration, HTTP/MCP, and smoke validation, see `ai-docs/cookbook.md`.

SQLite is the default storage backend; `store.backend: helix` enables the HelixDB-backed store.
If no Helix URL is configured, the Helix Go SDK uses its local default endpoint (`http://localhost:6969`).
Helix data is scoped by `project_id`; when omitted, the CLI/MCP server use the absolute root path.
Helix HTTP timeout plus read/write retry behavior are configurable with `store.helix.timeout`,
`store.helix.read_retry_attempts`, `store.helix.read_retry_backoff`,
`store.helix.write_retry_attempts`, and `store.helix.write_retry_backoff` or the matching CLI/MCP flags.
For Helix runtime validation, prefer a dedicated temporary instance and run:
`HELIX_URL=http://localhost:6970 HELIX_PROJECT_ID=code-context-smoke scripts/helix-smoke.sh`.
The smoke starts deterministic local OpenAI-compatible fake embedding and chat-completions servers
and verifies `status` capabilities, embedding lifecycle status, namespace inventory, prune
dry-run/apply safety, `answer --context-only`, provider-backed `answer`, `/api/text`, real
`/api/vector` query-text results, `/api/hybrid` vector fusion, `/api/answer`, and
`/api/graph/traverse` through `serve`; keep `CODE_CONTEXT_HELIX_SMOKE_PORT`,
`CODE_CONTEXT_HELIX_EMBEDDING_PORT`, and `CODE_CONTEXT_HELIX_ANSWER_PORT` free or set them to
available ports.
Advanced Helix-backed features should stay behind provider-neutral optional interfaces in
`internal/store/capabilities.go`; do not leak Helix SDK types into engine, search, graph, CLI, or MCP callers.
The Helix backend implements `TextSearcher` with BM25 over symbol `search_text` and document
metadata/summary `search_text`, `EmbeddingCache`/`VectorSearcher` over namespaced embedding chunk
nodes, plus `GraphTraverser` over the indexed file/symbol/import/call/route/document-link graph;
consumers should use the interfaces and keep fallbacks for providers that do not implement them.
Embedding support is optional and disabled by default. Configure `embedding.provider:
openai-compatible` with a local or hosted `/embeddings` API when semantic/vector phases are needed;
the project should keep embedding, vector search, hybrid search, and future RAG behavior behind
provider-neutral interfaces. SQLite and Helix implement the provider-neutral embedding cache; Helix
stores vectors in model+dimension-namespaced chunk properties so different embedding spaces are not
mixed in one vector index. Use `vector-search`, `/api/vector`, or MCP `vector_search`/
`code_context_vector_search` to call `VectorSearcher`; query-text search additionally requires a
configured `Embedder`. Use `embedding-status`, `/api/embedding-status`, or MCP
`embedding_status`/`code_context_embedding_status` for a read-only lifecycle summary with provider
configuration, coverage, namespaces, prune candidates, and recommended next actions. Use `embedding-plan`, `/api/embedding-plan`, or MCP
`code_context_embedding_plan` to inspect cache coverage and plan model backfills without calling the
embedding provider; use `embedding-backfill`, `POST /api/embedding-backfill?apply=true`, or MCP
`code_context_embedding_backfill` to fill missing/stale chunks, noting that backfill is dry-run by
default and only calls the provider when explicitly applied. Use `embedding-namespaces`,
`/api/embedding-namespaces`, or MCP `embedding_namespaces`/`code_context_embedding_namespaces` to
inventory existing model/dimension vector spaces before switching models or planning cleanup. Use
`embedding-prune`, `POST /api/embedding-prune`, or MCP `embedding_prune`/
`code_context_embedding_prune` to delete an old namespace; it is dry-run by default and refuses to
delete the currently configured namespace unless explicitly forced. Use `hybrid-search`, `/api/hybrid`, or MCP `hybrid_search`/
`code_context_hybrid_search` to fuse text, vector, and graph signals while degrading to the
capabilities present in the selected backend. The engine fallback normalizes each source's scores
per query before applying weights, and stores raw score, normalized score, rank, contribution, and
fusion details under `hybrid_*` metadata for explainable ranking. Query-focused `context`,
`snapshot`, HTTP responses, and agent `code_context_explore`/`code_context_context`/
`code_context_snapshot` also expose best-effort `hybrid_hits` as optional evidence; consumers should
treat the field as additive and not backend-specific.
Answer/RAG support follows the same provider-neutral rule: `answer`, `POST /api/answer`, and MCP
`answer`/`code_context_answer` build context from hybrid retrieval first, then call a configured
`Answerer` only when `answer.provider` is enabled. Use `--context-only` or JSON
`{"context_only": true}` to inspect evidence without any external model call. Answer results expose
stable citation/source metadata (`[1]`, `[2]`, ...), and callers can override `system_prompt` or pass
prior `messages` without coupling to a specific backend. Callers can also select prompt presets via
`template`/`--template` (`general`, `explain`, `review`, `plan`); explicit `system_prompt` still
overrides the preset text. Use CLI `answer-templates`, HTTP `/api/answer-templates`, or MCP
`answer_templates` to discover the current catalog. Callers can also select workflow profiles via
`profile`/`--profile` (`explain-code`, `review-change`, `plan-implementation`, `risk-analysis`,
`test-plan`) to preconfigure template, retrieval defaults, and grounding policy. Use CLI
`answer-profiles`, HTTP `/api/answer-profiles`, or MCP `answer_profiles` to discover built-in and
configured profiles. Project/user config can define `answer.profiles` entries that extend or
override built-ins by normalized name; profiles can include retrieval, rerank, grounding, and
evaluation defaults. `provider-doctor`, HTTP `/api/provider-diagnostics`, and MCP provider
diagnostics also validate configured profiles so unsupported templates, target kinds, and numeric
ranges are caught before selecting a profile at runtime.
Answer retrieval can be scoped/tuned with provider-neutral `filter`,
source weights, and graph `expand_from`/`expand_max_depth` controls. Retrieved context can then be
post-processed through the provider-neutral `AnswerReranker` hook with CLI `--min-score`,
`--dedupe`, `--max-per-file`, `--max-context-item-chars`, and `--max-context-chars` or JSON/MCP
`min_context_score`, `dedupe_context`, `max_per_file`, `max_context_item_chars`, and
`max_context_chars`. Set `answer.reranker: semantic` or pass `--answer-reranker semantic` to use
the configured `Embedder` as a semantic reranking provider before local constraints are applied;
results include a `retrieval` report. Use CLI `--format markdown` or
MCP `format: "markdown"` for agent-readable answers with a `Sources` section; use JSON for
structured consumers. Provider-backed answers include a deterministic `grounding` citation-label
audit (not semantic fact-checking) with valid/missing/uncited source labels; CLI
`--require-citations` / `--min-citation-coverage` or JSON/MCP `require_citations` /
`min_citation_coverage` marks the audit as required for hard-gate callers. CLI `--evaluate` or
JSON/MCP `evaluate` runs the local provider-neutral `AnswerEvaluator` hook, which reports answer
presence, evidence-overlap, and citation-grounding checks under `evaluation`; `--min-evaluation-score`
or `min_evaluation_score` makes that score a caller-visible gate.

## Recommended Dogfood Workflow

Use this sequence in the current repository to demonstrate the main value chain:

```bash
code-context index
code-context map
code-context snapshot
code-context search Snapshot
code-context answer "How does Snapshot build context?" --context-only
code-context context Snapshot
code-context impact Snapshot
code-context impact internal/engine/engine.go
code-context impact-git --state all
code-context review-context --state all
code-context ci
```

This moves from repository overview, to symbol understanding, to graph-aware LLM context, to file/symbol/git impact, and finally to CI health checks.

## Core Commands

### Indexing

```bash
code-context index                       # full index
code-context index --incremental         # only changed files
code-context index -v                    # verbose progress
code-context stats                       # includes document counts
```

By default, test files are excluded from indexing so graph analysis and recommendations stay focused on production code.

Documents (`.md`, `.markdown`, `.txt`) are also indexed and linked into the graph. Index stats now include document counts in addition to code file counts.

### Watch / Workflow Status

```bash
code-context watch --enabled
code-context status
code-context serve --watch
```

Use `status` to inspect provider capabilities, index version, last indexed timestamp, and current watch refresh state.

### Search

```bash
code-context search "Handler"           # keyword search
code-context search "Handler" --hybrid  # semantic hybrid search
code-context search "Handler" --kind function --limit 20
code-context search "Handler" --limit 50
```

### Find Definition & References

```bash
code-context find-def "Engine"          # find symbol definition
```

### Project Architecture

```bash
code-context map                         # show directory structure with stats
```

Includes repository-level graph analysis, bridge/hotspot insights, and recommended files.

### Explain a File

```bash
code-context explain internal/engine/engine.go
```

Shows:
- File path and language
- All symbols in the file (functions, types, methods)
- Imports (what this file imports)
- Importers (what files import this file)
- Nearby files and graph-derived recommendations

### Symbol Context

```bash
code-context context Engine
```

Shows:
- Definition location and signature
- Methods (if it's a type)
- Related symbols across the codebase
- Related files and recommended next files from the graph
- Related documents when docs mention the symbol or surrounding files
- Bridge files, hotspots, and suggested reading paths when available

### Generate LLM Context (Snapshot)

```bash
code-context snapshot                    # project-wide context
code-context snapshot "parser"           # query-based context
code-context snapshot "parser" --limit 3 # limit files
```

Generates a project-wide or query-focused context package for LLM consumption with:
- Related files and their symbols
- Related documents when relevant
- Summary of what was found
- Graph summaries, relation highlights, reading paths, and recommended next files

### Graph Exploration

```bash
code-context graph
code-context graph --focus Engine
code-context graph path Engine Server
code-context graph neighbors internal/engine/engine.go --limit 5
code-context graph subgraph Engine --depth 2
code-context graph traverse docs/health.md --edge references --include-paths --limit 10
code-context graph traverse "text:Health" --edge similar --target-kind symbol --include-paths
code-context graph html --focus internal/server/server.go > graph.html
```

Use graph commands to export graph JSON, inspect adjacency, find file-level paths, run provider-backed traversals, and focus on local subgraphs.
Provider-backed traversal accepts target strings (`docs/health.md`, `symbol:Name@path`, `GET /route`,
`text:query`), edge groups (`code`, `docs`, `symbols`, `entrypoints`), filters
(`--target-kind`, `--file-pattern`, `--metadata key=value`), and `--include-paths` for shortest
paths from the start target.
When the active backend supports `GraphTraverser`, higher-level outputs such as `explain`,
`context`, `impact`, `route-context`, `snapshot`, and matching MCP tools include best-effort
provider graph traversal summaries; SQLite/local fallback output simply omits those optional fields.
Focused `context`/`snapshot` output and agent `explore` additionally surface hybrid retrieval hits
when text/vector/graph signals are available, while still falling back to local text evidence.

Graph exports are versioned as `graph-export.v2` and include a richer repository graph with:

- `file`, `symbol`, `module`, `package`, and `document` nodes
- document/code edges such as `mentions_file`, `mentions_symbol`, and `describes`
- structure edges such as `belongs_to`, `declares_package`, `represents`, and `resolves_to`

### Interactive Graph HTML

```bash
code-context graph html > graph.html
code-context graph html --focus internal/server/server.go > graph.html
```

The HTML graph is now a canvas-first interactive view with:

- pan / zoom / drag
- minimap navigation
- node search and type filtering
- edge filtering
- cluster mode (`type`, `module`, `none`)
- document-focused mode
- hover tooltips for nodes and edges
- 1-hop / 2-hop focus modes
- visible selected-node action buttons
- embedded node content viewer for code and text

Selected-node actions include:

- **Open content**
- **Center node**
- **Pin / Unpin node**
- **1-hop / 2-hop / reset focus**

The node content modal supports:

- file, document, and symbol content previews
- code-oriented vs text-oriented rendering
- copy content
- expand / collapse view
- show file path

### Trace Call Chain

```bash
code-context trace New SearchSymbols     # trace between two symbols
```

Shows the path from one symbol to another through the import graph.

### Diff Impact Analysis

```bash
code-context diff-impact internal/store/sqlite.go
code-context diff-impact internal/store/sqlite.go --depth 2
```

Shows:
- Direct dependencies
- All dependencies (transitive)
- Dependent files (that import this)
- Recommended test files to run

## Git-aware Commands

### List Changed Files

```bash
code-context git-files                   # unstaged changes
code-context git-files --state unstaged  # unstaged (default)
code-context git-files --state staged    # staged changes
code-context git-files --state all       # all changes
```

### Rich Diff Output

```bash
code-context git-diff                    # unstaged diff
code-context git-diff --state staged     # staged diff
code-context git-diff --context 5        # show 5 context lines
```

Shows:
- File path
- Hunk headers (old/new line numbers)
- Changed code with context

- `snapshot-git` now includes graph summaries, relation highlights, and recommended next files for changed files

```bash
code-context snapshot-git                # context for unstaged
code-context snapshot-git --state all   # context for all changes
code-context snapshot-git --limit 10    # limit files
```

### Diff Impact from Git Changes

```bash
code-context impact-git                  # unified impact for unstaged changes
code-context impact-git --state all --json
code-context diff-impact-git             # file-level impact for unstaged
code-context diff-impact-git --state staged --depth 2
```

## Use Cases

### 1. Understanding a New Codebase

```bash
code-context map
code-context search "Engine"
code-context context Engine
code-context explain internal/engine/engine.go
```

### 2. Finding Implementation Details

```bash
code-context find-def "NewRouter"
code-context context NewRouter
```

### 3. Generating LLM Context

```bash
code-context snapshot
code-context snapshot "authentication"
code-context explain internal/auth/auth.go
```

### 3b. Exploring docs + code together

```bash
code-context graph --focus README.md
code-context context ExportGraph
code-context graph html > graph.html
```

Use this flow when you want to see how README/docs mention specific files or symbols.

### 4. Understanding Dependencies

```bash
code-context imports internal/store/sqlite.go
code-context importers "internal/api"
code-context diff-impact internal/store/sqlite.go
```

### 5. Tracing Code Flow

```bash
code-context trace "main" "Engine"
```

### 6. Analyzing Git Changes

```bash
code-context git-files --state all
code-context git-diff --context 3
code-context snapshot-git --state unstaged
code-context impact-git --state all
code-context review-context --state all
code-context diff-impact-git --state staged
```

## HTTP API

Start server: `code-context serve --port 9090`

### Search Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?`, `hybrid?` | Search symbols (add `hybrid=true` for semantic) |
| GET | `/api/semantic-search` | `q`, `kind?`, `limit?` | Semantic hybrid search |
| GET | `/api/text` | `q`, `file?`, `limit?` | Text search |
| POST | `/api/vector` | JSON `VectorSearchQuery` | Provider-backed vector search |
| POST | `/api/hybrid` | JSON `HybridSearchQuery` | Provider-neutral text/vector/graph fusion |
| POST | `/api/answer` | JSON `AnswerOptions` (`question`, `context_only?`, `filter?`, weights, `profile?`, `template?`, `min_context_score?`, `dedupe_context?`, `max_per_file?`, `max_context_chars?`, `max_context_item_chars?`, `require_citations?`, `min_citation_coverage?`, `evaluate?`, `min_evaluation_score?`, `system_prompt?`, `messages?`) | Build answer context and optionally call configured `Answerer`; retrieval report appears under `retrieval`, optional local evaluation under `evaluation` |
| GET | `/api/answer-templates` | `include_prompts?` | List built-in provider-neutral answer templates |
| GET | `/api/answer-profiles` | | List built-in and configured provider-neutral answer workflow profiles |
| GET | `/api/provider-diagnostics` | | Local embedding/answer provider configuration checks |

### Symbol Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/symbols` | `file` | List file symbols |
| GET | `/api/definitions` | `name` | Find definitions |
| GET | `/api/references` | `name` | Find references |

### Dependency Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/imports` | `file` | Get imports |
| GET | `/api/importers` | `source` | Get importers |

### Analysis Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/map` | — | Project architecture with graph analysis |
| GET | `/api/explain` | `file` | File summary |
| GET | `/api/context` | `name` | Symbol profile |
| GET | `/api/snapshot` | `q`, `limit?` | LLM context package |
| GET | `/api/trace` | `from`, `to` | Call chain |
| GET | `/api/diff-impact` | `file`, `depth?` | Change impact |
| GET | `/api/graph` | `focus?` | Export repository or focused graph |
| GET | `/api/graph/html` | `focus?` | Interactive graph HTML view |
| GET | `/api/graph/path` | `from`, `to` | Find file-level path through graph |
| GET | `/api/graph/neighbors` | `target`, `limit?` | Adjacent graph context |
| GET | `/api/graph/subgraph` | `target`, `depth?` | Local graph around a file or symbol |
| POST | `/api/graph/traverse` | JSON `GraphTraversalQuery` | Provider-backed graph traversal |

### Git-aware Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/git/files` | `state?` | Changed files |
| GET | `/api/git/diff` | `state?`, `context?` | Rich diff output |
| GET | `/api/snapshot-git` | `state?`, `limit?` | Context from git |
| GET | `/api/diff-impact-git` | `state?`, `depth?` | Impact from git |

### System Endpoints

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/stats` | — | Index stats with version metadata |
| GET | `/api/status` | — | Workflow/service status including provider capabilities and watch metadata |
| GET | `/api/embedding-status` | `limit?` | Embedding lifecycle summary with recommendations |
| GET | `/api/embedding-lifecycle` | `limit?` | Alias for `/api/embedding-status` |
| GET | `/api/embedding-plan` | `limit?` | Embedding cache coverage and backfill plan |
| POST | `/api/embedding-backfill` | `apply?`, `limit?` | Dry-run or apply embedding backfill |
| GET | `/api/embedding-namespaces` | — | Cached embedding model/dimension namespace inventory |
| POST | `/api/embedding-prune` | `model`, `dimensions`, `apply?`, `force_current?` | Dry-run or delete a selected embedding namespace |
| POST | `/api/index` | `incremental?` | Re-index |

## MCP Server

Use MCP server to expose code-context capabilities to AI agents (Claude Desktop, Cursor, etc.).

### HTTP API

- `GET /api/map` — architecture overview with graph analysis
- `GET /api/graph` — graph JSON export (`graph-export.v2`)
- `GET /api/graph/html` — interactive canvas graph HTML view with docs + code nodes
- `GET /api/graph/path?from=...&to=...` — graph path lookup
- `GET /api/graph/neighbors?target=...` — neighboring graph context
- `GET /api/graph/subgraph?target=...&depth=...` — focused local graph
- `POST /api/graph/traverse` — provider-backed graph traversal with `GraphTraversalQuery`
- `POST /api/answer` — provider-neutral RAG context/answer endpoint (`context_only` avoids external model calls)
- `GET /api/provider-diagnostics` — local embedding/answer provider configuration checks
- `GET /api/stats` — index stats with version metadata
- `GET /api/status` — workflow/service status with provider capabilities and watch metadata
- `GET /api/embedding-status` — embedding lifecycle summary with recommendations
- `GET /api/embedding-plan` — embedding cache coverage and backfill plan
- `POST /api/embedding-backfill?apply=true` — apply missing/stale embedding backfill
- `GET /api/embedding-namespaces` — cached embedding model/dimension namespace inventory
- `POST /api/embedding-prune?model=...&dimensions=...` — dry-run or delete a selected embedding namespace
- `POST /api/index?incremental=true` — trigger refresh

### MCP Server


The MCP server exposes these tools:

- `search`
- `find_def`
- `map`
- `explain`
- `context`
- `snapshot`
- `trace`
- `diff_impact`
- `graph`
- `graph_path`
- `graph_neighbors`
- `graph_subgraph`

Graph-related MCP responses return the same `graph-export.v2` structures used by the CLI and HTTP APIs, including document nodes and richer relation edges.

For workflow health, pair MCP usage with the HTTP `status` endpoint or CLI `status` command.

### Build

```bash
go build -o code-context-mcp ./cmd/mcp
```

### Configuration

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "code-context": {
      "command": "/path/to/code-context-mcp",
      "args": ["--root", "."]
    }
  }
}
```

### Available Tools

| Tool | Description | Parameters |
|---|---|---|
| `index` | Index the codebase | - |
| `search` | Search symbols by name | `query` |
| `find_def` | Find symbol definition | `name` |
| `find_refs` | Find symbol references | `name` |
| `files` | List indexed files | `language?` |
| `git_files` | List changed files | `state?` |
| `imports` | Show file imports | `file` |
| `importers` | Find importing files | `source` |
| `stats` | Index statistics | - |
| `code_context_embedding_status` | Embedding lifecycle summary and recommendations | `limit?` |
| `code_context_embedding_plan` | Embedding cache coverage and backfill plan | `limit?` |
| `code_context_embedding_backfill` | Dry-run or apply missing/stale embedding backfill | `apply?`, `limit?` |
| `code_context_embedding_namespaces` | Cached embedding model/dimension namespace inventory | - |
| `code_context_embedding_prune` | Dry-run or delete a cached embedding namespace | `model`, `dimensions`, `apply?`, `force_current?` |
| `map` | Project architecture with graph analysis | - |
| `graph` | Export repository or focused graph JSON | `focus?` |
| `graph_path` | Find a file-level path through the graph | `from`, `to` |
| `graph_neighbors` | Show adjacent graph context for a file or symbol | `target`, `limit?` |
| `graph_subgraph` | Export a local graph around a file or symbol | `target`, `depth?` |
| `explain` | File summary with graph guidance | `file` |
| `context` | Symbol profile with graph guidance | `symbol` |
| `snapshot` | Generate project-wide or query-focused LLM context with recommendations | `query?`, `limit?` |
| `snapshot_git` | Context from git | `state?`, `limit?` |
| `impact` | Unified file or symbol impact analysis | `target`, `depth?` |
| `impact_git` | Unified impact analysis for local git changes | `state?`, `depth?` |
| `diff_impact` | Change impact analysis | `file`, `depth?` |
| `diff_impact_git` | Impact from git | `state?`, `depth?` |
| `trace` | Call chain tracing | `from`, `to` |

### Example MCP Usage

- `graph` with focus: `{ "focus": "Engine" }`
- `graph_path`: `{ "from": "Engine", "to": "Server" }`
- `graph_neighbors`: `{ "target": "Engine", "limit": 5 }`
- `graph_subgraph`: `{ "target": "Engine", "depth": 2 }`
- `graph_traverse`: `{ "target": "docs/health.md", "edge_kinds": ["references"], "include_paths": true, "limit": 10 }`
- `graph_traverse`: `{ "target": "text:Health", "edge_kinds": ["similar"], "filter": { "target_kinds": ["symbol"] }, "include_paths": true, "limit": 10 }`

Expect graph payloads to include node types like `file`, `symbol`, `module`, `package`, `document` and edge types like `mentions_file`, `mentions_symbol`, `describes`, `belongs_to`, `declares_package`, `represents`, and `resolves_to`.

## Tips

- Run `code-context index` first before any search commands
- Use `snapshot` without a query for project-wide LLM context, or with a query for focused context
- Use `map` to understand project structure quickly
- Use `graph neighbors` or `graph subgraph` when a symbol/file is too isolated in plain search results
- Use `graph html` when you need a visual-first graph with document nodes, node actions, and content previews
- Use `--hybrid` flag with search for semantic matching
- Use git-aware commands (`git-files`, `git-diff`, `snapshot-git`, `impact-git`, `review-context`) to analyze changes
- Prefer `impact <file-or-symbol>` for new workflows; use `diff-impact` for legacy file-only impact checks
- Test files are excluded from indexing by default, so outputs stay focused on production code
- Create a `.code-context.yaml` config file for persistent settings
