# code-context

A code context system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. Pure Go, single binary.

## Features

- **Structural parsing** — tree-sitter AST, not regex
- **FTS5 symbol search** — fast full-text search on symbol names
- **Definition lookup** — find where symbols are defined
- **Import graph** — dependency analysis with BFS traversal, related-file scoring, path lookup, neighbors, and subgraph export
- **Call graph** — callers/callees lookup plus graph `calls` edges for symbol-level impact analysis
- **Framework route index** — extracts common HTTP routes from Go, TypeScript/JavaScript, Python, Java, and Rust frameworks
- **Document drift diagnostics** — links docs back to files, symbols, and modules, then reports stale references
- **Graph-aware context** — `map`, `context`, `snapshot`, and `snapshot-git` include graph summaries, bridge/hotspot insights, and recommended files
- **Context generation** — generate code context for LLM consumption
- **Trace & impact analysis** — understand code flow and change impact
- **HTTP API** — CLI parity for programmatic access, including graph endpoints
- **Incremental indexing** — only reindex changed files (content-hash based)
- **Test-file exclusion by default** — test sources are skipped during indexing to keep graph and context focused on production code
- **Pure Go SQLite** — `modernc.org/sqlite`, no external DB
- **Single binary** — no runtime dependencies

## Supported Languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| TypeScript | `.ts`, `.tsx` |
| JavaScript | `.js`, `.jsx`, `.mjs` |
| Python | `.py` |
| Rust | `.rs` |
| Java | `.java` |

## Installation

```bash
go install github.com/sjzsdu/code-context/cmd/code-context@latest
```

Or build from source:

```bash
git clone https://github.com/sjzsdu/code-context.git
cd code-context
go build -o code-context ./cmd/code-context
```

## Quick Start

```bash
# Index your project
code-context index

# Get project overview with graph analysis
code-context map

# Search for symbols
code-context search "Server"

# Inspect graph relationships around a symbol or file
code-context graph neighbors Engine
code-context graph path internal/engine/engine.go internal/server/server.go
code-context graph traverse docs/health.md --edge references --include-paths
code-context graph traverse "text:Health" --edge similar --target-kind symbol

# Generate LLM context with graph recommendations
code-context snapshot
code-context snapshot "authentication"

# Analyze change impact
code-context impact Engine --json
code-context impact internal/store/sqlite.go --depth 2
code-context impact-git --state all --json
code-context diff-impact internal/store/sqlite.go
code-context symbol-impact Engine

# Inspect app surface and documentation health
code-context routes
code-context route-context /api/users
code-context doc-drift
code-context doc-coverage

# Check and repair index health
code-context doctor
code-context freshness
code-context embedding-status
code-context embedding-plan
code-context embedding-namespaces
code-context embedding-prune --model text-embedding-old --dimensions 768
code-context embedding-backfill --apply
code-context rebuild

# Show index stats
code-context stats

# Start HTTP server
code-context serve --port 9090
```

## Configuration

`code-context` supports user-level and project-level config files. Values are merged with this
precedence, from highest to lowest:

1. CLI flags
2. Project config in the current directory tree
3. User config under `~/.code-context/`
4. Built-in defaults

Project config files are discovered by walking from the selected root/current directory upward.
Supported project config names, in lookup order within each directory:

- `.code-context/config.yaml`
- `.code-context/config.yml`
- `.code-context/config.json`
- `.code-context.yaml` (legacy)
- `.code-context.yml` (legacy)
- `.code-context.json` (legacy)

Supported user config names:

- `~/.code-context/config.yaml`
- `~/.code-context/config.yml`
- `~/.code-context/config.json`

SQLite remains the default storage backend. `store.backend: helix` enables the HelixDB-backed
store; if `store.helix.url` / `--helix-url` is omitted, the Helix Go SDK uses its local default
endpoint (`http://localhost:6969`).
Helix data is scoped by `project_id`; if omitted, the CLI and MCP server use the absolute root path
as the project namespace.

Supported options:

| Key | Type | Description |
|---|---|---|
| `root` | string | Codebase root directory |
| `db` | string | SQLite database path (legacy shorthand for `store.sqlite.db`) |
| `store.backend` | string | Storage backend: `sqlite` or `helix` |
| `store.sqlite.db` | string | SQLite database path |
| `store.helix.url` | string | HelixDB endpoint URL |
| `store.helix.api_key` | string | HelixDB API key |
| `store.helix.api_key_env` | string | Environment variable containing the HelixDB API key |
| `store.helix.project_id` | string | Helix project namespace (default: absolute root) |
| `embedding.provider` | string | Embedding provider: `none`, `openai`, or `openai-compatible` |
| `embedding.base_url` | string | Embedding API base URL (`/embeddings` is appended for OpenAI-compatible providers) |
| `embedding.api_key` | string | Embedding API key |
| `embedding.api_key_env` | string | Environment variable containing the embedding API key |
| `embedding.model` | string | Embedding model name |
| `embedding.dimensions` | int | Optional requested embedding dimensions |
| `embedding.timeout` | duration | Embedding request timeout |
| `embedding.batch_size` | int | Maximum embedding batch size |
| `answer.provider` | string | Answer provider: `none`, `openai`, or `openai-compatible` |
| `answer.base_url` | string | Answer API base URL (`/chat/completions` is appended for OpenAI-compatible providers) |
| `answer.api_key` | string | Answer API key |
| `answer.api_key_env` | string | Environment variable containing the answer API key |
| `answer.model` | string | Chat/answer model name |
| `answer.timeout` | duration | Answer request timeout |
| `answer.max_tokens` | int | Default max completion tokens |
| `answer.temperature` | number | Default answer sampling temperature |
| `server.port` | int | HTTP server port |
| `watch.enabled` | bool | Enable watch mode / background refresh by default |
| `watch.interval` | duration | Polling interval for incremental refresh |
| `watch.debounce` | duration | Minimum delay between follow-up refreshes |
| `docs.fail_on_broken` | bool | Default `doc-drift --fail-on-broken` |
| `docs.min_route_coverage` | number | Default `doc-coverage --min-route-coverage` percentage |
| `docs.min_symbol_coverage` | number | Default `doc-coverage --min-symbol-coverage` percentage |

