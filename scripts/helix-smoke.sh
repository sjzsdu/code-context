#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELIX_URL="${HELIX_URL:-http://localhost:6969}"
HELIX_PROJECT_ID="${HELIX_PROJECT_ID:-code-context-helix-smoke}"
SMOKE_DIR="${CODE_CONTEXT_HELIX_SMOKE_DIR:-}"
HTTP_PORT="${CODE_CONTEXT_HELIX_SMOKE_PORT:-19090}"
EMBEDDING_PORT="${CODE_CONTEXT_HELIX_EMBEDDING_PORT:-19091}"
ANSWER_PORT="${CODE_CONTEXT_HELIX_ANSWER_PORT:-19092}"
KEEP_SMOKE_DIR="${KEEP_SMOKE_DIR:-0}"
SERVER_PID=""
EMBEDDING_PID=""
ANSWER_PID=""
CLEAN_SMOKE_DIR=0

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ -n "$EMBEDDING_PID" ]] && kill -0 "$EMBEDDING_PID" 2>/dev/null; then
    kill "$EMBEDDING_PID" 2>/dev/null || true
    wait "$EMBEDDING_PID" 2>/dev/null || true
  fi
  if [[ -n "$ANSWER_PID" ]] && kill -0 "$ANSWER_PID" 2>/dev/null; then
    kill "$ANSWER_PID" 2>/dev/null || true
    wait "$ANSWER_PID" 2>/dev/null || true
  fi
  if [[ "$CLEAN_SMOKE_DIR" == "1" ]]; then
    rm -rf "$SMOKE_DIR"
  fi
}

if [[ -z "$SMOKE_DIR" ]]; then
  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/code-context-helix-smoke.XXXXXX")"
  if [[ "$KEEP_SMOKE_DIR" != "1" ]]; then
    CLEAN_SMOKE_DIR=1
  fi
else
  rm -rf "$SMOKE_DIR"
  mkdir -p "$SMOKE_DIR"
fi
trap cleanup EXIT

FIXTURE="$SMOKE_DIR/fixture"
mkdir -p "$FIXTURE/cmd/api" "$FIXTURE/internal/service" "$FIXTURE/docs"

cat > "$FIXTURE/go.mod" <<'EOF'
module smoke

go 1.22
EOF

cat > "$FIXTURE/cmd/api/main.go" <<'EOF'
package main

import (
	"fmt"
	"net/http"

	"smoke/internal/service"
)

func main() {
	http.HandleFunc("/health", HealthHandler)
	http.ListenAndServe(":8080", nil)
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, service.HealthMessage())
}
EOF

cat > "$FIXTURE/internal/service/health.go" <<'EOF'
package service

func HealthMessage() string {
	return "ok"
}
EOF

cat > "$FIXTURE/docs/health.md" <<'EOF'
# Health API

The health endpoint is implemented by HealthHandler and returns HealthMessage.
EOF

mkdir -p "$FIXTURE/.code-context"
cat > "$FIXTURE/.code-context/config.yaml" <<'EOF'
answer:
  profiles:
    - name: smoke-custom
      description: Smoke custom answer profile from project config
      template: review
      limit: 5
      filter:
        target_kinds: [symbol]
      text_weight: 0.6
      vector_weight: 0.4
      dedupe_context: true
      max_per_file: 2
      max_context_item_chars: 800
      max_context_chars: 2000
      require_citations: true
      min_citation_coverage: 0.1
      evaluate: true
      min_evaluation_score: 0.2
EOF

cd "$ROOT_DIR"

cat > "$SMOKE_DIR/fake-embedding.py" <<'PY'
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


def base_vector(text):
    t = (text or "").lower()
    if "healthmessage" in t:
        return [1.0, 0.0, 0.0]
    if "healthhandler" in t:
        return [0.85, 0.15, 0.0]
    if "health" in t:
        return [0.75, 0.25, 0.0]
    return [0.0, 0.0, 1.0]


def with_dimensions(values, dimensions):
    dimensions = dimensions or 3
    values = list(values[:dimensions])
    while len(values) < dimensions:
        values.append(0.0)
    return values


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        if not self.path.endswith("/embeddings"):
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", "0") or "0")
        body = json.loads(self.rfile.read(length) or b"{}")
        inputs = body.get("input", [])
        if isinstance(inputs, str):
            inputs = [inputs]
        dimensions = int(body.get("dimensions") or 3)
        model = body.get("model") or "smoke-embedding"
        data = [
            {"index": i, "embedding": with_dimensions(base_vector(text), dimensions)}
            for i, text in enumerate(inputs)
        ]
        payload = {
            "model": model,
            "data": data,
            "usage": {"prompt_tokens": len(inputs), "total_tokens": len(inputs)},
        }
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *args):
        return


HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY

cat > "$SMOKE_DIR/fake-answer.py" <<'PY'
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", "0") or "0")
        body = json.loads(self.rfile.read(length) or b"{}")
        model = body.get("model") or "smoke-answer"
        messages = body.get("messages") or []
        prompt = "\n".join(str(m.get("content", "")) for m in messages)
        if "HealthMessage" in prompt:
            answer = "Smoke answer: HealthMessage returns the health response and is reached from HealthHandler [1]."
        elif "HealthHandler" in prompt:
            answer = "Smoke answer: HealthHandler handles /health and delegates to HealthMessage [1]."
        else:
            answer = "Smoke answer: insufficient fixture evidence."
        payload = {
            "model": model,
            "choices": [{"message": {"role": "assistant", "content": answer}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": len(prompt.split()), "completion_tokens": len(answer.split()), "total_tokens": len(prompt.split()) + len(answer.split())},
        }
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *args):
        return


HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY

run_code_context_model() {
  local model="$1"
  local dimensions="$2"
  shift 2
  go run ./cmd/code-context \
    --root "$FIXTURE" \
    --store-backend helix \
    --helix-url "$HELIX_URL" \
    --helix-project-id "$HELIX_PROJECT_ID" \
    --helix-timeout 20s \
    --helix-write-retry-attempts 4 \
    --helix-write-retry-backoff 25ms \
    --embedding-provider openai-compatible \
    --embedding-base-url "http://127.0.0.1:${EMBEDDING_PORT}/v1" \
    --embedding-model "$model" \
    --embedding-dimensions "$dimensions" \
    --answer-provider openai-compatible \
    --answer-base-url "http://127.0.0.1:${ANSWER_PORT}/v1" \
    --answer-model smoke-answer \
    "$@"
}

run_code_context() {
  run_code_context_model smoke-embedding 3 "$@"
}

run_code_context_old_embedding() {
  run_code_context_model smoke-embedding-old 3 "$@"
}

wait_for_embedding() {
  local url="http://127.0.0.1:${EMBEDDING_PORT}/health"
  for _ in {1..60}; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    if [[ -n "$EMBEDDING_PID" ]] && ! kill -0 "$EMBEDDING_PID" 2>/dev/null; then
      echo "fake embedding server exited before becoming ready" >&2
      cat "$SMOKE_DIR/fake-embedding.log" >&2 || true
      return 1
    fi
    sleep 0.25
  done
  echo "timed out waiting for fake embedding server on port ${EMBEDDING_PORT}" >&2
  cat "$SMOKE_DIR/fake-embedding.log" >&2 || true
  return 1
}

wait_for_answer() {
  local url="http://127.0.0.1:${ANSWER_PORT}/health"
  for _ in {1..60}; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    if [[ -n "$ANSWER_PID" ]] && ! kill -0 "$ANSWER_PID" 2>/dev/null; then
      echo "fake answer server exited before becoming ready" >&2
      cat "$SMOKE_DIR/fake-answer.log" >&2 || true
      return 1
    fi
    sleep 0.25
  done
  echo "timed out waiting for fake answer server on port ${ANSWER_PORT}" >&2
  cat "$SMOKE_DIR/fake-answer.log" >&2 || true
  return 1
}

wait_for_http() {
  local url="http://127.0.0.1:${HTTP_PORT}/api/status"
  for _ in {1..60}; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    if [[ -n "$SERVER_PID" ]] && ! kill -0 "$SERVER_PID" 2>/dev/null; then
      echo "code-context server exited before becoming ready" >&2
      cat "$SERVER_LOG" >&2 || true
      return 1
    fi
    sleep 0.25
  done
  echo "timed out waiting for code-context server on port ${HTTP_PORT}" >&2
  cat "$SERVER_LOG" >&2 || true
  return 1
}

echo "Helix smoke fixture: $FIXTURE"
echo "Helix URL: $HELIX_URL"
echo "Helix project id: $HELIX_PROJECT_ID"
echo "Fake embedding URL: http://127.0.0.1:${EMBEDDING_PORT}/v1"
echo "Fake answer URL: http://127.0.0.1:${ANSWER_PORT}/v1"
echo "WARNING: use a fresh/dedicated Helix instance if it was initialized by older code-context versions with unscoped unique path indexes."

python3 "$SMOKE_DIR/fake-embedding.py" "$EMBEDDING_PORT" >"$SMOKE_DIR/fake-embedding.log" 2>&1 &
EMBEDDING_PID=$!
wait_for_embedding
python3 "$SMOKE_DIR/fake-answer.py" "$ANSWER_PORT" >"$SMOKE_DIR/fake-answer.log" 2>&1 &
ANSWER_PID=$!
wait_for_answer

run_code_context rebuild --verbose

stats="$(run_code_context stats)"
printf '%s\n' "$stats"
grep -q "Files:[[:space:]]*2" <<<"$stats"
grep -q "Documents:[[:space:]]*1" <<<"$stats"

status="$(run_code_context status)"
printf '%s\n' "$status"
grep -q "Capabilities:.*text_search" <<<"$status"
grep -q "Capabilities:.*graph_traversal" <<<"$status"
grep -q "Capabilities:.*hybrid_search" <<<"$status"
grep -q "Capabilities:.*vector_search" <<<"$status"
grep -q "Capabilities:.*embedding_cache" <<<"$status"
grep -q "Capabilities:.*answer" <<<"$status"
grep -q "Embedding:.*openai-compatible.*smoke-embedding" <<<"$status"
grep -q "Answer:.*openai-compatible.*smoke-answer" <<<"$status"

embedding_status="$(run_code_context embedding-status --json --limit 10)"
printf '%s\n' "$embedding_status"
grep -q '"enabled": true' <<<"$embedding_status"
grep -q '"model": "smoke-embedding"' <<<"$embedding_status"
grep -q '"type": "healthy"' <<<"$embedding_status"
grep -q '"current_namespace"' <<<"$embedding_status"

namespaces="$(run_code_context embedding-namespaces --json)"
printf '%s\n' "$namespaces"
grep -q '"model": "smoke-embedding"' <<<"$namespaces"
grep -q '"total_namespaces": 1' <<<"$namespaces"

old_backfill="$(run_code_context_old_embedding embedding-backfill --apply --json --limit 10)"
printf '%s\n' "$old_backfill"
grep -q '"model": "smoke-embedding-old"' <<<"$old_backfill"
grep -q '"dry_run": false' <<<"$old_backfill"

embedding_status_with_old="$(run_code_context embedding-status --json --limit 10)"
printf '%s\n' "$embedding_status_with_old"
grep -q '"model": "smoke-embedding-old"' <<<"$embedding_status_with_old"
grep -q '"type": "prune"' <<<"$embedding_status_with_old"
grep -q "code-context embedding-prune --model smoke-embedding-old --dimensions 3" <<<"$embedding_status_with_old"

old_prune_dry_run="$(run_code_context embedding-prune --model smoke-embedding-old --dimensions 3 --json)"
printf '%s\n' "$old_prune_dry_run"
grep -q '"dry_run": true' <<<"$old_prune_dry_run"
grep -q '"model": "smoke-embedding-old"' <<<"$old_prune_dry_run"
grep -q '"matched_chunks":' <<<"$old_prune_dry_run"

set +e
current_prune_apply="$(run_code_context embedding-prune --model smoke-embedding --dimensions 3 --apply --json 2>&1)"
current_prune_status=$?
set -e
printf '%s\n' "$current_prune_apply"
[[ "$current_prune_status" != "0" ]]
grep -q "refusing to prune current embedding namespace" <<<"$current_prune_apply"

old_prune_apply="$(run_code_context embedding-prune --model smoke-embedding-old --dimensions 3 --apply --json)"
printf '%s\n' "$old_prune_apply"
grep -q '"dry_run": false' <<<"$old_prune_apply"
grep -q '"deleted_chunks":' <<<"$old_prune_apply"

embedding_status_after_prune="$(run_code_context embedding-status --json --limit 10)"
printf '%s\n' "$embedding_status_after_prune"
grep -q '"type": "healthy"' <<<"$embedding_status_after_prune"
! grep -q '"model": "smoke-embedding-old"' <<<"$embedding_status_after_prune"

search="$(run_code_context search Health)"
printf '%s\n' "$search"
grep -q "HealthHandler" <<<"$search"
grep -q "HealthMessage" <<<"$search"

vector_cli="$(run_code_context vector-search HealthMessage --json --limit 5)"
printf '%s\n' "$vector_cli"
grep -q '"source": "vector"' <<<"$vector_cli"
grep -q "HealthMessage" <<<"$vector_cli"

hybrid_cli="$(run_code_context hybrid-search HealthMessage --json --limit 5)"
printf '%s\n' "$hybrid_cli"
grep -q '"source": "hybrid"' <<<"$hybrid_cli"
grep -q '"hybrid_vector_score"' <<<"$hybrid_cli"
grep -q "HealthMessage" <<<"$hybrid_cli"

answer_templates_cli="$(run_code_context answer-templates --json)"
printf '%s\n' "$answer_templates_cli"
grep -q '"name": "general"' <<<"$answer_templates_cli"
grep -q '"name": "plan"' <<<"$answer_templates_cli"

answer_profiles_cli="$(run_code_context answer-profiles --json)"
printf '%s\n' "$answer_profiles_cli"
grep -q '"name": "review-change"' <<<"$answer_profiles_cli"
grep -q '"name": "plan-implementation"' <<<"$answer_profiles_cli"
grep -q '"name": "smoke-custom"' <<<"$answer_profiles_cli"
grep -q '"max_context_chars": 2000' <<<"$answer_profiles_cli"

provider_doctor_cli="$(run_code_context provider-doctor --json)"
printf '%s\n' "$provider_doctor_cli"
grep -q '"kind": "embedding"' <<<"$provider_doctor_cli"
grep -q '"kind": "answer"' <<<"$provider_doctor_cli"
grep -q '"kind": "answer_profile"' <<<"$provider_doctor_cli"
grep -q '"profile": "smoke-custom"' <<<"$provider_doctor_cli"
grep -q '"provider": "openai-compatible"' <<<"$provider_doctor_cli"

answer_context_cli="$(run_code_context answer "Where is HealthMessage used?" --context-only --json --profile explain-code --limit 5 --target-kind symbol --text-weight 0.6 --vector-weight 0.4 --dedupe --max-per-file 2 --max-context-item-chars 800 --max-context-chars 2000)"
printf '%s\n' "$answer_context_cli"
grep -q '"context_only": true' <<<"$answer_context_cli"
grep -q '"profile": "explain-code"' <<<"$answer_context_cli"
grep -q '"template": "explain"' <<<"$answer_context_cli"
grep -q '"context":' <<<"$answer_context_cli"
grep -q '"sources":' <<<"$answer_context_cli"
grep -q '"retrieval":' <<<"$answer_context_cli"
grep -q '"dedupe_context": true' <<<"$answer_context_cli"
grep -q '"max_context_chars": 2000' <<<"$answer_context_cli"
grep -q '"citation": "\[1\]"' <<<"$answer_context_cli"
grep -q "HealthMessage" <<<"$answer_context_cli"

answer_markdown_cli="$(run_code_context answer "Where is HealthMessage used?" --context-only --format markdown --limit 5)"
printf '%s\n' "$answer_markdown_cli"
grep -q "# Answer" <<<"$answer_markdown_cli"
grep -q "## Sources" <<<"$answer_markdown_cli"
grep -q "\[1\]" <<<"$answer_markdown_cli"

answer_cli="$(run_code_context answer "Where is HealthMessage used?" --profile plan-implementation --system-prompt "Answer from smoke evidence and cite sources." --require-citations --min-citation-coverage 0.1 --evaluate --min-evaluation-score 0.2 --dedupe --max-per-file 2 --max-context-item-chars 800 --max-context-chars 2000 --json --limit 5)"
printf '%s\n' "$answer_cli"
grep -q '"model": "smoke-answer"' <<<"$answer_cli"
grep -q '"profile": "plan-implementation"' <<<"$answer_cli"
grep -q '"template": "plan"' <<<"$answer_cli"
grep -q "Smoke answer: HealthMessage" <<<"$answer_cli"
grep -q '"sources":' <<<"$answer_cli"
grep -q '"retrieval":' <<<"$answer_cli"
grep -q '"retriever": "local-reranker"' <<<"$answer_cli"
grep -q '"citation": "\[1\]"' <<<"$answer_cli"
grep -q '"grounding":' <<<"$answer_cli"
grep -q '"required": true' <<<"$answer_cli"
grep -q '"min_coverage": 0.1' <<<"$answer_cli"
grep -q '"passed": true' <<<"$answer_cli"
grep -q '"valid_citations":' <<<"$answer_cli"
grep -q '"evaluation":' <<<"$answer_cli"
grep -q '"evaluator": "local-rule"' <<<"$answer_cli"
grep -q '"min_score": 0.2' <<<"$answer_cli"
grep -q '"usage":' <<<"$answer_cli"

answer_custom_cli="$(run_code_context answer "Where is HealthMessage used?" --profile smoke-custom --json)"
printf '%s\n' "$answer_custom_cli"
grep -q '"profile": "smoke-custom"' <<<"$answer_custom_cli"
grep -q '"template": "review"' <<<"$answer_custom_cli"
grep -q '"retrieval":' <<<"$answer_custom_cli"
grep -q '"grounding":' <<<"$answer_custom_cli"
grep -q '"evaluation":' <<<"$answer_custom_cli"
grep -q "Smoke answer: HealthMessage" <<<"$answer_custom_cli"

routes="$(run_code_context routes)"
printf '%s\n' "$routes"
grep -q "/health" <<<"$routes"

docs="$(run_code_context docs-for HealthHandler)"
printf '%s\n' "$docs"
grep -q "docs/health.md" <<<"$docs"

traverse_cli="$(run_code_context graph traverse docs/health.md --edge references --include-paths --limit 10)"
printf '%s\n' "$traverse_cli"
grep -q '"kind": "references"' <<<"$traverse_cli"
grep -q "HealthHandler" <<<"$traverse_cli"
grep -q '"paths"' <<<"$traverse_cli"

traverse_text_cli="$(run_code_context graph traverse "text:Health" --edge similar --target-kind symbol --include-paths --limit 10)"
printf '%s\n' "$traverse_text_cli"
grep -q '"kind": "similar"' <<<"$traverse_text_cli"
grep -q "HealthMessage" <<<"$traverse_text_cli"

impact_json="$(run_code_context impact HealthMessage --json)"
printf '%s\n' "$impact_json"
grep -q '"graph_traversal"' <<<"$impact_json"
grep -q "Graph traversal" <<<"$impact_json"

SERVER_LOG="$SMOKE_DIR/code-context-server.log"
run_code_context serve --port "$HTTP_PORT" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
wait_for_http

text_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/text?q=Health&limit=10")"
printf '%s\n' "$text_api"
grep -q '"kind":"symbol"' <<<"$text_api"
grep -q '"kind":"document"' <<<"$text_api"
grep -q "HealthHandler" <<<"$text_api"
grep -q "docs/health.md" <<<"$text_api"

embedding_status_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/embedding-status?limit=10")"
printf '%s\n' "$embedding_status_api"
grep -q '"enabled":true' <<<"$embedding_status_api"
grep -q '"model":"smoke-embedding"' <<<"$embedding_status_api"
grep -q '"type":"healthy"' <<<"$embedding_status_api"

embedding_lifecycle_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/embedding-lifecycle?limit=10")"
printf '%s\n' "$embedding_lifecycle_api"
grep -q '"summary":' <<<"$embedding_lifecycle_api"
grep -q '"recommended_actions"' <<<"$embedding_lifecycle_api"

embedding_namespaces_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/embedding-namespaces")"
printf '%s\n' "$embedding_namespaces_api"
grep -q '"total_namespaces":1' <<<"$embedding_namespaces_api"
grep -q '"model":"smoke-embedding"' <<<"$embedding_namespaces_api"

embedding_prune_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/embedding-prune?model=smoke-embedding-missing&dimensions=3" -H 'Content-Type: application/json' --data '{}')"
printf '%s\n' "$embedding_prune_api"
grep -q '"dry_run":true' <<<"$embedding_prune_api"
grep -q "was not found" <<<"$embedding_prune_api"

vector_api_status="$(curl -sS -o "$SMOKE_DIR/vector-api.out" -w "%{http_code}" -X POST "http://127.0.0.1:${HTTP_PORT}/api/vector" -H 'Content-Type: application/json' --data '{}')"
cat "$SMOKE_DIR/vector-api.out"
[[ "$vector_api_status" == "400" ]]
grep -q "missing 'vector' or 'query_text'" "$SMOKE_DIR/vector-api.out"

vector_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/vector" -H 'Content-Type: application/json' --data '{"query_text":"HealthMessage","limit":5}')"
printf '%s\n' "$vector_api"
grep -q '"source":"vector"' <<<"$vector_api"
grep -q "HealthMessage" <<<"$vector_api"

hybrid_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/hybrid" -H 'Content-Type: application/json' --data '{"query":"HealthMessage","limit":5}')"
printf '%s\n' "$hybrid_api"
grep -q '"source":"hybrid"' <<<"$hybrid_api"
grep -q '"hybrid_vector_score"' <<<"$hybrid_api"
grep -q "HealthMessage" <<<"$hybrid_api"

answer_context_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/answer" -H 'Content-Type: application/json' --data '{"question":"Where is HealthMessage used?","context_only":true,"limit":5,"dedupe_context":true,"max_per_file":2,"max_context_item_chars":800,"max_context_chars":2000}')"
printf '%s\n' "$answer_context_api"
grep -q '"context_only":true' <<<"$answer_context_api"
grep -q '"context":' <<<"$answer_context_api"
grep -q '"sources":' <<<"$answer_context_api"
grep -q '"retrieval":' <<<"$answer_context_api"
grep -q '"dedupe_context":true' <<<"$answer_context_api"
grep -q '"max_context_chars":2000' <<<"$answer_context_api"
grep -q '"citation":"\[1\]"' <<<"$answer_context_api"
grep -q "HealthMessage" <<<"$answer_context_api"

answer_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/answer" -H 'Content-Type: application/json' --data '{"question":"Where is HealthMessage used?","profile":"review-change","require_citations":true,"min_citation_coverage":0.1,"evaluate":true,"min_evaluation_score":0.2,"dedupe_context":true,"max_per_file":2,"max_context_item_chars":800,"max_context_chars":2000,"system_prompt":"Answer from smoke evidence and cite sources.","filter":{"target_kinds":["symbol"]},"text_weight":0.6,"vector_weight":0.4,"limit":5}')"
printf '%s\n' "$answer_api"
grep -q '"model":"smoke-answer"' <<<"$answer_api"
grep -q '"profile":"review-change"' <<<"$answer_api"
grep -q '"template":"review"' <<<"$answer_api"
grep -q "Smoke answer: HealthMessage" <<<"$answer_api"
grep -q '"sources":' <<<"$answer_api"
grep -q '"retrieval":' <<<"$answer_api"
grep -q '"retriever":"local-reranker"' <<<"$answer_api"
grep -q '"citation":"\[1\]"' <<<"$answer_api"
grep -q '"grounding":' <<<"$answer_api"
grep -q '"required":true' <<<"$answer_api"
grep -q '"min_coverage":0.1' <<<"$answer_api"
grep -q '"passed":true' <<<"$answer_api"
grep -q '"valid_citations":' <<<"$answer_api"
grep -q '"evaluation":' <<<"$answer_api"
grep -q '"evaluator":"local-rule"' <<<"$answer_api"
grep -q '"min_score":0.2' <<<"$answer_api"
grep -q '"usage":' <<<"$answer_api"

answer_templates_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/answer-templates?include_prompts=true")"
printf '%s\n' "$answer_templates_api"
grep -q '"name":"general"' <<<"$answer_templates_api"
grep -q '"name":"plan"' <<<"$answer_templates_api"
grep -q '"prompt":' <<<"$answer_templates_api"

answer_profiles_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/answer-profiles")"
printf '%s\n' "$answer_profiles_api"
grep -q '"name":"review-change"' <<<"$answer_profiles_api"
grep -q '"name":"plan-implementation"' <<<"$answer_profiles_api"
grep -q '"name":"smoke-custom"' <<<"$answer_profiles_api"
grep -q '"max_context_chars":2000' <<<"$answer_profiles_api"

provider_doctor_api="$(curl -fsS "http://127.0.0.1:${HTTP_PORT}/api/provider-diagnostics")"
printf '%s\n' "$provider_doctor_api"
grep -q '"kind":"embedding"' <<<"$provider_doctor_api"
grep -q '"kind":"answer"' <<<"$provider_doctor_api"
grep -q '"kind":"answer_profile"' <<<"$provider_doctor_api"
grep -q '"profile":"smoke-custom"' <<<"$provider_doctor_api"
grep -q '"provider":"openai-compatible"' <<<"$provider_doctor_api"

traverse_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/graph/traverse" -H 'Content-Type: application/json' --data '{"target":"text:Health","edge_kinds":["similar"],"filter":{"target_kinds":["symbol"]},"include_paths":true,"direction":"outbound","limit":10}')"
printf '%s\n' "$traverse_api"
grep -q '"kind":"similar"' <<<"$traverse_api"
grep -q "HealthMessage" <<<"$traverse_api"
grep -q '"paths"' <<<"$traverse_api"

echo "Helix smoke passed."
