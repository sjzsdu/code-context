---
name: code-context
description: 'Code context system for AI agents and LLMs. Index codebases structurally with tree-sitter, provide efficient symbol search, dependency analysis, git-aware context, and hybrid semantic search. Use when analyzing codebase structure, generating LLM context, or analyzing code dependencies.'
license: MIT
allowed-tools: Bash, Grep, Glob, Read, Edit, LSP
---

# Code Context System

## Overview

A code context system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. Designed to help AI understand codebases quickly.

## Why Use This Skill

- **For Code Analysis**: Quickly understand unfamiliar codebases with `map`, `explain`, `context`
- **For Graph Navigation**: Explore repository structure with `graph`, `graph path`, `graph neighbors`, and `graph subgraph`
- **For Dependency Understanding**: Trace imports and find impact with `diff-impact`, `trace`
- **For Git-aware Context**: Analyze changes with `git-files`, `git-diff`, `snapshot-git`
- **For Semantic Search**: Use hybrid search combining keyword and semantic similarity
- **For LLM Context**: Generate focused context packages with `snapshot`

## Supported Languages

| Language | Extensions |
|---|---|
| Go | `.go` |
| TypeScript | `.ts`, `.tsx` |
| JavaScript | `.js`, `.jsx`, `.mjs` |
| Python | `.py` |
| Rust | `.rs` |
| Java | `.java` |

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

# 6. Generate LLM context
code-context snapshot "authentication"
```

## Configuration

Create `.code-context.yaml` in project root:

```yaml
root: .
db: .code-context/index.db
server:
  port: 9090
watch:
  enabled: false
  interval: 2s
  debounce: 250ms
```

## Core Commands

### Indexing

```bash
code-context index                       # full index
code-context index --incremental         # only changed files
code-context index -v                    # verbose progress
```

By default, test files are excluded from indexing so graph analysis and recommendations stay focused on production code.

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

Includes repository-level graph analysis and recommended files.

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

### Generate LLM Context (Snapshot)

```bash
code-context snapshot "parser"           # query-based context
code-context snapshot "parser" --limit 3 # limit files
```

Generates a context package for LLM consumption with:
- Related files and their symbols
- Summary of what was found
- Graph summaries and recommended next files

### Graph Exploration

```bash
code-context graph
code-context graph --focus Engine
code-context graph path Engine Server
code-context graph neighbors internal/engine/engine.go --limit 5
code-context graph subgraph Engine --depth 2
```

Use graph commands to export graph JSON, inspect adjacency, find file-level paths, and focus on local subgraphs.

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

### Snapshot from Git Changes

```bash
code-context snapshot-git                # context for unstaged
code-context snapshot-git --state all   # context for all changes
code-context snapshot-git --limit 10    # limit files
```

### Diff Impact from Git Changes

```bash
code-context diff-impact-git             # impact for unstaged
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

### 6. Analyzing Git Changes

```bash
code-context git-files --state all
code-context git-diff --context 3
code-context snapshot-git --state unstaged
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
| GET | `/api/stats` | — | Index stats |
| POST | `/api/index` | `incremental?` | Re-index |

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
| `explain` | File summary with graph guidance | `file` |
| `context` | Symbol profile with graph guidance | `symbol` |
| `snapshot` | Generate LLM context with recommendations | `query`, `limit?` |
| `snapshot_git` | Context from git | `state?`, `limit?` |
| `diff_impact` | Change impact analysis | `file`, `depth?` |
| `diff_impact_git` | Impact from git | `state?`, `depth?` |
| `trace` | Call chain tracing | `from`, `to` |

Note: the MCP server currently exposes the core analysis tools above; `graph`, `graph path`, `graph neighbors`, and `graph subgraph` remain available through the CLI and HTTP API.

## Tips

- Run `code-context index` first before any search commands
- Use `snapshot` for generating LLM context - it's the most useful for AI
- Use `map` to understand project structure quickly
- Use `graph neighbors` or `graph subgraph` when a symbol/file is too isolated in plain search results
- Use `--hybrid` flag with search for semantic matching
- Use git-aware commands (`git-files`, `git-diff`, `snapshot-git`) to analyze changes
- `diff-impact` is great for understanding what might break when changing a file
- Test files are excluded from indexing by default, so outputs stay focused on production code
- Create a `.code-context.yaml` config file for persistent settings