Example (`.code-context/config.yaml`):

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
embedding:
  provider: none
  # OpenAI-compatible local examples:
  # provider: openai-compatible
  # base_url: http://localhost:11434/v1
  # model: nomic-embed-text
  api_key_env: EMBEDDING_API_KEY
  dimensions: 0
  timeout: 30s
  batch_size: 64
answer:
  provider: none
  # OpenAI-compatible local or hosted chat-completions API:
  # provider: openai-compatible
  # base_url: http://localhost:11434/v1
  # model: qwen2.5-coder
  api_key_env: ANSWER_API_KEY
  timeout: 60s
  max_tokens: 1024
  temperature: 0.2
  profiles:
    - name: project-review
      description: Project-specific review profile
      template: review
      limit: 8
      filter:
        target_kinds: [symbol, file, document]
      dedupe_context: true
      max_per_file: 2
      max_context_chars: 6000
      require_citations: true
      min_citation_coverage: 0.2
      evaluate: true
      min_evaluation_score: 0.6
server:
  port: 9090
watch:
  enabled: false
  interval: 2s
  debounce: 250ms
docs:
  fail_on_broken: true
  min_route_coverage: 80
  min_symbol_coverage: 60
```

## CLI Commands

### `config inspect` — Show merged configuration

Prints the effective merged configuration and the config files that contributed to it.

```bash
code-context config inspect
code-context config inspect --format yaml
```

### `onboard` — Generate a starter config file

Creates `.code-context/config.yaml` in the target directory. Existing config files are not
overwritten unless `--force` is provided.

```bash
code-context onboard
code-context onboard --dir .
code-context onboard --dir /path/to/repo --force
code-context onboard --global
```

### `index` — Index the codebase

```bash
code-context index                       # full index
code-context index --incremental         # only changed files
code-context index -v                    # verbose progress
```

#### Recommended dogfood workflow for this repository

```bash
code-context index
code-context map
code-context snapshot
code-context search Snapshot
code-context embedding-status
code-context embedding-plan
code-context embedding-namespaces
code-context embedding-prune --model text-embedding-old --dimensions 768
code-context vector-search Snapshot
code-context hybrid-search Snapshot
code-context answer "How does Snapshot build context?" --context-only
code-context context Snapshot
code-context impact Snapshot
code-context impact internal/engine/engine.go
code-context impact-git --state all
code-context review-context --state all
code-context ci
```

Use this sequence to move from repository overview, to symbol-level understanding, to file/symbol/git impact, and finally to CI-style health checks.

By default, test files are excluded from indexing so graph analysis and context stay focused on production code.

### `map` — Project architecture overview

```bash
code-context map
```

Shows directory structure with file/symbol counts plus repository-level graph analysis, bridge/hotspot insights, and recommended files.

### `search <query>` — Search symbols by name

```bash
code-context search "Handler"
code-context search "parse" --kind function --limit 20
```

### `vector-search [query]` — Search provider-backed embedding vectors

Requires a backend that implements `VectorSearcher` (Helix) and either a configured embedding
provider for query text or a raw vector supplied with `--vector`.

```bash
code-context vector-search "handler health check" --limit 10
code-context vector-search --vector 0.1,0.2,0.3 --model text-embedding-test --dimensions 3 --json
```

### `embedding-status` — Summarize embedding lifecycle state

Combines embedding configuration, current model cache coverage, cached namespaces, prune candidates,
and recommended next actions. `embedding-lifecycle` is an alias.

```bash
code-context embedding-status
code-context embedding-status --json --limit 100
```

### `embedding-plan` — Inspect embedding cache coverage

Computes the expected symbol/document embedding chunks for the currently configured embedding
model namespace without calling the embedding provider. Use it before or after switching models to
see how many chunks are already cached and how many need a normal `index`/`rebuild` backfill.

```bash
code-context embedding-plan
code-context embedding-plan --json --limit 100
```

### `embedding-namespaces` — List cached embedding model namespaces

Lists all cached model + dimension namespaces in the active embedding cache, including chunk counts,
input kinds, target kinds, and last update timestamps. Use it before changing embedding providers or
models to see which vector spaces already exist and which ones can be cleaned up later.

```bash
code-context embedding-namespaces
code-context embedding-namespaces --json
```

### `embedding-prune` — Delete an old embedding namespace

Dry-runs deletion of a selected model + dimension namespace by default. Pass `--apply` only after
reviewing `embedding-namespaces`; pruning the currently configured embedding namespace is blocked
unless `--force-current` is also provided.

```bash
code-context embedding-prune --model text-embedding-old --dimensions 768
code-context embedding-prune --model text-embedding-old --dimensions 768 --apply
```

### `embedding-backfill` — Fill missing/stale embedding cache entries

Runs the same plan, then embeds only missing/stale chunks for the configured model namespace. It is
dry-run by default; pass `--apply` to call the embedding provider and write cache entries.

```bash
code-context embedding-backfill          # dry run
code-context embedding-backfill --apply  # write missing/stale cache entries
code-context embedding-backfill --apply --limit 100
```

### `hybrid-search [query]` — Fuse text, vector, and graph signals

Uses provider-neutral `TextSearcher`, `VectorSearcher`, and `GraphTraverser` capabilities when
available. Without embeddings it degrades to text/graph fusion; with Helix vectors and an embedding
provider it also includes semantic vector hits. The engine fallback normalizes each source's scores
per query before applying weights, then records raw score, normalized score, rank, contribution, and
fusion metadata in each hit for explainable tuning.

```bash
code-context hybrid-search "handler health check" --limit 10
code-context hybrid-search "handler health check" --text-weight 0.5 --vector-weight 0.4 --graph-weight 0.1
```

### `answer <question>` — Provider-neutral RAG answer

Builds answer context from hybrid retrieval, then optionally calls the configured `Answerer`.
The provider is disabled by default, so `--context-only` is the safe way to preview the retrieved
evidence without any external model call. Results include a provider-neutral `sources` list with
stable citation labels (`[1]`, `[2]`, ...), and `--system-prompt` can override the default answer
instruction without changing retrieval. `--template general|explain|review|plan` selects a reusable
provider-neutral answer prompt preset; `--system-prompt` still has highest priority when both are
set. `--profile explain-code|review-change|plan-implementation|risk-analysis|test-plan` selects a
workflow profile that preconfigures template, retrieval defaults, and grounding policy; explicit
flags still override profile defaults. Retrieval can be scoped/tuned with the same provider-neutral
filter and fusion controls used by `hybrid-search` (`--target-kind`, `--file-pattern`,
`--metadata`, `--text-weight`, `--vector-weight`, `--graph-weight`, `--expand-from`). Retrieved
context can also be post-processed before answering with `--min-score`, `--dedupe`,
`--max-per-file`, `--max-context-item-chars`, and `--max-context-chars`; the JSON/Markdown result
includes a `retrieval` report. Use
`--format markdown` for agent-readable answer output with a `Sources` section, or `--json` for
machine-readable payloads. Provider-backed answers include a local citation grounding audit
(citation-label coverage, not semantic fact-checking) in `grounding`; `--require-citations` turns
missing/unknown retrieved-source citations into a CLI failure after printing the result.
`--min-citation-coverage 0.5` can require a minimum cited-source coverage ratio.
Use `--evaluate` to include a deterministic local answer evaluation report with answer presence,
evidence-overlap, and citation-grounding checks. `--min-evaluation-score 0.7` turns that local
evaluation score into a CLI success gate without calling any extra model.
Use `answer-templates` or `GET /api/answer-templates` to discover available built-in templates.
Use `answer-profiles` or `GET /api/answer-profiles` to discover built-in and configured workflow profiles.

```bash
code-context answer-templates
code-context answer-profiles
code-context answer "Where is status served?" --profile project-review --context-only --json
code-context answer "Where is status served?" --context-only
code-context answer "Where is status served?" --context-only --format markdown
code-context answer "Where is status served?" --context-only --target-kind symbol --text-weight 0.7 --vector-weight 0.3
code-context answer "Where is status served?" --context-only --dedupe --max-per-file 2 --max-context-chars 6000 --json
code-context answer "Where is status served?" --template explain --format markdown
code-context answer "Where is status served?" --profile review-change --format markdown
code-context answer "Where is status served?" --answer-provider openai-compatible --answer-base-url http://localhost:11434/v1 --answer-model qwen2.5-coder
code-context answer "Where is status served?" --system-prompt "Answer briefly and cite sources."
code-context answer "Where is status served?" --require-citations --json
code-context answer "Where is status served?" --min-citation-coverage 0.5 --json
code-context answer "Where is status served?" --evaluate --min-evaluation-score 0.7 --json
```

### `find-def <name>` — Find definition of a symbol

```bash
code-context find-def "NewServer"
```

### `explain <file>` — File summary

```bash
code-context explain internal/engine/engine.go
```

Shows symbols, imports, importers, nearby files, and graph-derived recommendations for a file.

### `context <symbol>` — Symbol profile

```bash
code-context context Engine
```

Shows definition, methods, related symbols, related files, and graph-guided reading suggestions with bridge/hotspot insights.

### `snapshot [query]` — Generate LLM context

```bash
code-context snapshot
code-context snapshot "authentication"
code-context snapshot "parser" --limit 5
```

Generates a project-wide or query-focused context package for LLM consumption, including graph summaries, relation highlights, reading paths, and recommended next files.

### `graph` — Export and explore the repository graph

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

Exports graph JSON, finds file-level paths, shows neighboring files/symbols, runs provider-backed
traversals when supported, and returns local subgraphs for focused analysis.
Provider-backed traversals accept structured starts (`--kind/--path/--name`) or target strings such
as `docs/health.md`, `symbol:HealthHandler@cmd/api/main.go`, `GET /health`, and `text:Health`.
Edge filters support concrete edge kinds plus semantic groups: `code`, `docs`, `symbols`, and
`entrypoints`. Use `--target-kind`, `--file-pattern`, `--metadata key=value`, and `--include-paths`
to narrow traversal output and include shortest paths from the start target.

Graph exports are versioned as `graph-export.v2` and now include richer code-knowledge graph structure:
- node types: `file`, `symbol`, `import`, `module`, `package`
- edge types: `defines`, `imports`, `belongs_to`, `declares_package`, `represents`, `resolves_to`
- code-intelligence edges: `calls`, `declares_route`, `handles_route`, `mentions_file`, `mentions_symbol`, `mentions_module`
- confidence labels such as `EXTRACTED`, `INFERRED`, and `AMBIGUOUS`

### `callers` / `callees` — Lightweight call graph lookup

```bash
code-context callers NewEngine
code-context callees ReviewContext
```

Shows heuristic call graph edges extracted during indexing. Edges include source/target file and line metadata plus confidence labels. The extractor ignores matches inside comments and string literals to reduce false-positive edges. For Go, imported package selector calls such as `fmt.Println` or aliased external calls are filtered out. For JavaScript/TypeScript, imported bindings and namespaces such as `useState()` or `axios.get()` are filtered out. This keeps local call graphs less polluted by standard-library or third-party symbols. `callers <name>` matches exact targets plus separator-qualified calls such as `pkg.Name` or `mod::Name` without matching unrelated suffixes like `MyName`.

### `routes [query]` — List framework routes

```bash
code-context routes
code-context routes users
```

Lists routes detected from common framework patterns such as Go `http.HandleFunc`, Gin/chi methods, Express, NestJS decorators, FastAPI/Flask decorators, Django paths, Spring mappings, Rust route attributes, and Axum `.route` calls. Route extraction also merges common framework prefixes such as NestJS `@Controller`, Spring class-level `@RequestMapping`, FastAPI `APIRouter(prefix=...)`, and Flask `Blueprint(url_prefix=...)`.

### `route-context <query>` — Route-level impact package

```bash
code-context route-context /users
code-context route-context GetUserHandler
```

Aggregates matching routes, resolved handlers, callers, callees, related docs, recommended tests, and a route risk score. This is the fastest entry point when reviewing or changing an HTTP API surface.

### `docs-for <query>` / `doc-drift` — Document reference diagnostics

```bash
code-context docs-for NewEngine
code-context doc-drift --json --fail-on-broken
code-context doc-coverage --json --min-route-coverage 80 --min-symbol-coverage 60
code-context ci --json
```

`docs-for` finds documentation links that mention a file, symbol, module, or route reference such as `GET /api/items` and includes Markdown section metadata (`section_title`, `section_slug`, `section_line`) so agents can jump to the relevant heading. `doc-drift` reports broken references where documentation points to missing files, symbols, modules, or routes. `doc-coverage` reports indexed routes and public symbols that are not referenced by documentation so API docs can be completed proactively. Both diagnostics support `--json` for CI and agent automation; `doc-drift --fail-on-broken` and coverage thresholds can make documentation health checks fail builds. `ci` runs doctor, doc drift, and doc coverage together using CLI flags or `.code-context.yaml` defaults.

### `trace <from> <to>` — Call chain tracing

```bash
code-context trace "main" "Engine"
```

Traces the path between two symbols through imports.

### `impact <file-or-symbol>` — Unified impact analysis

```bash
code-context impact Engine
code-context impact internal/store/sqlite.go --depth 2
code-context impact Engine --json
code-context impact-git --state all --json
```

Automatically detects whether the target is an indexed file or a symbol. File impact reports direct/all dependencies, dependent files, and recommended tests. Symbol impact combines definition lookup, callers, callees, file-level dependents, related routes, related docs, recommended tests, and a risk score. Use `--json` for agents and CI automation.

`impact-git` applies the same idea to local git changes, summarizing changed files, changed symbols, file impacts, symbol impacts, aggregate risk, related tests, and runnable test commands for `unstaged`, `staged`, or `all` state.

### `diff-impact <file>` — Change impact analysis

```bash
code-context diff-impact internal/store/sqlite.go
code-context diff-impact internal/store/sqlite.go --depth 2
```

Shows dependencies, dependent files, and recommended test files.

### `symbol-impact <name>` — Symbol-level impact package

```bash
code-context symbol-impact GitDiff
```

Combines definition lookup, callers, callees, file-level dependents, related routes, related docs, recommended tests, and a risk score for one symbol. Prefer `impact <target>` for new workflows because it also handles files and supports JSON output.

### `files` — List indexed files

```bash
code-context files
code-context files --lang go
```

### `imports <file>` — Show imports of a file

```bash
code-context imports internal/server/server.go
```

### `importers <source>` — Show files that import a given source

```bash
code-context importers "fmt"
```

### `stats` — Show index statistics

```bash
code-context stats
# Files:   42
# Symbols: 318
# Imports: 156
```

Also shows index version metadata and the last successful indexing timestamp when available.

### `status` — Show workflow and service status

```bash
code-context status
```

Shows root/database metadata, graph version, provider capabilities, index version, last indexed time,
and current watch refresh state.

### `provider-doctor` — Check provider configuration

```bash
code-context provider-doctor
code-context provider-doctor --json
```

Runs deterministic local checks for embedding and answer provider configuration plus configured
Answer profiles without making network calls or exposing secrets. It reports disabled providers,
missing hosted API keys, resolved models/base URLs, invalid profile templates/filters/ranges, and
suggested follow-up actions.

### `freshness` / `doctor` / `rebuild` — Index health and repair

```bash
code-context freshness
code-context freshness --json --limit 0
code-context doctor
code-context doctor --json
code-context rebuild
```

`freshness` reports indexed source/document files that are modified, deleted, or unreadable compared with the filesystem. `doctor` validates the project root, SQLite database, applied schema migration version, expected schema tables/indexes, index statistics, and freshness. Schema upgrades are applied through ordered migrations recorded in `schema_migrations`. `rebuild` clears the current index tables and runs a full reindex from disk.

### `serve` — Start HTTP server

```bash
code-context serve              # default port 9090
code-context serve --port 8080
code-context serve --watch      # enable background incremental refresh while serving
```

When `--watch` is enabled, the server continuously runs incremental refresh in the background and exposes workflow state through `/api/status`.

## Git-aware Commands

### `git-files` — List files tracked by git

```bash
code-context git-files
```

### `snapshot-git` — Generate LLM context from git changes

```bash
code-context snapshot-git
code-context snapshot-git --state all --limit 5
```

### `diff-impact-git` — Analyze impact using git-aware scope

```bash
code-context diff-impact-git
code-context diff-impact-git --state staged --depth 2
```

### `review-context` / `test-impact` — Git review workflows

```bash
code-context review-context --state all
code-context test-impact --state unstaged
```

`review-context` summarizes changed files, changed symbols, route/doc/test impact, risk, suggested review order, and runnable test commands. `test-impact` returns recommended tests for changed files and symbols plus commands such as `go test ./pkg`, `pytest file`, `npm test -- file`, `mvn test -Dtest=...`, or `cargo test`. Supported states are `unstaged`, `staged`, and `all`.

### Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--root` | `-r` | `.` | Codebase root directory |
| `--db` | | `<root>/.code-context/index.db` | Database path |
| `--store-backend` | | `sqlite` | Storage backend (`sqlite` or `helix`) |
| `--helix-url` | | | HelixDB endpoint URL |
| `--helix-api-key` | | | HelixDB API key |
| `--helix-api-key-env` | | | Environment variable containing the HelixDB API key |
| `--helix-project-id` | | `<absolute root>` | Helix project namespace |
| `--embedding-provider` | | `none` | Embedding provider (`none`, `openai`, `openai-compatible`) |
| `--embedding-base-url` | | | Embedding API base URL |
| `--embedding-api-key` | | | Embedding API key |
| `--embedding-api-key-env` | | | Environment variable containing the embedding API key |
| `--embedding-model` | | | Embedding model name |
| `--embedding-dimensions` | | `0` | Optional requested embedding dimensions |
| `--embedding-timeout` | | `30s` | Embedding request timeout when provider is enabled |
| `--embedding-batch-size` | | `64` | Maximum embedding batch size |
| `--answer-provider` | | `none` | Answer provider (`none`, `openai`, `openai-compatible`) |
| `--answer-base-url` | | | Answer API base URL |
| `--answer-api-key` | | | Answer API key |
| `--answer-api-key-env` | | | Environment variable containing the answer API key |
| `--answer-model` | | | Chat/answer model name |
| `--answer-timeout` | | `60s` | Answer request timeout when provider is enabled |
| `--answer-max-tokens` | | `1024` | Default answer max completion tokens |
| `--answer-temperature` | | `0.2` | Default answer sampling temperature |

