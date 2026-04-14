#!/usr/bin/env bash
# run_tests.sh — runs ALL available tests in the codebase.
#
# By default this script runs every test layer:
#   1. Go backend unit/api/security/integration tests (in a Docker container)
#   2. Frontend Vitest unit/component tests (in a Docker container)
#   3. Playwright E2E smoke tests (needs the stack running)
#   4. External HTTP API tests (needs the stack running)
#
# Usage:
#   ./run_tests.sh              — all tests
#   ./run_tests.sh --backend    — Go tests only
#   ./run_tests.sh --frontend   — Vitest tests only
#   ./run_tests.sh --e2e        — Playwright E2E only
#   ./run_tests.sh --external   — External HTTP API tests only
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
BACKEND_ONLY=false
FRONTEND_ONLY=false
E2E_ONLY=false
EXTERNAL_ONLY=false

for arg in "$@"; do
  case "$arg" in
    --backend)  BACKEND_ONLY=true   ;;
    --frontend) FRONTEND_ONLY=true  ;;
    --e2e)      E2E_ONLY=true       ;;
    --external) EXTERNAL_ONLY=true  ;;
  esac
done

log()  { printf '\n\033[1;34m[run_tests] %s\033[0m\n' "$1"; }
pass() { printf '\033[1;32m  PASS: %s\033[0m\n' "$1"; }
fail() { printf '\033[1;31m  FAIL: %s\033[0m\n' "$1"; exit 1; }

cd "$ROOT"

run_all() {
  [ "$BACKEND_ONLY" = false ] && [ "$FRONTEND_ONLY" = false ] && \
  [ "$E2E_ONLY" = false ] && [ "$EXTERNAL_ONLY" = false ]
}

# ── Ensure Docker stack is running for E2E + external tests ──────────────────
ensure_stack() {
  log "Ensuring Docker stack is running..."
  if ! docker compose ps --format json 2>/dev/null | grep -q '"api"'; then
    log "Starting Docker stack..."
    docker compose up --build -d
    log "Waiting for API health check..."
    for i in $(seq 1 60); do
      if curl -sf http://localhost:8080/api/health >/dev/null 2>&1; then
        log "API is healthy."
        return 0
      fi
      sleep 2
    done
    log "ERROR: API did not become healthy in 120s"
    return 1
  else
    log "Stack already running."
  fi
}

# ── Extract bootstrap passwords from Docker secrets ──────────────────────────
extract_passwords() {
  export ADMIN_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_admin.txt 2>/dev/null | tr -d '\r\n' || echo "")
  export FINANCE_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_finance.txt 2>/dev/null | tr -d '\r\n' || echo "")
  export PROCUREMENT_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_procurement.txt 2>/dev/null | tr -d '\r\n' || echo "")
  export APPROVER_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_approver.txt 2>/dev/null | tr -d '\r\n' || echo "")
  export LEARNER_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_learner.txt 2>/dev/null | tr -d '\r\n' || echo "")
  export MODERATOR_PW=$(docker compose exec -T api cat /runtime/secrets/bootstrap_pw_moderator.txt 2>/dev/null | tr -d '\r\n' || echo "")
}

# ── 1. Backend tests (Go inside Docker) ─────────────────────────────────────
if run_all || [ "$BACKEND_ONLY" = true ]; then
  log "Running Go backend tests (unit + api + security + integration + scheduler)..."

  if docker run --rm \
    -v "$ROOT":/src \
    -w /src \
    golang:1.23-alpine \
    sh -c "apk add --no-cache git >/dev/null 2>&1 && go test -count=1 -timeout 120s ./tests/api/... ./tests/security/... ./tests/unit/... ./tests/integration/... ./cmd/..."; then
    pass "Backend tests"
  else
    fail "Backend tests"
  fi
fi

# ── 2. Frontend tests (Vitest inside Docker) ────────────────────────────────
if run_all || [ "$FRONTEND_ONLY" = true ]; then
  log "Running frontend Vitest unit/component tests..."

  if docker run --rm \
    -v "$ROOT":/src \
    -w /src \
    node:20-alpine \
    sh -c "npm ci --prefer-offline --silent 2>/dev/null; npx vitest run --reporter=verbose"; then
    pass "Frontend unit/component tests"
  else
    fail "Frontend unit/component tests"
  fi
fi

# ── 3. Playwright E2E (needs running stack) ─────────────────────────────────
if run_all || [ "$E2E_ONLY" = true ]; then
  ensure_stack

  log "Running Playwright E2E tests..."

  if docker run --rm \
    --network host \
    -v "$ROOT":/src \
    -w /src \
    -e BASE_URL="${BASE_URL:-http://localhost:3000}" \
    -e SKIP_WEBSERVER=1 \
    mcr.microsoft.com/playwright:v1.42.1-jammy \
    sh -c "npm ci --prefer-offline --silent 2>/dev/null; npx playwright test --reporter=line"; then
    pass "E2E tests"
  else
    fail "E2E tests"
  fi
fi

# ── 4. External HTTP API tests (needs running stack) ────────────────────────
if run_all || [ "$EXTERNAL_ONLY" = true ]; then
  ensure_stack
  extract_passwords

  log "Running external HTTP API tests against live stack..."

  if docker run --rm \
    --network host \
    -v "$ROOT":/src \
    -w /src \
    -e ADMIN_PW="$ADMIN_PW" \
    -e FINANCE_PW="$FINANCE_PW" \
    -e PROCUREMENT_PW="$PROCUREMENT_PW" \
    -e APPROVER_PW="$APPROVER_PW" \
    -e LEARNER_PW="$LEARNER_PW" \
    -e MODERATOR_PW="$MODERATOR_PW" \
    golang:1.23-alpine \
    sh -c "apk add --no-cache git >/dev/null 2>&1 && go test -v -count=1 -timeout 120s ./tests/external/..."; then
    pass "External HTTP API tests"
  else
    fail "External HTTP API tests"
  fi
fi

log "All tests completed successfully."
