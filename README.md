# code-context

A code context system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. Pure Go, single binary.

## Features

- **Structural parsing** — tree-sitter AST, not regex
- **FTS5 symbol search** — fast full-text search on symbol names
- **Definition lookup** — find where symbols are defined
- **Import graph** — dependency analysis with BFS traversal, related-file scoring, path lookup, neighbors, and subgraph export
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

# Generate LLM context with graph recommendations
code-context snapshot "authentication"

# Analyze change impact
code-context diff-impact internal/store/sqlite.go

# Show index stats
code-context stats

# Start HTTP server
code-context serve --port 9090
```

## Configuration

`code-context` supports a project config file at `.code-context.yaml`.

Supported options:

| Key | Type | Description |
|---|---|---|
| `root` | string | Codebase root directory |
| `db` | string | SQLite database path |
| `server.port` | int | HTTP server port |

Example (`.code-context.yaml`):

```yaml
root: .
db: .code-context/index.db
server:
  port: 9090
```

## CLI Commands

### `index` — Index the codebase

```bash
code-context index                       # full index
code-context index --incremental         # only changed files
code-context index -v                    # verbose progress
```

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

### `snapshot <query>` — Generate LLM context

```bash
code-context snapshot "authentication"
code-context snapshot "parser" --limit 5
```

Generates a context package for LLM consumption, including graph summaries, relation highlights, reading paths, and recommended next files.

### `graph` — Export and explore the repository graph

```bash
code-context graph
code-context graph --focus Engine
code-context graph path Engine Server
code-context graph neighbors internal/engine/engine.go --limit 5
code-context graph subgraph Engine --depth 2
```

Exports graph JSON, finds file-level paths, shows neighboring files/symbols, and returns local subgraphs for focused analysis.

### `trace <from> <to>` — Call chain tracing

```bash
code-context trace "main" "Engine"
```

Traces the path between two symbols through imports.

### `diff-impact <file>` — Change impact analysis

```bash
code-context diff-impact internal/store/sqlite.go
code-context diff-impact internal/store/sqlite.go --depth 2
```

Shows dependencies and recommended test files.

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

### `serve` — Start HTTP server

```bash
code-context serve              # default port 9090
code-context serve --port 8080
```

## Git-aware Commands

### `git-files` — List files tracked by git

```bash
code-context git-files
```

### `snapshot-git <query>` — Generate LLM context from git-tracked files

```bash
code-context snapshot-git "authentication"
code-context snapshot-git "parser" --limit 5
```

### `diff-impact-git <file>` — Analyze impact using git-aware scope

```bash
code-context diff-impact-git internal/store/sqlite.go
code-context diff-impact-git internal/store/sqlite.go --depth 2
```

### Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--root` | `-r` | `.` | Codebase root directory |
| `--db` | | `<root>/.code-context/index.db` | Database path |

## HTTP API

Start the server with `code-context serve`, then:

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?` | Search symbols by name |
| GET | `/api/symbols` | `file` | List symbols in a file |
| GET | `/api/definitions` | `name` | Find symbol definitions |
| GET | `/api/references` | `name` | Find references to a symbol |
| GET | `/api/text` | `q`, `file?`, `limit?` | Full-text search in source |
| GET | `/api/imports` | `file` | Get imports of a file |
| GET | `/api/importers` | `source` | Find files importing a source |
| GET | `/api/map` | — | Project architecture overview with graph analysis |
| GET | `/api/graph` | `focus?` | Export repository or focused graph JSON |
| GET | `/api/graph/path` | `from`, `to` | Find a file-level path through the graph |
| GET | `/api/graph/neighbors` | `target`, `limit?` | Show adjacent graph context for a file or symbol |
| GET | `/api/graph/subgraph` | `target`, `depth?` | Export a local graph around a file or symbol |
| GET | `/api/explain` | `file` | File summary with symbols, imports, and graph guidance |
| GET | `/api/context` | `name` | Symbol profile with related context and graph guidance |
| GET | `/api/snapshot` | `q`, `limit?` | Generate LLM context package with recommendations |
| GET | `/api/trace` | `from`, `to` | Trace call chain between symbols |
| GET | `/api/diff-impact` | `file`, `depth?` | Analyze change impact and related tests |
| GET | `/api/stats` | — | Index statistics |
| POST | `/api/index` | `incremental?` | Trigger re-indexing |

Response format:

```json
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
| `find_def` | Find where a symbol is defined | `name` |
| `find_refs` | Find all references to a symbol | `name` |
| `files` | List indexed files | `language?` |
| `imports` | Show imports of a file | `file` |
| `importers` | Find files importing a source | `source` |
| `stats` | Show index statistics | - |
| `map` | Show project architecture overview with graph analysis | - |
| `graph` | Export repository or focused graph JSON | `focus?` |
| `graph_path` | Find a file-level path through the graph | `from`, `to` |
| `graph_neighbors` | Show adjacent graph context for a file or symbol | `target`, `limit?` |
| `graph_subgraph` | Export a local graph around a file or symbol | `target`, `depth?` |
| `explain` | Show file summary with graph guidance | `file` |
| `context` | Show symbol profile with graph guidance | `symbol` |
| `snapshot` | Generate LLM context for a query | `query`, `limit?` |
| `diff_impact` | Analyze change impact for a file | `file`, `depth?` |
| `trace` | Trace call chain between symbols | `from`, `to` |

### Usage Example

```bash
# First, index your project
code-context index

# Or via MCP tool
code-context:index

# Then search
code-context:search "Server"

# Inspect graph navigation via MCP
code-context:graph_neighbors '{"target":"Engine","limit":5}'
code-context:graph_path '{"from":"Engine","to":"Server"}'

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