### Helix Smoke Validation

Use a dedicated temporary Helix instance for first-time validation:

```bash
helix init local --no-skills
helix start dev --port 6970
HELIX_URL=http://localhost:6970 HELIX_PROJECT_ID=code-context-smoke scripts/helix-smoke.sh
```

The smoke creates a small Go fixture, starts deterministic local OpenAI-compatible fake embedding
and chat-completions servers, runs the Helix backend through `rebuild`, starts the HTTP server, and
verifies `stats`, `status` capabilities, embedding lifecycle status, namespace inventory, prune
dry-run/apply safety, `search`, `routes`, `docs-for`, `answer --context-only`, provider-backed
`answer`, `/api/text`, real `/api/vector` query-text results, `/api/hybrid` vector fusion,
`/api/answer`, and `/api/graph/traverse` read paths. It only rebuilds the configured
`HELIX_PROJECT_ID`; use a fresh instance if it was initialized by older code-context builds that
created unscoped unique path indexes.

### Advanced Capability Interfaces

Helix-specific features are kept behind provider-neutral optional interfaces in
`internal/store/capabilities.go`. Callers should depend on capabilities such as `TextSearcher`,
`VectorSearcher`, `HybridSearcher`, `GraphTraverser`, `WorkspaceSearcher`, `MemoryStore`,
`Embedder`, `EmbeddingCache`, and `Answerer`
instead of importing Helix SDK types. Backends can implement any subset of these interfaces; callers
can use `store.DetectCapabilities(provider)` or normal Go type assertions and keep SQLite/local
fallbacks where appropriate. The Helix backend currently implements `TextSearcher` with BM25 over
indexed symbol text and document metadata/summary text, `EmbeddingCache`/`VectorSearcher` over
namespaced embedding chunk nodes, and `GraphTraverser` over the indexed
file/symbol/import/call/route/document-link graph. Helix vector properties are namespaced by
embedding model + dimensions so different embedding spaces do not share the same vector index.
Graph traversal supports target-string parsing,
semantic edge groups, target/file/metadata filters, depth/path metadata, text-query expansion through
`similar` edges, and handler-to-route relationships. Higher-level outputs such as `explain`,
`context`, `impact`, `route-context`, `snapshot`, and their MCP equivalents include best-effort
provider graph traversal summaries when the backend supports `GraphTraverser`; SQLite/local
fallbacks simply omit those optional fields. `/api/text` and search callers use text search when
available and keep the local grep fallback for backends without the capability. `hybrid-search`,
`/api/hybrid`, and MCP `hybrid_search`/`code_context_hybrid_search` fuse text, vector, and graph
signals when available and degrade to the supported subset. Query-focused `context`, `snapshot`,
HTTP responses, and agent `code_context_explore`/`code_context_context`/`code_context_snapshot`
also surface best-effort hybrid retrieval hints as optional `hybrid_hits` so callers can use the
extra evidence without coupling to any specific backend. Engine fallback fusion uses a weighted
normalized sum and annotates each hit with source ranks/contributions (`hybrid_*` metadata), keeping
ranking tuning transparent while remaining backend-neutral. `vector-search`,
`/api/vector`, and MCP `vector_search`/`code_context_vector_search` call `VectorSearcher` directly;
when query text is provided, they first use the configured `Embedder` to produce the query vector.

