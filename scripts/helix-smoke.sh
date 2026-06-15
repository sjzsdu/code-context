#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELIX_URL="${HELIX_URL:-http://localhost:6969}"
HELIX_PROJECT_ID="${HELIX_PROJECT_ID:-code-context-helix-smoke}"
SMOKE_DIR="${CODE_CONTEXT_HELIX_SMOKE_DIR:-}"
KEEP_SMOKE_DIR="${KEEP_SMOKE_DIR:-0}"

if [[ -z "$SMOKE_DIR" ]]; then
  SMOKE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/code-context-helix-smoke.XXXXXX")"
  if [[ "$KEEP_SMOKE_DIR" != "1" ]]; then
    trap 'rm -rf "$SMOKE_DIR"' EXIT
  fi
else
  rm -rf "$SMOKE_DIR"
  mkdir -p "$SMOKE_DIR"
fi

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

echo "Helix smoke fixture: $FIXTURE"
echo "Helix URL: $HELIX_URL"
echo "Helix project id: $HELIX_PROJECT_ID"
echo "WARNING: use a fresh/dedicated Helix instance if it was initialized by older code-context versions with unscoped unique path indexes."

run_code_context rebuild --verbose

stats="$(run_code_context stats)"
printf '%s\n' "$stats"
grep -q "Files:[[:space:]]*2" <<<"$stats"
grep -q "Documents:[[:space:]]*1" <<<"$stats"

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

echo "Helix smoke passed."
