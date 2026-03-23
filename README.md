# code-memory

A code memory system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval — symbol search, definition lookup, reference finding, and import graph traversal. Pure Go, single binary.

## Features

- **Structural parsing** — tree-sitter AST, not regex
- **FTS5 symbol search** — fast full-text search on symbol names
- **Definition & reference lookup** — find where symbols are defined and used
- **Import graph** — dependency analysis with BFS traversal and related-file scoring
- **Text search** — line-level grep across indexed files
- **HTTP API** — 9 endpoints for programmatic access
- **Incremental indexing** — only reindex changed files (content-hash based)
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
go install github.com/sjzsdu/code-memory/cmd/code-memory@latest
```

Or build from source:

```bash
git clone https://github.com/sjzsdu/code-memory.git
cd code-memory
go build -o code-memory ./cmd/code-memory
```

## Quick Start

```bash
# Index your project
code-memory index

# Search for symbols
code-memory search "Server"

# Find where a function is defined
code-memory find-def "NewRouter"

# Show index stats
code-memory stats

# Start HTTP server
code-memory serve --port 9090
```

## CLI Commands

### `index` — Index the codebase

```bash
code-memory index                       # full index
code-memory index --incremental         # only changed files
code-memory index --incremental -v      # with per-file progress
```

### `search <query>` — Search symbols by name

```bash
code-memory search "Handler"
code-memory search "parse" --kind function --limit 20
```

### `find-def <name>` — Find definition of a symbol

```bash
code-memory find-def "NewServer"
```

### `find-ref <name>` — Find references to a symbol

```bash
code-memory find-ref "Config"
```

### `files` — List indexed files

```bash
code-memory files
code-memory files --lang go
```

### `imports <file>` — Show imports of a file

```bash
code-memory imports internal/server/server.go
```

### `importers <source>` — Show files that import a given source

```bash
code-memory importers "fmt"
```

### `stats` — Show index statistics

```bash
code-memory stats
# Files:   42
# Symbols: 318
# Imports: 156
```

### `serve` — Start HTTP server

```bash
code-memory serve              # default port 9090
code-memory serve --port 8080
```

### Global Flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--root` | `-r` | `.` | Codebase root directory |
| `--db` | | `<root>/.code-memory/index.db` | Database path |

## HTTP API

Start the server with `code-memory serve`, then:

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?` | Search symbols by name |
| GET | `/api/symbols` | `file` | List symbols in a file |
| GET | `/api/definitions` | `name` | Find symbol definitions |
| GET | `/api/references` | `name` | Find symbol references |
| GET | `/api/text` | `q`, `file?`, `limit?` | Full-text search in source |
| GET | `/api/imports` | `file` | Get imports of a file |
| GET | `/api/importers` | `source` | Find files importing a source |
| GET | `/api/stats` | — | Index statistics |
| POST | `/api/index` | `incremental?` | Trigger re-indexing |

Response format:

```json
{
  "results": [...],
  "count": 5
}
```

## Architecture

```
cmd/code-memory/       CLI entry point (cobra)
internal/
├── api/               Core types: Symbol, FileInfo, ImportEdge, IndexStats
├── parser/            Tree-sitter parsing + language detection
├── lang/              Language definitions (queries per language)
├── store/             SQLite storage with FTS5 full-text index
├── indexer/           Parallel file walking + parsing + sequential writes
├── search/            Symbol search, text grep, definition/reference lookup
├── graph/             Import dependency graph with BFS + related scoring
├── engine/            Orchestration: wires all subsystems together
└── server/            HTTP API (net/http)
```

## How It Works

1. **Walk** — recursively scan project, skip `node_modules`/`vendor`/dot-dirs
2. **Detect** — map file extension to language
3. **Parse** — tree-sitter AST queries extract symbols (functions, types, classes) and imports
4. **Store** — upsert into SQLite with FTS5 triggers for symbol name indexing
5. **Serve** — CLI commands or HTTP API query the store and import graph

Indexing is parallelized: files are parsed concurrently (up to 16 workers), results are written sequentially to SQLite.

## Storage

- **Engine**: `modernc.org/sqlite` (pure Go, no CGo for SQLite itself)
- **Default path**: `<project-root>/.code-memory/index.db`
- **FTS5**: full-text index on symbol names and signatures
- **Cascade deletes**: removing a file automatically removes its symbols and imports
- **Content hashing**: SHA-256 for incremental change detection

## License

MIT
