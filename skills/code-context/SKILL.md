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

# 4. Get detailed context
code-context context Engine

# 5. Explore graph relationships
code-context graph neighbors Engine

# 6. Generate project-wide or focused LLM context
code-context snapshot
code-context snapshot "authentication"

# 7. Open the interactive visual graph
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
server:
  port: 9090
watch:
  enabled: false
  interval: 2s
  debounce: 250ms
```

`watch.*` settings apply both to the standalone `watch` command and to `serve --watch` background refresh.

SQLite is the default storage backend; `store.backend: helix` enables the HelixDB-backed store.
If no Helix URL is configured, the Helix Go SDK uses its local default endpoint (`http://localhost:6969`).
Helix data is scoped by `project_id`; when omitted, the CLI/MCP server use the absolute root path.
For Helix runtime validation, prefer a dedicated temporary instance and run:
`HELIX_URL=http://localhost:6970 HELIX_PROJECT_ID=code-context-smoke scripts/helix-smoke.sh`.

## Recommended Dogfood Workflow

Use this sequence in the current repository to demonstrate the main value chain:

```bash
code-context index
code-context map
code-context snapshot
code-context search Snapshot
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

Use `status` to inspect index version, last indexed timestamp, and current watch refresh state.

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
code-context graph html --focus internal/server/server.go > graph.html
```

Use graph commands to export graph JSON, inspect adjacency, find file-level paths, and focus on local subgraphs.

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
| GET | `/api/status` | — | Workflow/service status including watch metadata |
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
- `GET /api/stats` — index stats with version metadata
- `GET /api/status` — workflow/service status with watch metadata
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