Answer/RAG support is also provider-neutral. `answer`, `POST /api/answer`, and MCP
`answer`/`code_context_answer` first build context from `SearchHybrid`, then call the configured
`Answerer` only when `answer.provider` is enabled. Use `--context-only` or JSON
`{"context_only": true}` to inspect retrieved evidence without any external model call. Answer
results include provider-neutral citation/source metadata, and requests can override
`system_prompt` or pass prior `messages` for provider-specific conversation style while keeping
retrieval backend-neutral. They can also select reusable provider-neutral prompt presets with
`template: "general" | "explain" | "review" | "plan"`; `system_prompt` overrides the preset text
when both are present, and `answer-templates` / `/api/answer-templates` / MCP
`answer_templates` exposes the current catalog. Workflow profiles are discoverable via
`answer-profiles` / `/api/answer-profiles` / MCP `answer_profiles`, and can be selected with
`profile`. Built-in profiles can be extended or overridden from user/project config under
`answer.profiles`; project config wins over user config for the same normalized profile name.
Configured profiles can include retrieval, rerank, grounding, and evaluation defaults. Answer
profile definitions are also checked by `provider-doctor`, `/api/provider-diagnostics`, and MCP
provider diagnostics so bad templates, target kinds, or numeric ranges are caught before runtime.
Requests also accept `filter`, `text_weight`, `vector_weight`,
`graph_weight`, `expand_from`, and `expand_max_depth` so callers can scope and tune retrieval
without dropping to backend-specific APIs. They can then post-process the selected answer context
through the provider-neutral `AnswerReranker` hook with `min_context_score`, `dedupe_context`,
`max_per_file`, `max_context_item_chars`, and `max_context_chars`; results include a `retrieval`
report describing selected, dropped, and truncated context. MCP answer tools can return
`format: "markdown"` for a
ready-to-display answer with sources, or JSON for structured consumers. Provider-backed answers run
a deterministic citation-label audit that reports valid, missing, and uncited source labels under
`grounding`; `require_citations` or `min_citation_coverage` marks this audit as required for callers
that want a hard gate. Callers can also set `evaluate` / `--evaluate` to run the built-in local
`AnswerEvaluator` hook, which scores answer presence, evidence-overlap, and citation grounding under
`evaluation`; `min_evaluation_score` / `--min-evaluation-score` makes that score a caller-visible
quality gate. The hook is provider-neutral, so future semantic or LLM judge implementations can plug
in without changing Answer callers.
The built-in
`openai-compatible` answer adapter posts to
`{base_url}/chat/completions`; additional answer providers can implement `store.Answerer` without
changing retrieval or storage call sites.

