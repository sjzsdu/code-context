# code-context Cookbook

> Practical, provider-neutral recipes for SQLite, Helix, embeddings, Answer/RAG, and project-specific profiles.

This cookbook intentionally keeps Helix, embedding providers, and answer providers behind the existing
code-context configuration and capability interfaces. You can start with SQLite-only local analysis and
upgrade individual capabilities later without changing caller code.

## 1. Local-only SQLite setup

Use this when you want structural code search, graph navigation, docs drift checks, and snapshots
without any external service or model calls.

```yaml
root: .
db: .code-context/index.db
store:
  backend: sqlite
  sqlite:
    db: .code-context/index.db
embedding:
  provider: none
answer:
  provider: none
  reranker: local
  evaluator: local
```

Recommended commands:

```bash
code-context rebuild --verbose
code-context doctor
code-context map
code-context search "Handler"
code-context graph traverse "text:health" --target-kind symbol --include-paths
code-context snapshot "authentication"
```

This mode is deterministic and offline. `answer --context-only` still works as a provider-neutral
RAG-context builder, but it will not call an external answer provider.

## 2. Helix storage with local OpenAI-compatible models

Use this when you want Helix-backed text/vector/graph capabilities while keeping embedding and answer
models local, for example through an OpenAI-compatible local server.

```yaml
root: .
db: .code-context/index.db
store:
  backend: helix
  helix:
    url: http://localhost:6969
    project_id: my-repo
    timeout: 30s
    read_retry_attempts: 2
    read_retry_backoff: 50ms
    write_retry_attempts: 3
    write_retry_backoff: 50ms
embedding:
  provider: openai-compatible
  base_url: http://localhost:11434/v1
  model: nomic-embed-text
  dimensions: 768
  timeout: 30s
  batch_size: 64
answer:
  provider: openai-compatible
  base_url: http://localhost:11434/v1
  model: qwen2.5-coder
  # Use semantic when embedding.provider is configured; local is deterministic/offline.
  reranker: semantic
  # Use llm when answer.provider is configured and you want semantic judging.
  evaluator: llm
  timeout: 60s
  max_tokens: 1024
  temperature: 0.2
```

Validation flow:

```bash
code-context provider-doctor
code-context rebuild --verbose
code-context embedding-status
code-context embedding-plan --json
code-context embedding-backfill --apply --limit 100
code-context vector-search "request handler" --json
code-context hybrid-search "request handler" --json
code-context answer "How is request routing implemented?" --context-only --json
```

Operational notes:

- Keep `store.helix.project_id` stable per repository/environment. It scopes Helix data.
- Use a dedicated Helix instance or project id for smoke tests.
- `store.helix.timeout` protects CLI/MCP/API operations from hanging on a slow endpoint.
- `read_retry_attempts` / `read_retry_backoff` cover transient read/network failures.
- `write_retry_attempts` / `write_retry_backoff` cover write conflicts and transient write/network failures.

## 3. Hosted OpenAI-compatible providers

Use this when embeddings and/or answers are provided by a hosted OpenAI-compatible API. Store secrets
in environment variables rather than project config.

```yaml
store:
  backend: helix
  helix:
    url: https://helix.example.com
    api_key_env: HELIX_API_KEY
    project_id: my-repo-prod
    timeout: 30s
embedding:
  provider: openai-compatible
  base_url: https://embedding.example.com/v1
  api_key_env: EMBEDDING_API_KEY
  model: text-embedding-3-small
  dimensions: 1536
  timeout: 30s
  batch_size: 64
answer:
  provider: openai-compatible
  base_url: https://answer.example.com/v1
  api_key_env: ANSWER_API_KEY
  model: gpt-4.1-mini
  reranker: semantic
  evaluator: llm
  timeout: 60s
  max_tokens: 2048
  temperature: 0.2
```

Preflight checklist:

```bash
code-context config inspect --format yaml
code-context provider-doctor --json
code-context doctor --json
```

`provider-doctor` is local and deterministic. It validates provider/profile configuration without
calling hosted APIs or exposing secret values.

## 4. Project-specific Answer profiles

