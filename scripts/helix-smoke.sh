#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELIX_URL="${HELIX_URL:-http://localhost:6969}"
HELIX_PROJECT_ID="${HELIX_PROJECT_ID:-code-context-helix-smoke}"
SMOKE_DIR="${CODE_CONTEXT_HELIX_SMOKE_DIR:-}"
HTTP_PORT="${CODE_CONTEXT_HELIX_SMOKE_PORT:-19090}"
KEEP_SMOKE_DIR="${KEEP_SMOKE_DIR:-0}"
SERVER_PID=""
CLEAN_SMOKE_DIR=0

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
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

cd "$ROOT_DIR"

run_code_context() {
  go run ./cmd/code-context \
    --root "$FIXTURE" \
    --store-backend helix \
    --helix-url "$HELIX_URL" \
    --helix-project-id "$HELIX_PROJECT_ID" \
    "$@"
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
echo "WARNING: use a fresh/dedicated Helix instance if it was initialized by older code-context versions with unscoped unique path indexes."

run_code_context rebuild --verbose

stats="$(run_code_context stats)"
printf '%s\n' "$stats"
grep -q "Files:[[:space:]]*2" <<<"$stats"
grep -q "Documents:[[:space:]]*1" <<<"$stats"

status="$(run_code_context status)"
printf '%s\n' "$status"
grep -q "Capabilities:.*text_search" <<<"$status"
grep -q "Capabilities:.*graph_traversal" <<<"$status"

search="$(run_code_context search Health)"
printf '%s\n' "$search"
grep -q "HealthHandler" <<<"$search"
grep -q "HealthMessage" <<<"$search"

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

traverse_api="$(curl -fsS -X POST "http://127.0.0.1:${HTTP_PORT}/api/graph/traverse" -H 'Content-Type: application/json' --data '{"target":"text:Health","edge_kinds":["similar"],"filter":{"target_kinds":["symbol"]},"include_paths":true,"direction":"outbound","limit":10}')"
printf '%s\n' "$traverse_api"
grep -q '"kind":"similar"' <<<"$traverse_api"
grep -q "HealthMessage" <<<"$traverse_api"
grep -q '"paths"' <<<"$traverse_api"

echo "Helix smoke passed."