Embedding support starts as provider-neutral plumbing rather than a full RAG framework dependency.
The built-in `openai-compatible` adapter posts batches to `{base_url}/embeddings`, preserves source
target metadata, records model/dimension metadata, and keeps the provider disabled by default. This
lets local runtimes such as Ollama/LocalAI/TEI or hosted OpenAI-compatible APIs supply vectors while
code-context owns only the chunking, caching, storage, and retrieval glue. When a backend implements
`EmbeddingCache` (SQLite and Helix do), indexing builds symbol/document chunks and stores generated
vectors by model + dimensions + chunk hash. Helix stores those vectors in `CodeContextEmbeddingChunk`
nodes and exposes them through `VectorSearcher`; the engine-level hybrid fusion layer combines
provider text, vector, and graph signals without requiring Helix-specific call sites.
Use `embedding-status`, `/api/embedding-status`, or MCP
`embedding_status`/`code_context_embedding_status` for a read-only lifecycle summary that combines
configuration, plan coverage, cached namespaces, prune candidates, and recommended next actions. Use
`embedding-namespaces`, `/api/embedding-namespaces`, or MCP
`embedding_namespaces`/`code_context_embedding_namespaces` to inventory cached model/dimension
namespaces before switching models or planning future cache cleanup. Use `embedding-prune`,
`POST /api/embedding-prune`, or MCP `embedding_prune`/`code_context_embedding_prune` to delete a
selected old namespace; prune is dry-run by default and refuses to remove the currently configured
namespace unless explicitly forced.