Profiles let a repository define reusable Answer/RAG behavior without binding callers to Helix or a
specific LLM provider.

If `answer.reranker: semantic` is configured, Answer first embeds the question and candidate context
with the configured `Embedder`, reorders hits by semantic similarity, and then applies local
dedupe/per-file/context-budget constraints. Use `answer.reranker: local` when no embedding provider
is configured.

If `answer.evaluator: llm` is configured, `answer --evaluate` asks the configured answer provider for
a JSON faithfulness/completeness/citation-quality judgment while retaining local deterministic
guardrails. Use `answer.evaluator: local` when no answer provider is configured or when evaluation
must stay fully offline.

```yaml
answer:
  reranker: semantic
  evaluator: llm
  profiles:
    - name: project-review
      description: Project review with concise evidence and local gates
      template: review
      limit: 10
      filter:
        target_kinds: [symbol, file, document]
        file_pattern: "internal/*"
      text_weight: 0.55
      vector_weight: 0.20
      graph_weight: 0.25
      expand_max_depth: 2
      min_context_score: 0.1
      dedupe_context: true
      max_per_file: 2
      max_context_item_chars: 1000
      max_context_chars: 6000
      require_citations: true
      min_citation_coverage: 0.2
      evaluate: true
      min_evaluation_score: 0.6
```

Usage:

```bash
code-context answer-profiles
code-context provider-doctor
code-context answer "Review this change" --profile project-review --context-only --json
code-context answer "Review this change" --profile project-review --format markdown
```

Merge behavior:

- User-level profiles live under `~/.code-context/config.yaml`.
- Project profiles live under `.code-context/config.yaml`.
- Profiles merge by normalized name: trim, lowercase, and `_` becomes `-`.
- Project config overrides user config for the same normalized profile name.
- To avoid accidentally clearing inherited user profiles, keep example `profiles:` blocks fully
  commented until you intend to define project profiles.

## 5. Embedding lifecycle and model migration

Embedding vectors are tied to model name, dimensions, target, and content hash. Changing the embedding
model or dimensions creates a new namespace; existing chunks need to be backfilled for the new namespace.

Recommended migration flow:

```bash
code-context embedding-status --json
code-context embedding-namespaces --json
code-context embedding-plan --json
code-context embedding-backfill --limit 100        # dry run
code-context embedding-backfill --apply --limit 100
code-context embedding-status --json
```

After the new namespace is healthy, old namespaces can be pruned:

```bash
code-context embedding-prune --model old-model --dimensions 768       # dry run
code-context embedding-prune --model old-model --dimensions 768 --apply
```

Do not prune the currently configured namespace unless you explicitly pass `--force-current`.

## 6. HTTP and MCP integration

The HTTP server and MCP server use the same provider-neutral configuration:

```bash
code-context serve --port 9090
code-context-mcp --root . --store-backend helix --helix-url http://localhost:6969
```

Useful HTTP endpoints:

```bash
curl -fsS http://127.0.0.1:9090/api/status
curl -fsS http://127.0.0.1:9090/api/provider-diagnostics
curl -fsS http://127.0.0.1:9090/api/embedding-status
curl -fsS http://127.0.0.1:9090/api/answer-profiles
```

For agents, prefer this workflow:

1. Call status/doctor/provider diagnostics first.
2. Use `answer --context-only` or MCP `code_context_answer` with `context_only=true` before enabling
   provider-backed answers.
3. Select profiles by name rather than duplicating retrieval/rerank/evaluation knobs in every call.

## 7. Smoke validation

For Helix end-to-end validation, use a dedicated temporary instance and project id:

```bash
helix init local --no-skills
helix start dev --port 6970
HELIX_URL=http://localhost:6970 \
HELIX_PROJECT_ID=code-context-smoke \
scripts/helix-smoke.sh
```

The smoke script starts deterministic local OpenAI-compatible fake embedding and answer providers,
then verifies Helix rebuild, status, embedding lifecycle, vector/hybrid search, Answer/RAG,
provider diagnostics, and graph traversal through CLI and HTTP.
