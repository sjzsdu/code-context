---
name: code-context
description: 'Code context system for AI agents and LLMs. Use when user wants to analyze codebase structure, generate LLM context, or analyze code dependencies. Commands: index, search, find-def, map, explain, context, snapshot, trace, diff-impact'
license: MIT
allowed-tools: Bash, Grep, Glob, Read, Edit, LSP
---

# Code Context System

## Overview

A code context system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. Designed to help AI understand codebases quickly.

## Supported Languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| TypeScript | `.ts`, `.tsx` |
| JavaScript | `.js`, `.jsx`, `.mjs` |
| Python | `.py` |
| Rust | `.rs` |
| Java | `.java` |

## Core Commands

### Index the Codebase

```bash
code-context index                       # full index
code-context index --incremental         # only changed files
code-context index -v                    # verbose progress
```

### Search Symbols

```bash
code-context search "Handler"           # search by name
code-context search "parse" --kind function --limit 20
```

### Find Definition

```bash
code-context find-def "Engine"          # find symbol definition
```

### Project Architecture Map

```bash
code-context map                         # show directory structure with stats
```

Output:
```
[root]
  files: 24, symbols: 302 (func: 188, type: 24, method: 66)
  cmd/code-context/
  internal/store/
  internal/engine/
  ...
```

### Explain a File

```bash
code-context explain internal/engine/engine.go
```

Shows:
- File path and language
- All symbols in the file (functions, types, methods)
- Imports (what this file imports)
- Importers (what files import this file)

### Symbol Context

```bash
code-context context Engine
```

Shows:
- Definition location and signature
- Methods (if it's a type)
- Related symbols across the codebase

### Generate LLM Context (Snapshot)

```bash
code-context snapshot "parser"           # query-based context
code-context snapshot "parser" --limit 3 # limit files
```

Generates a context package for LLM consumption with:
- Related files and their symbols
- Summary of what was found

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
code-context snapshot "authentication"
code-context explain internal/auth/auth.go
```

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

## HTTP API

Start server: `code-context serve --port 9090`

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?` | Search symbols |
| GET | `/api/symbols` | `file` | List file symbols |
| GET | `/api/definitions` | `name` | Find definitions |
| GET | `/api/references` | `name` | Find references |
| GET | `/api/text` | `q`, `file?`, `limit?` | Text search |
| GET | `/api/imports` | `file` | Get imports |
| GET | `/api/importers` | `source` | Get importers |
| GET | `/api/stats` | — | Index stats |
| POST | `/api/index` | `incremental?` | Re-index |

## Additional Commands

### List Indexed Files

```bash
code-context files                      # list all files
code-context files --lang go           # filter by language
```

### Show Imports/Importers

```bash
code-context imports internal/store/sqlite.go  # what this file imports
code-context importers "fmt"                   # who imports "fmt"
```

### Statistics

```bash
code-context stats                      # show index statistics
```

### Start HTTP Server

```bash
code-context serve                      # default port 9090
code-context serve --port 8080
```

## MCP Server

Use MCP server to expose code-context capabilities to AI agents (Claude Desktop, Cursor, etc.).

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
      "args": ["--root", "/path/to/your/project"]
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
| `imports` | Show file imports | `file` |
| `importers` | Find importing files | `source` |
| `stats` | Index statistics | - |
| `map` | Project architecture | - |
| `explain` | File summary | `file` |
| `context` | Symbol profile | `symbol` |
| `snapshot` | Generate LLM context | `query`, `limit?` |
| `diff_impact` | Change impact analysis | `file`, `depth?` |
| `trace` | Call chain tracing | `from`, `to` |

## Tips

- Run `code-context index` first before any search commands
- Use `snapshot` for generating LLM context - it's the most useful for AI
- Use `map` to understand project structure quickly
- `diff-impact` is great for understanding what might break when changing a file