## HTTP API

Start the server with `code-context serve`, then:

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?` | Search symbols by name |
| GET | `/api/symbols` | `file` | List symbols in a file |
| GET | `/api/definitions` | `name` | Find symbol definitions |
| GET | `/api/references` | `name` | Find references to a symbol |
| GET | `/api/text` | `q`, `file?`, `limit?` | Full-text search in source |
| POST | `/api/vector` | JSON `VectorSearchQuery` with `query_text` or `vector` | Provider-backed vector search when supported |
| POST | `/api/hybrid` | JSON `HybridSearchQuery` with `query`, `vector?`, weights, and `expand_from?` | Provider-neutral text/vector/graph fusion |
| POST | `/api/answer` | JSON `AnswerOptions` with `question`/`query`, `context_only?`, `limit?`, `filter?`, weights, `expand_from?`, `profile?`, `template?`, `min_context_score?`, `dedupe_context?`, `max_per_file?`, `max_context_chars?`, `max_context_item_chars?`, `require_citations?`, `min_citation_coverage?`, `evaluate?`, `min_evaluation_score?`, `system_prompt?`, `messages?` | Build RAG context and optionally call the configured answer provider; JSON result includes `sources`, `retrieval`, provider-backed `grounding`, and optional local `evaluation` |
| GET | `/api/answer-templates` | `include_prompts?` | List built-in provider-neutral answer templates |
| GET | `/api/answer-profiles` | — | List built-in and configured provider-neutral answer workflow profiles |
| GET | `/api/provider-diagnostics` | — | Check provider and configured Answer profile settings without network calls |
| GET | `/api/imports` | `file` | Get imports of a file |
| GET | `/api/importers` | `source` | Find files importing a source |
| GET | `/api/callers` | `name` | Show heuristic callers of a symbol |
| GET | `/api/callees` | `name` | Show heuristic callees from a symbol |
| GET | `/api/routes` | `q?` | List indexed framework routes |
| GET | `/api/route-context` | `q` | Return route-level handlers, calls, docs, tests, and risk |
| GET | `/api/docs-for` | `q` | Find documentation references for a file, symbol, or module |
| GET | `/api/doc-drift` | — | Report broken documentation references |
| GET | `/api/map` | — | Project architecture overview with graph analysis |
| GET | `/api/graph` | `focus?` | Export repository or focused graph JSON (`graph-export.v2`) |
| GET | `/api/graph/path` | `from`, `to` | Find a file-level path through the graph |
| GET | `/api/graph/neighbors` | `target`, `limit?` | Show adjacent graph context for a file or symbol |
| GET | `/api/graph/subgraph` | `target`, `depth?` | Export a local graph around a file or symbol |
| POST | `/api/graph/traverse` | JSON `GraphTraversalQuery` | Provider-backed graph traversal when supported |
| GET | `/api/graph/html` | `focus?` | Render an interactive HTML graph view |
| GET | `/api/explain` | `file` | File summary with symbols, imports, and graph guidance |
| GET | `/api/context` | `name` | Symbol profile with related context and graph guidance |
| GET | `/api/snapshot` | `q?`, `limit?` | Generate project-wide or query-focused LLM context package with recommendations |
| GET | `/api/trace` | `from`, `to` | Trace call chain between symbols |
| GET | `/api/impact` | `target`, `depth?` | Unified file or symbol impact analysis with JSON output |
| GET | `/api/impact-git` | `state?`, `depth?` | Unified impact analysis for local git changes |
| GET | `/api/diff-impact` | `file`, `depth?` | Analyze change impact and related tests |
| GET | `/api/git/files` | `state?` | List git changed files for `unstaged`, `staged`, or `all` |
| GET | `/api/git/diff` | `state?`, `context?` | Return git diff hunks for changed files |
| GET | `/api/snapshot-git` | `state?`, `limit?` | Generate an LLM context package from git changes |
| GET | `/api/diff-impact-git` | `state?`, `depth?` | Analyze impact for git changed files |
| GET | `/api/review-context` | `state?` | Build review context with risk, routes, docs, and tests |
| GET | `/api/test-impact` | `state?` | Recommend tests for git changed files and symbols |
| GET | `/api/symbol-impact` | `name` | Return symbol-level callers, callees, docs, routes, tests, and risk |
| GET | `/api/stats` | — | Index statistics with version metadata |
| GET | `/api/status` | — | Service/workflow status including provider capabilities and watch metadata |
| GET | `/api/embedding-status` | `limit?` | Embedding lifecycle summary with recommendations |
| GET | `/api/embedding-lifecycle` | `limit?` | Alias for `/api/embedding-status` |
| GET | `/api/embedding-plan` | `limit?` | Embedding cache coverage and backfill plan for the configured model |
| POST | `/api/embedding-backfill` | `apply?`, `limit?` | Dry-run or apply missing/stale embedding backfill |
| GET | `/api/embedding-namespaces` | — | List cached embedding model/dimension namespaces |
| POST | `/api/embedding-prune` | `model`, `dimensions`, `apply?`, `force_current?` | Dry-run or delete a selected embedding namespace |
| GET | `/api/freshness` | `limit?` | Report indexed files/documents that differ from the filesystem |
| GET | `/api/doctor` | — | Check database schema, index freshness, and service health |
| POST | `/api/index` | `incremental?` | Trigger indexing |
| POST | `/api/rebuild` | — | Clear the current index and rebuild from disk |

Response format:
{
  "results": [...],
  "count": 5
}
```

