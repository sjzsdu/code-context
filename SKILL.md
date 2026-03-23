---
name: code-memory
description: 'Code memory system for intelligent codebase indexing and search. Use when user wants to analyze codebase structure, find symbol definitions, generate LLM context, or analyze code dependencies. Commands: index, search, find-def, map, explain, context, snapshot, trace, diff-impact'
license: MIT
allowed-tools: Bash, Grep, Glob, Read, Edit, LSP
---

# Code Memory System

## Overview

A code memory system that reads entire codebases, indexes them structurally using tree-sitter, and provides efficient retrieval for AI agents and LLMs. It's designed to help AI understand codebases quickly.

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
code-memory index                       # full index
code-memory index --incremental         # only changed files
code-memory index -v                    # verbose progress
```

### Search Symbols

```bash
code-memory search "Handler"           # search by name
code-memory search "parse" --kind function --limit 20
```

### Find Definition

```bash
code-memory find-def "Engine"          # find symbol definition
```

### Project Architecture Map

```bash
code-memory map                         # show directory structure with stats
```

Output:
```
[root]
  files: 24, symbols: 302 (func: 188, type: 24, method: 66)
  cmd/code-memory/
  internal/store/
  internal/engine/
  ...
```

### Explain a File

```bash
code-memory explain internal/engine/engine.go
```

Shows:
- File path and language
- All symbols in the file (functions, types, methods)
- Imports (what this file imports)
- Importers (what files import this file)

### Symbol Context

```bash
code-memory context Engine
```

Shows:
- Definition location and signature
- Methods (if it's a type)
- Related symbols across the codebase

### Generate LLM Context (Snapshot)

```bash
code-memory snapshot "parser"           # query-based context
code-memory snapshot "parser" --limit 3 # limit files
```

Generates a context package for LLM consumption with:
- Related files and their symbols
- Summary of what was found

### Trace Call Chain

```bash
code-memory trace New SearchSymbols     # trace between two symbols
```

Shows the path from one symbol to another through the import graph.

### Diff Impact Analysis

```bash
code-memory diff-impact internal/store/sqlite.go
code-memory diff-impact internal/store/sqlite.go --depth 2
```

Shows:
- Direct dependencies
- All dependencies (transitive)
- Dependent files (that import this)
- Recommended test files to run

## Use Cases

### 1. Understanding a New Codebase

```bash
# Get overview
code-memory map

# Find key types
code-memory search "Engine"
code-memory context Engine

# See how it's used
code-memory explain internal/engine/engine.go
```

### 2. Finding Implementation Details

```bash
# Find where something is defined
code-memory find-def "NewRouter"

# Get full context
code-memory context NewRouter
```

### 3. Generating LLM Context

```bash
# For a feature query
code-memory snapshot "authentication"

# For a specific file
code-memory explain internal/auth/auth.go
```

### 4. Understanding Dependencies

```bash
# What does this file import?
code-memory imports internal/store/sqlite.go

# What imports this file?
code-memory importers "internal/api"

# Full impact analysis
code-memory diff-impact internal/store/sqlite.go
```

### 5. Tracing Code Flow

```bash
code-memory trace "main" "Engine"
```

## HTTP API

Start server: `code-memory serve --port 9090`

| Method | Endpoint | Parameters | Description |
|---|---|---|---|
| GET | `/api/search` | `q`, `kind?`, `limit?` | Search symbols |
| GET | `/api/symbols` | `file` | List file symbols |
| GET | `/api/definitions` | `name` | Find definitions |
| GET | `/api/text` | `q`, `file?`, `limit?` | Text search |
| GET | `/api/imports` | `file` | Get imports |
| GET | `/api/importers` | `source` | Get importers |
| GET | `/api/stats` | — | Index stats |
| POST | `/api/index` | `incremental?` | Re-index |

## Tips

- Run `code-memory index` first before any search commands
- Use `snapshot` for generating LLM context - it's the most useful for AI
- Use `map` to understand project structure quickly
- `diff-impact` is great for understanding what might break when changing a file