## MCP Server

Expose code-context as a Model Context Protocol server for AI agents like Claude Desktop, Cursor, etc.

### Installation

Download from [Releases](https://github.com/sjzsdu/code-context/releases) or build from source:

```bash
go build -o code-context-mcp ./cmd/mcp
```

### Configuration

Add to your AI client config:

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

**Cursor** (`~/.cursor/mcp.json`):

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
| `index` | Index the codebase for search | - |
| `search` | Search symbols by name | `query` |
| `vector_search` | Provider-backed vector search | `query_text?`, `vector?`, `model?`, `dimensions?`, `filter?`, `limit?`, `offset?` |
| `hybrid_search` | Provider-neutral text/vector/graph fusion | `query?`, `vector?`, `model?`, `dimensions?`, `filter?`, weights, `expand_from?`, `limit?` |
| `embedding_status` | Embedding lifecycle summary and recommendations | `limit?` |
| `embedding_namespaces` | Cached embedding model/dimension namespace inventory | - |
| `embedding_prune` | Dry-run or delete a cached embedding namespace | `model`, `dimensions`, `apply?`, `force_current?` |
| `find_def` | Find where a symbol is defined | `name` |
| `find_refs` | Find all references to a symbol | `name` |
| `files` | List indexed files | `language?` |
| `imports` | Show imports of a file | `file` |
| `importers` | Find files importing a source | `source` |
| `callers` | Show heuristic callers of a symbol | `symbol` |
| `callees` | Show heuristic callees from a symbol | `symbol` |
| `routes` | List indexed framework routes | `query?` |
| `route_context` | Analyze route-level handlers, calls, docs, tests, and risk | `query` |
| `docs_for` | Find documentation references for a file, symbol, or module | `query` |
| `doc_drift` | Report broken documentation references | - |
| `stats` | Show index statistics | - |
| `code_context_embedding_status` | Embedding lifecycle summary and recommendations | `limit?` |
| `code_context_embedding_plan` | Embedding cache coverage and backfill plan | `limit?` |
| `code_context_embedding_backfill` | Dry-run or apply missing/stale embedding backfill | `apply?`, `limit?` |
| `code_context_embedding_namespaces` | Cached embedding model/dimension namespace inventory | - |
| `code_context_embedding_prune` | Dry-run or delete a cached embedding namespace | `model`, `dimensions`, `apply?`, `force_current?` |
| `code_context_freshness` | Report indexed files/documents that differ from the filesystem | `limit?` |
| `code_context_doctor` | Check database schema, index freshness, and service health | - |
| `map` | Show project architecture overview with graph analysis | - |
| `graph` | Export repository or focused graph JSON | `focus?` |
| `graph_path` | Find a file-level path through the graph | `from`, `to` |
| `graph_neighbors` | Show adjacent graph context for a file or symbol | `target`, `limit?` |
| `graph_subgraph` | Export a local graph around a file or symbol | `target`, `depth?` |
| `explain` | Show file summary with graph guidance | `file` |
| `context` | Show symbol profile with graph guidance | `symbol` |
| `snapshot` | Generate project-wide or query-focused LLM context | `query?`, `limit?` |
| `impact` | Unified file or symbol impact analysis as JSON | `target`, `depth?` |
| `impact_git` | Unified impact analysis for local git changes as JSON | `state?`, `depth?` |
| `diff_impact` | Analyze change impact for a file | `file`, `depth?` |
| `review_context` | Build git review context with risk, routes, docs, and tests | `state?` |
| `test_impact` | Recommend tests for git changed files and symbols | `state?` |
| `symbol_impact` | Return symbol-level impact, risk, docs, routes, and tests | `symbol` |
| `code_context_impact` | Agent-friendly unified file or symbol impact report with recommendations | `target`, `depth?` |
| `code_context_impact_git` | Agent-friendly unified impact report for local git changes | `state?`, `depth?` |
| `code_context_vector_search` | Agent-friendly provider-backed vector search | `query_text?`, `vector?`, `model?`, `dimensions?`, `filter?`, `limit?`, `offset?` |
| `code_context_hybrid_search` | Agent-friendly text/vector/graph fusion | `query?`, `vector?`, `model?`, `dimensions?`, `filter?`, weights, `expand_from?`, `limit?` |
| `trace` | Trace call chain between symbols | `from`, `to` |

### Usage Example

```bash
# First, index your project
code-context index

# Or via MCP tool
code-context:index

# Then search
code-context:search "Server"
code-context:embedding_status
code-context:embedding_namespaces
code-context:vector_search '{"query_text":"health handler","limit":5}'
code-context:hybrid_search '{"query":"health handler","limit":5}'

# Inspect graph navigation via MCP
code-context:graph_neighbors '{"target":"Engine","limit":5}'
code-context:graph_path '{"from":"Engine","to":"Server"}'
code-context:graph_traverse '{"target":"docs/health.md","edge_kinds":["references"],"include_paths":true,"limit":10}'
code-context:graph_traverse '{"target":"text:Health","edge_kinds":["similar"],"filter":{"target_kinds":["symbol"]},"include_paths":true,"limit":10}'
code-context:graph '{"focus":"internal/server/server.go"}'

# Unified impact analysis for an edit target
code-context:impact '{"target":"Engine","depth":2}'
code-context:impact_git '{"state":"all","depth":2}'

# Rich graph exports now use graph-export.v2 with module/package nodes and
# edges such as belongs_to, declares_package, represents, and resolves_to.

# Generate context for a feature
code-context:snapshot "authentication"
```

## Architecture

```
cmd/code-context/      CLI entry point (cobra)
internal/
├── api/               Core types: Symbol, FileInfo, ImportEdge, IndexStats
├── parser/            Tree-sitter parsing + language detection
├── lang/              Language definitions (queries per language)
├── store/             SQLite storage with FTS5 full-text index
├── indexer/           Parallel file walking + parsing + sequential writes
├── search/            Symbol search, text grep, definition lookup
├── graph/             Import dependency graph with BFS + related scoring
├── engine/            Orchestration: wires all subsystems together
└── server/            HTTP API (net/http)
```

## Use Cases

### For AI Agents / LLMs

```bash
# Generate context for a feature
code-context snapshot "user authentication"

# Understand project structure
code-context map

# Find implementation details
code-context context "AuthService"
```

### For Developers

```bash
# What files might break if I change this?
code-context diff-impact internal/store/sqlite.go

# How does this code flow work?
code-context trace "handleRequest" "database.Query"

# What's in this file?
code-context explain internal/api/types.go
```

## Storage

- **Engine**: `modernc.org/sqlite` (pure Go, no CGo for SQLite itself)
- **Default path**: `<project-root>/.code-context/index.db`
- **FTS5**: full-text index on symbol names and signatures
- **Cascade deletes**: removing a file automatically removes its symbols and imports
- **Content hashing**: SHA-256 for incremental change detection
- **Default indexing scope**: production/source files only; common test file patterns are excluded by default

## License

MIT
